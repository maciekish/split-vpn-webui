package config

import (
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
