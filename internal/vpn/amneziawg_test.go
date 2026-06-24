package vpn

import (
	"strings"
	"testing"
)

const awgTestConfig = `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32, 2001:db8:a161::2/128
Jc = 4
Jmin = 40
Jmax = 70
S1 = 116
S2 = 61
H1 = 1234567891
H2 = 1234567892
H3 = 1234567893
H4 = 1234567894
I1 = <b 0xf6ab3267fa><c><t><r 16>
J1 = <b 0xffffffff><r 30>
Itime = 120

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = sgp.contoso.com:51820
PersistentKeepalive = 25
`

func TestAmneziaWGProviderParsesFullParamSet(t *testing.T) {
	provider := NewAmneziaWGProvider()
	profile, err := provider.ParseConfig(awgTestConfig)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if profile.Type != "amneziawg" {
		t.Fatalf("expected type amneziawg, got %q", profile.Type)
	}
	params := profile.AmneziaWG
	if params == nil {
		t.Fatalf("expected AmneziaWG params to be parsed")
	}
	if params.Jc == nil || *params.Jc != 4 {
		t.Fatalf("expected Jc=4, got %v", params.Jc)
	}
	if params.Jmin == nil || *params.Jmin != 40 || params.Jmax == nil || *params.Jmax != 70 {
		t.Fatalf("unexpected Jmin/Jmax: %v/%v", params.Jmin, params.Jmax)
	}
	if params.S1 == nil || *params.S1 != 116 || params.S2 == nil || *params.S2 != 61 {
		t.Fatalf("unexpected S1/S2: %v/%v", params.S1, params.S2)
	}
	if params.H1 != "1234567891" || params.H4 != "1234567894" {
		t.Fatalf("unexpected headers: %q..%q", params.H1, params.H4)
	}
	if params.I1 == "" || params.J1 == "" {
		t.Fatalf("expected I1 and J1 to be set")
	}
	if params.ITime == nil || *params.ITime != 120 {
		t.Fatalf("unexpected Itime: %v", params.ITime)
	}
	if !params.UsesSpecialJunk() {
		t.Fatalf("expected UsesSpecialJunk to be true")
	}
	if params.UsesExtendedPadding() {
		t.Fatalf("expected UsesExtendedPadding to be false")
	}
}

func TestAmneziaWGProviderAcceptsVanillaConfig(t *testing.T) {
	config := `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0
Endpoint = 1.2.3.4:51820
`
	profile, err := NewAmneziaWGProvider().ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if !profile.AmneziaWG.IsEmpty() {
		t.Fatalf("expected empty params for vanilla config")
	}
}

func TestAmneziaWGProviderAcceptsKernelOnlyHeaderRanges(t *testing.T) {
	config := `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32
H1 = 100-199
H2 = 200-299
H3 = 300-399
H4 = 400-499

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0
Endpoint = 1.2.3.4:51820
`
	profile, err := NewAmneziaWGProvider().ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if !profile.AmneziaWG.UsesHeaderRanges() {
		t.Fatalf("expected header ranges to be detected")
	}
}

func TestAmneziaWGParamValidation(t *testing.T) {
	base := `[Interface]
PrivateKey = test-private-key
Address = 10.49.1.2/32
%s

[Peer]
PublicKey = test-peer-key
AllowedIPs = 0.0.0.0/0
Endpoint = 1.2.3.4:51820
`
	cases := []struct {
		name    string
		lines   string
		wantErr string
	}{
		{"jmin above jmax", "Jc = 2\nJmin = 80\nJmax = 70", "Jmin"},
		{"jc without sizes", "Jc = 2", "requires Jmin and Jmax"},
		{"partial headers", "H1 = 5", "all be set"},
		{"duplicate headers", "H1 = 5\nH2 = 5\nH3 = 6\nH4 = 7", "share the same value"},
		{"reversed header range", "H1 = 20-10\nH2 = 30\nH3 = 40\nH4 = 50", "range start"},
		{"overlapping header range", "H1 = 10-20\nH2 = 20-30\nH3 = 40\nH4 = 50", "overlap"},
		{"s1 collides with s2", "S1 = 10\nS2 = 66", "must not equal"},
		{"invalid junk tag", "I1 = not-a-tag", "junk packet definition"},
		{"kernel and userspace exclusive", "S3 = 10\nJ1 = <r 16>", "no available engine"},
		{"negative itime", "Itime = -5", "Itime"},
		{"non-integer jc", "Jc = many", "integer"},
		{"duplicate key", "Jc = 1\nJc = 2\nJmin = 10\nJmax = 20", "once"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := strings.Replace(base, "%s", testCase.lines, 1)
			_, err := NewAmneziaWGProvider().ParseConfig(config)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", testCase.wantErr)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected error containing %q, got %q", testCase.wantErr, err.Error())
			}
		})
	}
}

