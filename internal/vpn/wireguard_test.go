package vpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWGConfig_ValidConfig(t *testing.T) {
	raw := `[Interface]
PrivateKey = QLowSWJxH9WJ4Az7MwZXN49wdMUt8KAe9yU8xgoJGGs=
Address = 10.49.1.2 ,2001:db8:a161::2
DNS = 1.1.1.1, 2606:4700:4700::1111
PostUp = sh /etc/split-vpn/vpn/updown.sh %i up
PreDown = sh /etc/split-vpn/vpn/updown.sh %i down
Table = 101

[Peer]
PublicKey = bbbaUHaEAPokg0IlEh2ShB35kIAosMo1pSlB3TduUTA=
PresharedKey = aPmtbvpiMLEqrdlusMrP8ywxNtXwjtZu0daWvvN0MVw=
AllowedIPs = 0.0.0.0/1,128.0.0.0/1,::/1,8000::/1
Endpoint = sgp.contoso.com:51820
PersistentKeepalive = 25

[Peer]
PublicKey = secondPublicKey=
AllowedIPs = 10.0.0.0/8
Endpoint = another.example.com:51820
`

	profile, err := ParseWGConfig(raw)
	if err != nil {
		t.Fatalf("ParseWGConfig returned error: %v", err)
	}
	if profile.Type != "wireguard" {
		t.Fatalf("expected type wireguard, got %q", profile.Type)
	}
	if profile.RouteTable != 101 {
		t.Fatalf("expected route table 101, got %d", profile.RouteTable)
	}
	if profile.Gateway != "sgp.contoso.com" {
		t.Fatalf("expected gateway sgp.contoso.com, got %q", profile.Gateway)
	}
	if profile.WireGuard == nil {
		t.Fatal("expected wireguard payload")
	}
	if got := profile.WireGuard.Interface.Addresses; len(got) != 2 || got[0] != "10.49.1.2" || got[1] != "2001:db8:a161::2" {
		t.Fatalf("unexpected addresses: %#v", got)
	}
	if got := profile.WireGuard.Interface.PostUp; len(got) != 1 {
		t.Fatalf("expected one PostUp command, got %#v", got)
	}
	if got := profile.WireGuard.Interface.PreDown; len(got) != 1 {
		t.Fatalf("expected one PreDown command, got %#v", got)
	}
	if len(profile.WireGuard.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(profile.WireGuard.Peers))
	}
}

func TestValidateWGConfig_InvalidConfigs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing private key",
			raw: `[Interface]
Address = 10.0.0.2/32
[Peer]
PublicKey = key
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`,
			want: "PrivateKey",
		},
		{
			name: "missing address",
			raw: `[Interface]
PrivateKey = key
[Peer]
PublicKey = key
AllowedIPs = 0.0.0.0/0
Endpoint = host:51820
`,
			want: "Address",
		},
		{
			name: "missing peer",
			raw: `[Interface]
PrivateKey = key
Address = 10.0.0.2/32
`,
			want: "Peer",
		},
		{
			name: "peer missing endpoint",
			raw: `[Interface]
PrivateKey = key
Address = 10.0.0.2/32
[Peer]
PublicKey = key
AllowedIPs = 0.0.0.0/0
`,
			want: "Endpoint",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWGConfig(tc.raw)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestWireGuardGenerateUnit(t *testing.T) {
	provider := NewWireGuardProvider()
	unit := provider.GenerateUnit(&VPNProfile{Name: "wg-sgp", ConfigFile: "wg0.conf"}, "/data/split-vpn-webui")

	checks := []string{
		"Description=split-vpn-webui WireGuard tunnel (wg-sgp)",
		"ExecStart=/usr/bin/wg-quick up /data/split-vpn-webui/vpns/wg-sgp/wg0.conf",
		"ExecStop=/usr/bin/wg-quick down /data/split-vpn-webui/vpns/wg-sgp/wg0.conf",
	}
	for _, check := range checks {
		if !strings.Contains(unit, check) {
			t.Fatalf("generated unit missing %q\n%s", check, unit)
		}
	}
}

func TestParseWGConfig_ReferenceSample(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "wg0.reference.conf"))
	if err != nil {
		t.Fatalf("read reference config: %v", err)
	}
	profile, err := ParseWGConfig(string(raw))
	if err != nil {
		t.Fatalf("ParseWGConfig failed for reference sample: %v", err)
	}
	if profile.RouteTable != 101 {
		t.Fatalf("expected route table 101 from reference sample, got %d", profile.RouteTable)
	}
	if profile.Gateway != "sgp.contoso.com" {
		t.Fatalf("expected gateway sgp.contoso.com, got %q", profile.Gateway)
	}
	if len(profile.WireGuard.Interface.Addresses) != 2 {
		t.Fatalf("expected 2 addresses from reference sample, got %d", len(profile.WireGuard.Interface.Addresses))
	}
}
