package server

import "testing"

func TestFlowMarkMatchesVPN(t *testing.T) {
	cases := []struct {
		name    string
		flow    uint32
		vpn     uint32
		matches bool
	}{
		{
			name:    "exact match",
			flow:    0x169,
			vpn:     0x169,
			matches: true,
		},
		{
			name:    "lower 16 bits match",
			flow:    0x1a0169,
			vpn:     0x169,
			matches: true,
		},
		{
			name:    "lower 8 bits match",
			flow:    0x1a00c8,
			vpn:     0xc8,
			matches: true,
		},
		{
			name:    "no match",
			flow:    0x1a00d0,
			vpn:     0xc8,
			matches: false,
		},
		{
			name:    "zero flow mark",
			flow:    0,
			vpn:     0x169,
			matches: false,
		},
		{
			name:    "invalid vpn mark below minimum",
			flow:    0x169,
			vpn:     100,
			matches: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := flowMarkMatchesVPN(tc.flow, tc.vpn)
			if actual != tc.matches {
				t.Fatalf("expected %v, got %v (flow=%#x vpn=%#x)", tc.matches, actual, tc.flow, tc.vpn)
			}
		})
	}
}
