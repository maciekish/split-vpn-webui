package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPathWithinBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "vpns")
	m := NewManager(base)
	m.configs["good"] = &VPNConfig{
		Name: "good",
		Path: filepath.Join(base, "good"),
	}

	path, err := m.ConfigPath("good")
	if err != nil {
		t.Fatalf("expected config path, got %v", err)
	}
	want, err := filepath.Abs(filepath.Join(base, "good", "vpn.conf"))
	if err != nil {
		t.Fatalf("failed to resolve expected path: %v", err)
	}
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestConfigPathRejectsEscapingDirectory(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "vpns")
	outside := filepath.Join(root, "outside")
	m := NewManager(base)
	m.configs["bad"] = &VPNConfig{
		Name: "bad",
		Path: outside,
	}

	_, err := m.ConfigPath("bad")
	if err == nil {
		t.Fatalf("expected escaping path to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes base path") {
		t.Fatalf("expected escape-path error, got %v", err)
	}
}

func TestDiscoverKeepsConfiguredWireGuardInterface(t *testing.T) {
	base := filepath.Join(t.TempDir(), "vpns")
	vpnDir := filepath.Join(base, "wg-sgp")
	if err := os.MkdirAll(vpnDir, 0o700); err != nil {
		t.Fatalf("mkdir vpn dir: %v", err)
	}

	vpnConf := "VPN_PROVIDER=external\nDEV=wg-legacy\nCONFIG_FILE=dreammachine.conf\n"
	if err := os.WriteFile(filepath.Join(vpnDir, "vpn.conf"), []byte(vpnConf), 0o644); err != nil {
		t.Fatalf("write vpn.conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vpnDir, "dreammachine.conf"), []byte("[Interface]\n"), 0o600); err != nil {
		t.Fatalf("write wireguard config: %v", err)
	}

	m := NewManager(base)
	configs, err := m.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected one config, got %d", len(configs))
	}
	if configs[0].InterfaceName != "wg-legacy" {
		t.Fatalf("expected configured DEV value wg-legacy, got %q", configs[0].InterfaceName)
	}
}