func TestAmneziaWGGenerateUnit(t *testing.T) {
	provider := NewAmneziaWGProvider()
	unit := provider.GenerateUnit(&VPNProfile{Name: "awg-sgp"}, "/data/split-vpn-webui")
	for _, want := range []string{
		"Type=notify",
		"ExecStart=/data/split-vpn-webui/split-vpn-webui tunnel run --name awg-sgp --data-dir /data/split-vpn-webui",
		"Restart=on-failure",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestInferInterfaceFromTypeAmneziaWG(t *testing.T) {
	iface := inferInterfaceFromType("amneziawg", "sgp.contoso.com")
	if !strings.HasPrefix(iface, "awg-sv-") {
		t.Fatalf("expected awg-sv- prefix, got %q", iface)
	}
	if len(iface) > 15 {
		t.Fatalf("interface name %q exceeds 15 characters", iface)
	}
	if err := validateInterfaceName(iface); err != nil {
		t.Fatalf("derived interface name invalid: %v", err)
	}
}

func TestNormalizeVPNTypeAmneziaWG(t *testing.T) {
	for _, alias := range []string{"amneziawg", "AWG", "Amnezia"} {
		if got := normalizeVPNType(alias); got != "amneziawg" {
			t.Fatalf("normalizeVPNType(%q) = %q", alias, got)
		}
	}
	if providerConfigValue("amneziawg") != "amneziawg" {
		t.Fatalf("unexpected providerConfigValue for amneziawg")
	}
}

func TestManagerCreateAmneziaWG(t *testing.T) {
	manager, _, unitManager := newTestManager(t)

	created, err := manager.Create(UpsertRequest{
		Name:   "awg-sgp",
		Type:   "amneziawg",
		Config: awgTestConfig,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if created.Type != "amneziawg" {
		t.Fatalf("expected type amneziawg, got %q", created.Type)
	}
	if !strings.HasPrefix(created.InterfaceName, "awg-sv-") {
		t.Fatalf("expected managed awg interface, got %q", created.InterfaceName)
	}
	if created.Meta["VPN_PROVIDER"] != "amneziawg" {
		t.Fatalf("expected VPN_PROVIDER=amneziawg, got %q", created.Meta["VPN_PROVIDER"])
	}
	unit, ok := unitManager.written["svpn-awg-sgp.service"]
	if !ok {
		t.Fatalf("expected unit to be written")
	}
	if !strings.Contains(unit, "tunnel run --name awg-sgp") {
		t.Fatalf("unit does not invoke tunnel subcommand:\n%s", unit)
	}
	// Table injection from the shared sanitizer must be present in the stored config.
	loaded, err := manager.Get("awg-sgp")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !strings.Contains(loaded.RawConfig, "Table = ") {
		t.Fatalf("expected Table injection in stored config:\n%s", loaded.RawConfig)
	}
	if loaded.AmneziaWG == nil || loaded.AmneziaWG.Jc == nil {
		t.Fatalf("expected params to round-trip through storage")
	}
}

func TestManagerRejectsAWGKeysOnWireGuardType(t *testing.T) {
	manager, _, _ := newTestManager(t)
	_, err := manager.Create(UpsertRequest{
		Name:   "plain-wg",
		Type:   "wireguard",
		Config: awgTestConfig,
	})
	if err == nil || !strings.Contains(err.Error(), "AmneziaWG") {
		t.Fatalf("expected AmneziaWG-keys rejection for wireguard type, got %v", err)
	}
}
