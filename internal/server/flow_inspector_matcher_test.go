package server

import (
	"net/netip"
	"testing"

	"split-vpn-webui/internal/routing"
)

func TestMakeSelectorSetAndMACSetNormalization(t *testing.T) {
	selectors := makeSelectorSet([]string{" br0 ", "BR6", ""})
	if len(selectors) != 2 {
		t.Fatalf("expected 2 selectors, got %d", len(selectors))
	}
	if _, ok := selectors["br0"]; !ok {
		t.Fatalf("expected br0 selector")
	}
	if _, ok := selectors["br6"]; !ok {
		t.Fatalf("expected br6 selector")
	}

	macs := makeMACSet([]string{
		" AA:BB:CC:DD:EE:FF ",
		"aa:bb:cc:dd:ee:ff",
		"aa-bb-cc-dd-ee-ff",
		"#commented",
		"00:11:22:33:44:55 # Media device",
		"",
	})
	if len(macs) != 2 {
		t.Fatalf("expected deduplicated mac set size 2, got %d", len(macs))
	}
	if _, ok := macs["aa:bb:cc:dd:ee:ff"]; !ok {
		t.Fatalf("expected normalized mac key in set")
	}
	if _, ok := macs["00:11:22:33:44:55"]; !ok {
		t.Fatalf("expected inline-comment mac key in set")
	}
}

func TestDetectFlowNoMatchReason(t *testing.T) {
	sourceAddr := netip.MustParseAddr("10.0.1.20")
	destAddr := netip.MustParseAddr("142.250.74.14")
	flow := conntrackFlowSample{
		Protocol:        "tcp",
		SourceIP:        sourceAddr.String(),
		DestinationIP:   destAddr.String(),
		SourcePort:      50000,
		DestinationPort: 443,
	}
	rules := []compiledFlowRule{
		{
			SourceMACs: map[string]struct{}{
				"00:11:22:33:44:55": {},
			},
			DestinationPorts: []routing.PortRange{
				{Protocol: "tcp", Start: 443, End: 443},
			},
			DestinationPrefixes:       []netip.Prefix{netip.MustParsePrefix("142.250.74.0/24")},
			RequiresDestinationPrefix: true,
		},
	}

	reason := detectFlowNoMatchReason(rules, flow, sourceAddr, destAddr, "", "br0")
	if reason != flowNoMatchSourceMAC {
		t.Fatalf("expected source-mac reason, got %q", reason)
	}
}
