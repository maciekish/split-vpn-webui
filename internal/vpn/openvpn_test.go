package vpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOVPNConfig_ValidWithInlineBlocks(t *testing.T) {
	raw := `client
proto udp
remote 87.98.233.31 1194
dev tun
nobind
<ca>
-----BEGIN CERTIFICATE-----
abc
-----END CERTIFICATE-----
</ca>
<cert>
-----BEGIN CERTIFICATE-----
def
-----END CERTIFICATE-----
</cert>
<key>
-----BEGIN PRIVATE KEY-----
xyz
-----END PRIVATE KEY-----
</key>
<tls-crypt>
-----BEGIN OpenVPN Static key V1-----
123
-----END OpenVPN Static key V1-----
</tls-crypt>
`

	if err := ValidateOVPNConfig(raw); err != nil {
		t.Fatalf("ValidateOVPNConfig returned error: %v", err)
	}

	profile, err := NewOpenVPNProvider().ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if profile.Type != "openvpn" {
		t.Fatalf("expected type openvpn, got %q", profile.Type)
	}
	if profile.InterfaceName != "tun0" {
		t.Fatalf("expected dev tun -> tun0, got %q", profile.InterfaceName)
	}
	if profile.Gateway != "87.98.233.31" {
		t.Fatalf("expected gateway 87.98.233.31, got %q", profile.Gateway)
	}
	if profile.OpenVPN == nil {
		t.Fatal("expected parsed OpenVPN config")
	}
	for _, block := range []string{"ca", "cert", "key", "tls-crypt"} {
		if _, ok := profile.OpenVPN.InlineBlocks[block]; !ok {
			t.Fatalf("expected inline block %q", block)
		}
	}
}

func TestValidateOVPNConfig_InvalidConfigs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing client",
			raw: `remote 1.2.3.4 1194
dev tun0
`,
			want: "client",
		},
		{
			name: "missing remote",
			raw: `client
dev tun0
`,
			want: "remote",
		},
		{
			name: "missing dev",
			raw: `client
remote vpn.example.com 1194
`,
			want: "dev",
		},
		{
			name: "unclosed block",
			raw: `client
remote vpn.example.com 1194
dev tun
<ca>
abc
`,
			want: "unclosed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOVPNConfig(tc.raw)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestOpenVPNGenerateUnit(t *testing.T) {
	provider := NewOpenVPNProvider()
	unit := provider.GenerateUnit(&VPNProfile{Name: "ovpn-web", ConfigFile: "DreamMachine.ovpn", InterfaceName: "tun1"}, "/data/split-vpn-webui")

	checks := []string{
		"Description=split-vpn-webui OpenVPN tunnel (ovpn-web)",
		"ExecStart=/usr/sbin/openvpn --config /data/split-vpn-webui/vpns/ovpn-web/DreamMachine.ovpn --dev tun1 --route-noexec --script-security 1",
		"Restart=on-failure",
	}
	for _, check := range checks {
		if !strings.Contains(unit, check) {
			t.Fatalf("generated unit missing %q\n%s", check, unit)
		}
	}
}

func TestValidateOVPNConfig_ReferenceSample(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "dreammachine.reference.ovpn"))
	if err != nil {
		t.Fatalf("read reference ovpn: %v", err)
	}
	if err := ValidateOVPNConfig(string(raw)); err != nil {
		t.Fatalf("ValidateOVPNConfig failed for reference sample: %v", err)
	}
	profile, err := NewOpenVPNProvider().ParseConfig(string(raw))
	if err != nil {
		t.Fatalf("ParseConfig failed for reference sample: %v", err)
	}
	if profile.InterfaceName != "tun0" {
		t.Fatalf("expected dev tun to normalize to tun0, got %q", profile.InterfaceName)
	}
	if len(profile.OpenVPN.InlineBlocks) < 3 {
		t.Fatalf("expected inline blocks to be parsed, got %d", len(profile.OpenVPN.InlineBlocks))
	}
}

func TestRequiredOpenVPNFiles_ExternalReferences(t *testing.T) {
	raw := `client
remote vpn.example.com 1194
dev tun
ca ca.crt
cert client.crt
key client.key
auth-user-pass creds.txt
`
	profile, err := NewOpenVPNProvider().ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	files, err := requiredOpenVPNFiles(profile.OpenVPN)
	if err != nil {
		t.Fatalf("requiredOpenVPNFiles failed: %v", err)
	}
	want := []string{"ca.crt", "client.crt", "client.key", "creds.txt"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected supporting file list: got %v want %v", files, want)
	}
}

func TestRequiredOpenVPNFiles_RejectsEscapingPath(t *testing.T) {
	raw := `client
remote vpn.example.com 1194
dev tun
ca ../ca.crt
`
	profile, err := NewOpenVPNProvider().ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	_, err = requiredOpenVPNFiles(profile.OpenVPN)
	if err == nil {
		t.Fatalf("expected invalid supporting file path error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "supporting file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
