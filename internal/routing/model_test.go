package routing

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidateSupportsSourceInterfaceMACAndBothProtocol(t *testing.T) {
	group, err := NormalizeAndValidate(DomainGroup{
		Name:      "LAN-Devices",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:             "Device DNS",
				SourceInterfaces: []string{"BR6", "br6", "br0"},
				SourceMACs:       []string{"00:30:93:10:0A:12", "00:30:93:10:0a:12"},
				DestinationPorts: []PortRange{{Protocol: "both", Start: 53, End: 53}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeAndValidate failed: %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(group.Rules))
	}
	rule := group.Rules[0]
	if len(rule.SourceInterfaces) != 2 || rule.SourceInterfaces[0] != "br6" || rule.SourceInterfaces[1] != "br0" {
		t.Fatalf("unexpected source interfaces: %#v", rule.SourceInterfaces)
	}
	if len(rule.SourceMACs) != 1 || rule.SourceMACs[0] != "00:30:93:10:0a:12" {
		t.Fatalf("unexpected source macs: %#v", rule.SourceMACs)
	}
	if len(rule.DestinationPorts) != 1 || rule.DestinationPorts[0].Protocol != "both" {
		t.Fatalf("unexpected destination ports: %#v", rule.DestinationPorts)
	}
}

func TestNormalizeAndValidateRejectsInvalidSourceInterface(t *testing.T) {
	_, err := NormalizeAndValidate(DomainGroup{
		Name:      "LAN-Devices",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{Name: "bad", SourceInterfaces: []string{"br 6"}},
		},
	})
	if !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected ErrGroupValidation, got %v", err)
	}
}

func TestNormalizeAndValidateRejectsInvalidSourceMAC(t *testing.T) {
	_, err := NormalizeAndValidate(DomainGroup{
		Name:      "LAN-Devices",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{Name: "bad", SourceMACs: []string{"not-a-mac"}},
		},
	})
	if !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected ErrGroupValidation, got %v", err)
	}
}

func TestNormalizeAndValidateSupportsRawSelectorComments(t *testing.T) {
	group, err := NormalizeAndValidate(DomainGroup{
		Name:      "Commented",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name: "Rule 1",
				RawSelectors: &RuleRawSelectors{
					SourceMACs:       []string{"#00:11:22:33:44:55", "00:11:22:33:44:66#Apple TV"},
					Domains:          []string{"www.apple.com#All Apple"},
					DestinationPorts: []string{"tcp:443", "#udp:53"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeAndValidate failed: %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(group.Rules))
	}
	rule := group.Rules[0]
	if len(rule.SourceMACs) != 1 || rule.SourceMACs[0] != "00:11:22:33:44:66" {
		t.Fatalf("unexpected normalized source MACs: %#v", rule.SourceMACs)
	}
	if len(rule.Domains) != 1 || rule.Domains[0] != "www.apple.com" {
		t.Fatalf("unexpected normalized domains: %#v", rule.Domains)
	}
	if rule.RawSelectors == nil || len(rule.RawSelectors.SourceMACs) != 2 {
		t.Fatalf("expected raw selector lines to be preserved: %#v", rule.RawSelectors)
	}
}

func TestNormalizeAndValidateExclusionsDefaultMulticast(t *testing.T) {
	group, err := NormalizeAndValidate(DomainGroup{
		Name:      "ExcludeTest",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:                     "Rule 1",
				SourceCIDRs:              []string{"10.0.0.0/24"},
				ExcludedSourceCIDRs:      []string{"10.0.0.10/32"},
				DestinationCIDRs:         []string{"1.1.1.1/32"},
				ExcludedDestinationCIDRs: []string{"17.0.0.0/8"},
				ExcludedDestinationPorts: []PortRange{{Protocol: "udp", Start: 5353}},
				ExcludedDestinationASNs:  []string{"13335"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeAndValidate failed: %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(group.Rules))
	}
	rule := group.Rules[0]
	if len(rule.ExcludedSourceCIDRs) != 1 || rule.ExcludedSourceCIDRs[0] != "10.0.0.10/32" {
		t.Fatalf("unexpected excluded source CIDRs: %#v", rule.ExcludedSourceCIDRs)
	}
	if len(rule.ExcludedDestinationCIDRs) != 1 || rule.ExcludedDestinationCIDRs[0] != "17.0.0.0/8" {
		t.Fatalf("unexpected excluded destination CIDRs: %#v", rule.ExcludedDestinationCIDRs)
	}
	if len(rule.ExcludedDestinationASNs) != 1 || rule.ExcludedDestinationASNs[0] != "AS13335" {
		t.Fatalf("unexpected excluded ASNs: %#v", rule.ExcludedDestinationASNs)
	}
	if len(rule.ExcludedDestinationPorts) != 1 || rule.ExcludedDestinationPorts[0].Protocol != "udp" || rule.ExcludedDestinationPorts[0].Start != 5353 {
		t.Fatalf("unexpected excluded destination ports: %#v", rule.ExcludedDestinationPorts)
	}
	if !RuleExcludeMulticastEnabled(rule) {
		t.Fatalf("expected excludeMulticast default to true")
	}
}
