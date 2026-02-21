package vpn

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testUnitManager struct {
	written map[string]string
	removed []string
}

func (t *testUnitManager) WriteUnit(unitName, content string) error {
	if t.written == nil {
		t.written = map[string]string{}
	}
	t.written[unitName] = content
	return nil
}

func (t *testUnitManager) RemoveUnit(unitName string) error {
	t.removed = append(t.removed, unitName)
	return nil
}

func newTestManager(t *testing.T) (*Manager, string, *testUnitManager) {
	t.Helper()
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}
	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip"),
			"ip -6 rule show": errors.New("missing ip"),
		},
	})
	if err != nil {
		t.Fatalf("create allocator: %v", err)
	}
	unitManager := &testUnitManager{}
	manager, err := NewManager(vpnsDir, alloc, unitManager)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	return manager, vpnsDir, unitManager
}

func TestManagerCreateGetUpdateDeleteWireGuard(t *testing.T) {
	manager, vpnsDir, unitManager := newTestManager(t)

	config := `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32, 2001:db8:a161::2/128
PostUp = sh /etc/split-vpn/vpn/updown.sh %i up
PreDown = sh /etc/split-vpn/vpn/updown.sh %i down

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = sgp.swic.name:51820
PersistentKeepalive = 25
`

	created, err := manager.Create(UpsertRequest{
		Name:   "wg-sgp",
		Type:   "wireguard",
		Config: config,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if created.RouteTable < 200 {
		t.Fatalf("expected route table >= 200, got %d", created.RouteTable)
	}
	if created.FWMark < 200 {
		t.Fatalf("expected fwmark >= 200, got %d", created.FWMark)
	}
	if len(created.Warnings) == 0 {
		t.Fatalf("expected warnings about removed legacy hooks")
	}
	if _, ok := unitManager.written["svpn-wg-sgp.service"]; !ok {
		t.Fatalf("expected unit to be written for created vpn")
	}

	profileDir := filepath.Join(vpnsDir, "wg-sgp")
	if info, err := os.Stat(profileDir); err != nil {
		t.Fatalf("stat profile dir: %v", err)
	} else if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("expected profile dir mode 0700, got %o", mode)
	}

	configPath := filepath.Join(profileDir, created.ConfigFile)
	if info, err := os.Stat(configPath); err != nil {
		t.Fatalf("stat config file: %v", err)
	} else if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected config mode 0600, got %o", mode)
	}

	vpnConfPath := filepath.Join(profileDir, "vpn.conf")
	if info, err := os.Stat(vpnConfPath); err != nil {
		t.Fatalf("stat vpn.conf: %v", err)
	} else if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("expected vpn.conf mode 0644, got %o", mode)
	}

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(rawConfig), "Table = ") {
		t.Fatalf("expected generated config to include Table directive")
	}
	if strings.Contains(string(rawConfig), "updown.sh") {
		t.Fatalf("expected legacy updown hooks to be stripped")
	}

	fetched, err := manager.Get("wg-sgp")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if fetched.Name != "wg-sgp" {
		t.Fatalf("unexpected fetched name %q", fetched.Name)
	}

	updatedConfig := `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0
Endpoint = updated.swic.name:51820
`
	updated, err := manager.Update("wg-sgp", UpsertRequest{Config: updatedConfig})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !strings.Contains(updated.RawConfig, "updated.swic.name") {
		t.Fatalf("expected updated config to be persisted")
	}

	if err := manager.Delete("wg-sgp"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if len(unitManager.removed) == 0 || unitManager.removed[len(unitManager.removed)-1] != "svpn-wg-sgp.service" {
		t.Fatalf("expected unit removal for deleted vpn, got %#v", unitManager.removed)
	}
	if _, err := manager.Get("wg-sgp"); !errors.Is(err, ErrVPNNotFound) {
		t.Fatalf("expected ErrVPNNotFound after delete, got %v", err)
	}
}

func TestManagerNameValidationAndDuplicates(t *testing.T) {
	manager, _, _ := newTestManager(t)

	wgConfig := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`

	if _, err := manager.Create(UpsertRequest{Name: "../bad", Type: "wireguard", Config: wgConfig}); !errors.Is(err, ErrVPNValidation) {
		t.Fatalf("expected validation error for invalid name, got %v", err)
	}

	if _, err := manager.Create(UpsertRequest{Name: "good-vpn", Type: "wireguard", Config: wgConfig}); err != nil {
		t.Fatalf("unexpected error creating valid vpn: %v", err)
	}
	if _, err := manager.Create(UpsertRequest{Name: "good-vpn", Type: "wireguard", Config: wgConfig}); !errors.Is(err, ErrVPNAlreadyExists) {
		t.Fatalf("expected duplicate vpn error, got %v", err)
	}
}

func TestManagerPreservesUserTableAndDetectsConflicts(t *testing.T) {
	manager, _, _ := newTestManager(t)

	configWithTable := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
Table = 222
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`

	first, err := manager.Create(UpsertRequest{Name: "wg-one", Type: "wireguard", Config: configWithTable})
	if err != nil {
		t.Fatalf("Create first vpn failed: %v", err)
	}
	if first.RouteTable != 222 {
		t.Fatalf("expected route table 222, got %d", first.RouteTable)
	}

	if _, err := manager.Create(UpsertRequest{Name: "wg-two", Type: "wireguard", Config: configWithTable}); !errors.Is(err, ErrAllocationConflict) {
		t.Fatalf("expected allocation conflict for duplicate table, got %v", err)
	}
}

