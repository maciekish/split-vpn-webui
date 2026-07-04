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
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_streaming_sg_r1d4 dst -j MARK --set-mark 0x169",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_streaming_sg_r1d6 dst -j MARK --set-mark 0x169",
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
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_gaming_r1s4 src -m set --match-set svpn_gaming_r1d4 dst -p tcp --dport 443 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_gaming_r1s6 src -m set --match-set svpn_gaming_r1d6 dst -p tcp --dport 443 -j MARK --set-mark 0x170",
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
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_dnssplit_r1d4 dst -p tcp --dport 53 -j MARK --set-mark 0x170",
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_dnssplit_r1d4 dst -p udp --dport 53 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_dnssplit_r1d6 dst -p tcp --dport 53 -j MARK --set-mark 0x170",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_dnssplit_r1d6 dst -p udp --dport 53 -j MARK --set-mark 0x170",
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
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_landevice_r1d4 dst -i br6 -m mac --mac-source 00:30:93:10:0a:12 -j MARK --set-mark 0x171",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_landevice_r1d6 dst -i br6 -m mac --mac-source 00:30:93:10:0a:12 -j MARK --set-mark 0x171",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected call %q in %#v", expected, calls)
		}
	}
}

func TestApplyRulesAppliesExclusionsAndMulticastByRule(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:                "ExcludePolicy",
			RuleIndex:                0,
			DestinationSetV4:         "svpn_ex_r1d4",
			DestinationSetV6:         "svpn_ex_r1d6",
			ExcludedDestinationSetV4: "svpn_ex_r1xd4",
			ExcludedDestinationSetV6: "svpn_ex_r1xd6",
			HasDestination:           true,
			HasExcludedDestination:   true,
			ExcludedDestinationPorts: []PortRange{{Protocol: "udp", Start: 5353, End: 5353}},
			ExcludeMulticast:         true,
			Mark:                     0x172,
			RouteTable:               204,
			Interface:                "wg-ex",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_ex_r1d4 dst -d 224.0.0.0/4 -j RETURN",
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_ex_r1d4 dst -m set --match-set svpn_ex_r1xd4 dst -j RETURN",
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_ex_r1d4 dst -p udp --dport 5353 -j RETURN",
		"iptables -t mangle -A SVPNA_001_4 -m set --match-set svpn_ex_r1d4 dst -j MARK --set-mark 0x172",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_ex_r1d6 dst -d ff00::/8 -j RETURN",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_ex_r1d6 dst -m set --match-set svpn_ex_r1xd6 dst -j RETURN",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_ex_r1d6 dst -p udp --dport 5353 -j RETURN",
		"ip6tables -t mangle -A SVPNA_001_6 -m set --match-set svpn_ex_r1d6 dst -j MARK --set-mark 0x172",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected call %q in %#v", expected, calls)
		}
	}
}

func TestApplyRulesEmitsMSSClampRules(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "Meta-FRA",
			RuleIndex:        0,
			DestinationSetV4: "svpn_meta_r1d4",
			DestinationSetV6: "svpn_meta_r1d6",
			HasDestination:   true,
			Mark:             0xca,
			RouteTable:       204,
			Interface:        "wg-sv-fra",
			MSSClampV4:       "pmtu",
			MSSClampV6:       "1320",
		},
		{
			// Second rule on the same interface must not duplicate the clamp.
			GroupName:        "Meta-FRA",
			RuleIndex:        1,
			DestinationSetV4: "svpn_meta2_r1d4",
			DestinationSetV6: "svpn_meta2_r1d6",
			HasDestination:   true,
			Mark:             0xca,
			RouteTable:       204,
			Interface:        "wg-sv-fra",
			MSSClampV4:       "pmtu",
			MSSClampV6:       "1320",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// The mock Executor never errors, so idempotent -C checks "succeed" and the
	// -A/-I fallbacks are skipped; assert on the calls that always fire.
	calls := joinCalls(mock.RunCalls)
	for _, expected := range []string{
		"iptables -t mangle -N SVPN_MSS",
		"iptables -t mangle -C FORWARD -j SVPN_MSS",
		"ip6tables -t mangle -C FORWARD -j SVPN_MSS",
		"iptables -t mangle -A SVPN_MSS_A -o wg-sv-fra -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu",
		"ip6tables -t mangle -A SVPN_MSS_A -o wg-sv-fra -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1320",
		"iptables -t mangle -C SVPN_MSS -j SVPN_MSS_A",
	} {
		if !containsCall(calls, expected) {
			t.Fatalf("expected mss call %q in %#v", expected, calls)
		}
	}

	clampCalls := 0
	for _, call := range calls {
		if strings.Contains(call, "-o wg-sv-fra -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu") {
			clampCalls++
		}
	}
	if clampCalls != 1 {
		t.Fatalf("expected exactly one v4 clamp rule for the shared interface, got %d in %#v", clampCalls, calls)
	}
}

func TestApplyRulesOmitsMSSClampWhenDisabled(t *testing.T) {
	mock := &MockExec{}
	manager := NewRuleManager(mock)

	bindings := []RouteBinding{
		{
			GroupName:        "Plain",
			RuleIndex:        0,
			DestinationSetV4: "svpn_plain_r1d4",
			DestinationSetV6: "svpn_plain_r1d6",
			HasDestination:   true,
			Mark:             0xca,
			RouteTable:       204,
			Interface:        "wg-sv-fra",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}
	// The SVPN_MSS chain is always prepared/switched, but with no clamp rules.
	for _, call := range joinCalls(mock.RunCalls) {
		if strings.Contains(call, "-j TCPMSS") {
			t.Fatalf("unexpected TCPMSS rule when clamping disabled: %q", call)
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
		"iptables -t mangle -F SVPN_MSS",
		"ip6tables -t mangle -F SVPN_MSS",
		"iptables -t mangle -F SVPN_MSS_A",
		"iptables -t mangle -F SVPN_MSS_B",
		"ip6tables -t mangle -F SVPN_MSS_A",
		"ip6tables -t mangle -F SVPN_MSS_B",
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
