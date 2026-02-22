package util

import "testing"

func TestGuessGatewayFromIP_DefaultPattern(t *testing.T) {
	gateway, err := guessGatewayFromIP("eth8", "192.168.10.27")
	if err != nil {
		t.Fatalf("guessGatewayFromIP returned error: %v", err)
	}
	if gateway != "192.168.10.1" {
		t.Fatalf("unexpected gateway: %s", gateway)
	}
}

func TestGuessGatewayFromIP_WireGuardPattern(t *testing.T) {
	gateway, err := guessGatewayFromIP("wg-sgp", "10.49.1.2")
	if err != nil {
		t.Fatalf("guessGatewayFromIP returned error: %v", err)
	}
	if gateway != "10.49.0.1" {
		t.Fatalf("unexpected wireguard gateway guess: %s", gateway)
	}
}

func TestGuessGatewayFromIP_InvalidInput(t *testing.T) {
	if _, err := guessGatewayFromIP("eth8", "not-an-ip"); err == nil {
		t.Fatalf("expected invalid IP error")
	}
}

func TestSelectLANInterfaceAndIPv4_PrefersBr0(t *testing.T) {
	name, ip := selectLANInterfaceAndIPv4([]InterfaceInfo{
		{Name: "eth8", Addresses: []string{"192.168.1.10/24"}},
		{Name: "br0", Addresses: []string{"10.0.0.1/24"}},
		{Name: "br5", Addresses: []string{"10.20.0.1/24"}},
	})

	if name != "br0" {
		t.Fatalf("expected br0, got %q", name)
	}
	if ip != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %q", ip)
	}
}

func TestSelectLANInterfaceAndIPv4_SkipsTunnelInterfaces(t *testing.T) {
	name, ip := selectLANInterfaceAndIPv4([]InterfaceInfo{
		{Name: "wg-sgp", Addresses: []string{"10.99.0.2/32"}},
		{Name: "tun0", Addresses: []string{"10.80.0.2/24"}},
		{Name: "br10", Addresses: []string{"192.168.10.1/24"}},
	})

	if name != "br10" {
		t.Fatalf("expected br10, got %q", name)
	}
	if ip != "192.168.10.1" {
		t.Fatalf("expected 192.168.10.1, got %q", ip)
	}
}

func TestSelectLANInterfaceAndIPv4_NoPrivateCandidate(t *testing.T) {
	name, ip := selectLANInterfaceAndIPv4([]InterfaceInfo{
		{Name: "lo", Addresses: []string{"127.0.0.1/8"}},
		{Name: "eth0", Addresses: []string{"198.51.100.10/24"}},
	})
	if name != "" || ip != "" {
		t.Fatalf("expected no candidate, got name=%q ip=%q", name, ip)
	}
}

func TestInterfaceStateConnected(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		flagUp bool
		want   bool
	}{
		{name: "up", state: "up", flagUp: false, want: true},
		{name: "unknown with up flag", state: "unknown", flagUp: true, want: true},
		{name: "unknown without up flag", state: "unknown", flagUp: false, want: false},
		{name: "dormant with up flag", state: "dormant", flagUp: true, want: true},
		{name: "down", state: "down", flagUp: true, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := interfaceStateConnected(tc.state, tc.flagUp)
			if got != tc.want {
				t.Fatalf("interfaceStateConnected(%q, %v)=%v, want %v", tc.state, tc.flagUp, got, tc.want)
			}
		})
	}
}