func TestManagerCreateOpenVPN(t *testing.T) {
	manager, _, _ := newTestManager(t)

	ovpn := `client
remote 87.98.233.31 1194
dev tun
nobind
<ca>
abc
</ca>
`
	profile, err := manager.Create(UpsertRequest{Name: "ovpn-web", Type: "openvpn", Config: ovpn})
	if err != nil {
		t.Fatalf("Create openvpn failed: %v", err)
	}
	if profile.Type != "openvpn" {
		t.Fatalf("expected type openvpn, got %q", profile.Type)
	}
	if profile.InterfaceName != "tun0" {
		t.Fatalf("expected interface tun0, got %q", profile.InterfaceName)
	}
	if filepath.Ext(profile.ConfigFile) != ".ovpn" {
		t.Fatalf("expected .ovpn config file, got %q", profile.ConfigFile)
	}
}

func TestManagerRejectsSystemInterfaceConflict(t *testing.T) {
	manager, _, _ := newTestManager(t)
	manager.listInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "wg0"}}, nil
	}

	wgConfig := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`
	_, err := manager.Create(UpsertRequest{
		Name:          "wg-system-conflict",
		Type:          "wireguard",
		Config:        wgConfig,
		InterfaceName: "wg0",
	})
	if !errors.Is(err, ErrVPNValidation) {
		t.Fatalf("expected validation error for system interface conflict, got %v", err)
	}
}

func TestManagerAllowsUpdateKeepingExistingSystemInterface(t *testing.T) {
	manager, _, _ := newTestManager(t)
	manager.listInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{}, nil
	}

	wgConfig := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`
	_, err := manager.Create(UpsertRequest{
		Name:          "wg-existing-iface",
		Type:          "wireguard",
		Config:        wgConfig,
		InterfaceName: "wg-existing",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	manager.listInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "wg-existing"}}, nil
	}
	_, err = manager.Update("wg-existing-iface", UpsertRequest{
		Config: strings.ReplaceAll(wgConfig, "host:51820", "updated:51820"),
	})
	if err != nil {
		t.Fatalf("expected update to allow existing interface, got %v", err)
	}
}

func TestManagerRejectsPeaceyInterfaceConflict(t *testing.T) {
	manager, _, _ := newTestManager(t)
	peaceyDir := t.TempDir()
	manager.peaceyDir = peaceyDir
	manager.listInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{}, nil
	}

	if err := os.MkdirAll(filepath.Join(peaceyDir, "peacey-one"), 0o700); err != nil {
		t.Fatalf("mkdir peacey profile: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(peaceyDir, "peacey-one", "vpn.conf"),
		[]byte("DEV=wg-peacey\nROUTE_TABLE=300\nMARK=0x300\n"),
		0o644,
	); err != nil {
		t.Fatalf("write peacey vpn.conf: %v", err)
	}

	wgConfig := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`
	_, err := manager.Create(UpsertRequest{
		Name:          "wg-peacey-iface",
		Type:          "wireguard",
		Config:        wgConfig,
		InterfaceName: "wg-peacey",
	})
	if !errors.Is(err, ErrVPNValidation) {
		t.Fatalf("expected interface validation conflict against peacey profile, got %v", err)
	}
}

func TestManagerRejectsPeaceyRouteTableConflict(t *testing.T) {
	manager, _, _ := newTestManager(t)
	peaceyDir := t.TempDir()
	manager.peaceyDir = peaceyDir
	manager.listInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{}, nil
	}

	if err := os.MkdirAll(filepath.Join(peaceyDir, "peacey-two"), 0o700); err != nil {
		t.Fatalf("mkdir peacey profile: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(peaceyDir, "peacey-two", "vpn.conf"),
		[]byte("DEV=wg-other\nROUTE_TABLE=444\nMARK=0x444\n"),
		0o644,
	); err != nil {
		t.Fatalf("write peacey vpn.conf: %v", err)
	}

	wgConfig := `[Interface]
PrivateKey = test
Address = 10.0.0.2/32
Table = 444
[Peer]
PublicKey = peer
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`
	_, err := manager.Create(UpsertRequest{
		Name:   "wg-peacey-table",
		Type:   "wireguard",
		Config: wgConfig,
	})
	if !errors.Is(err, ErrAllocationConflict) {
		t.Fatalf("expected route table allocation conflict against peacey profile, got %v", err)
	}
}
