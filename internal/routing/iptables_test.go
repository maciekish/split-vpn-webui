package routing

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyRulesIncludesIPv4AndIPv6Commands(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "Streaming-SG",
			RuleIndex:        0,
			DestinationSetV4: "svpn_streaming_sg_r1d4",
			DestinationSetV6: "svpn_streaming_sg_r1d6",
			HasDestination:   true,
			Mark:             0x169,
			RouteTable:       201,
			Interface:        "wg-sgp",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	checks := []string{
		"iptables -t mangle -A SVPN_MARK_A -m set --match-set svpn_streaming_sg_r1d4 dst -j MARK --set-mark 0x169",
		"ip6tables -t mangle -A SVPN_MARK_A -m set --match-set svpn_streaming_sg_r1d6 dst -j MARK --set-mark 0x169",
		"iptables -t nat -A SVPN_NAT_A -m mark --mark 0x169 -o wg-sgp -j MASQUERADE",
		"ip6tables -t nat -A SVPN_NAT_A -m mark --mark 0x169 -o wg-sgp -j MASQUERADE",
		"ip rule add fwmark 0x169 table 201 priority 100",
		"ip -6 rule add fwmark 0x169 table 201 priority 100",
	}
	for _, check := range checks {
		if !containsCall(calls, check) {
			t.Fatalf("expected call %q in %#v", check, calls)
		}
	}
}

func TestApplyRulesIsDeterministic(t *testing.T) {
	bindings := []RouteBinding{
		{GroupName: "B", RuleIndex: 1, DestinationSetV4: "svpn_b_r2d4", DestinationSetV6: "svpn_b_r2d6", HasDestination: true, Mark: 205, RouteTable: 205, Interface: "wg-b"},
		{GroupName: "A", RuleIndex: 0, DestinationSetV4: "svpn_a_r1d4", DestinationSetV6: "svpn_a_r1d6", HasDestination: true, Mark: 204, RouteTable: 204, Interface: "wg-a"},
	}

	first := &MockExec{}
	if err := NewRuleManager(first).ApplyRules(bindings); err != nil {
		t.Fatalf("first ApplyRules failed: %v", err)
	}
	second := &MockExec{}
	if err := NewRuleManager(second).ApplyRules(bindings); err != nil {
		t.Fatalf("second ApplyRules failed: %v", err)
	}

	if !reflect.DeepEqual(first.RunCalls, second.RunCalls) {
		t.Fatalf("expected deterministic command order\nfirst: %#v\nsecond: %#v", first.RunCalls, second.RunCalls)
	}
}

func TestApplyRulesIncludesSourceAndPortSelectors(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "Gaming",
			RuleIndex:        0,
			SourceSetV4:      "svpn_gaming_r1s4",
			SourceSetV6:      "svpn_gaming_r1s6",
			DestinationSetV4: "svpn_gaming_r1d4",
			DestinationSetV6: "svpn_gaming_r1d6",
			HasSource:        true,
			HasDestination:   true,
			DestinationPorts: []PortRange{{Protocol: "tcp", Start: 443, End: 443}},
			Mark:             0x170,
			RouteTable:       202,
			Interface:        "wg-gaming",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -A SVPN_MARK_A -m set --match-set svpn_gaming_r1s4 src -m set --match-set svpn_gaming_r1d4 dst -p tcp --dport 443 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPN_MARK_A -m set --match-set svpn_gaming_r1s6 src -m set --match-set svpn_gaming_r1d6 dst -p tcp --dport 443 -j MARK --set-mark 0x170",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected call %q in %#v", expected, calls)
		}
	}
}

func TestApplyRulesExpandsBothProtocolPorts(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "DnsSplit",
			RuleIndex:        0,
			DestinationSetV4: "svpn_dnssplit_r1d4",
			DestinationSetV6: "svpn_dnssplit_r1d6",
			HasDestination:   true,
			DestinationPorts: []PortRange{{Protocol: "both", Start: 53, End: 53}},
			Mark:             0x170,
			RouteTable:       202,
			Interface:        "wg-dns",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -A SVPN_MARK_A -m set --match-set svpn_dnssplit_r1d4 dst -p tcp --dport 53 -j MARK --set-mark 0x170",
		"iptables -t mangle -A SVPN_MARK_A -m set --match-set svpn_dnssplit_r1d4 dst -p udp --dport 53 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPN_MARK_A -m set --match-set svpn_dnssplit_r1d6 dst -p tcp --dport 53 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPN_MARK_A -m set --match-set svpn_dnssplit_r1d6 dst -p udp --dport 53 -j MARK --set-mark 0x170",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected call %q in %#v", expected, calls)
		}
	}
}

func TestApplyRulesIncludesSourceInterfaceAndMACSelectors(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "LanDevice",
			RuleIndex:        0,
			SourceInterfaces: []string{"br6"},
			SourceMACs:       []string{"00:30:93:10:0a:12"},
			DestinationSetV4: "svpn_landevice_r1d4",
			DestinationSetV6: "svpn_landevice_r1d6",
			HasDestination:   true,
			Mark:             0x171,
			RouteTable:       203,
			Interface:        "wg-lan",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -A SVPN_MARK_A -m set --match-set svpn_landevice_r1d4 dst -i br6 -m mac --mac-source 00:30:93:10:0a:12 -j MARK --set-mark 0x171",
		"ip6tables -t mangle -A SVPN_MARK_A -m set --match-set svpn_landevice_r1d6 dst -i br6 -m mac --mac-source 00:30:93:10:0a:12 -j MARK --set-mark 0x171",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected call %q in %#v", expected, calls)
		}
	}
}

func TestFlushRulesRemovesChainsAndManagedRules(t *testing.T) {
	mock := &MockExec{
		Outputs: map[string][]byte{
			"ip rule show":    []byte("100: from all fwmark 0xc9 lookup 201\n"),
			"ip -6 rule show": []byte("100: from all fwmark 0xc9 lookup 201\n"),
		},
	}
	manager := NewRuleManager(mock)

	if err := manager.FlushRules(); err != nil {
		t.Fatalf("FlushRules failed: %v", err)
	}
	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -F SVPN_MARK",
		"ip6tables -t mangle -F SVPN_MARK",
		"iptables -t nat -F SVPN_NAT",
		"ip6tables -t nat -F SVPN_NAT",
		"ip rule del fwmark 0xc9 table 201 priority 100",
		"ip -6 rule del fwmark 0xc9 table 201 priority 100",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected flush call %q in %#v", expected, calls)
		}
	}
}

func joinCalls(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, strings.Join(call, " "))
	}
	return out
}

func containsCall(calls []string, needle string) bool {
	for _, call := range calls {
		if call == needle {
			return true
		}
	}
	return false
}
