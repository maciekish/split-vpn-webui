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
			GroupName:  "Streaming-SG",
			SetV4:      "svpn_streaming_sg_v4",
			SetV6:      "svpn_streaming_sg_v6",
			Mark:       0x169,
			RouteTable: 201,
			Interface:  "wg-sgp",
		},
	}
	if err := manager.ApplyRules(bindings); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	calls := joinCalls(mock.RunCalls)
	checks := []string{
		"iptables -t mangle -A SVPN_MARK -m set --match-set svpn_streaming_sg_v4 dst -j MARK --set-mark 0x169",
		"ip6tables -t mangle -A SVPN_MARK -m set --match-set svpn_streaming_sg_v6 dst -j MARK --set-mark 0x169",
		"iptables -t nat -A SVPN_NAT -m mark --mark 0x169 -o wg-sgp -j MASQUERADE",
		"ip6tables -t nat -A SVPN_NAT -m mark --mark 0x169 -o wg-sgp -j MASQUERADE",
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
		{GroupName: "B", SetV4: "svpn_b_v4", SetV6: "svpn_b_v6", Mark: 205, RouteTable: 205, Interface: "wg-b"},
		{GroupName: "A", SetV4: "svpn_a_v4", SetV6: "svpn_a_v6", Mark: 204, RouteTable: 204, Interface: "wg-a"},
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
