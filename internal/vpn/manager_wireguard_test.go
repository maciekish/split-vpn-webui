package vpn

import (
	"strings"
	"testing"
)

func TestSanitizeWireGuardConfigStripsLegacyHooks(t *testing.T) {
	raw := `[Interface]
PrivateKey = abc
Address = 10.0.0.2/32
PostUp = sh /etc/split-vpn/vpn/updown.sh %i up
PreDown = sh /etc/split-vpn/vpn/updown.sh %i down

[Peer]
PublicKey = def
AllowedIPs = 0.0.0.0/0
Endpoint = example.com:51820
`

	sanitized, warnings, err := sanitizeWireGuardConfig(raw, 201, true)
	if err != nil {
		t.Fatalf("sanitizeWireGuardConfig failed: %v", err)
	}
	if strings.Contains(strings.ToLower(sanitized), "updown.sh") {
		t.Fatalf("expected legacy hooks to be removed: %s", sanitized)
	}
	if !strings.Contains(sanitized, "Table = 201") {
		t.Fatalf("expected route table to be injected: %s", sanitized)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected warnings for removed legacy hooks")
	}
}

func TestSanitizeWireGuardConfigRemovesDNSWhenResolvconfMissing(t *testing.T) {
	raw := `[Interface]
PrivateKey = abc
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = def
AllowedIPs = 0.0.0.0/0
Endpoint = example.com:51820
`

	sanitized, warnings, err := sanitizeWireGuardConfig(raw, 201, false)
	if err != nil {
		t.Fatalf("sanitizeWireGuardConfig failed: %v", err)
	}
	if strings.Contains(strings.ToLower(sanitized), "dns =") {
		t.Fatalf("expected DNS directive to be removed when resolvconf is unavailable: %s", sanitized)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(strings.ToLower(warning), "resolvconf") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected resolvconf warning, got %#v", warnings)
	}
}
