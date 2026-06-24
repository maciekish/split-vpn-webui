package prewarm

import (
	"context"
	"testing"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/vpn"
)

func TestWorkerFallsBackToActiveManagedVPNInterfaces(t *testing.T) {
	groups := &mockGroupSource{
		groups: []routing.DomainGroup{
			{Name: "Fallback", EgressVPN: "rbx.contoso.com", Domains: []string{"example.com"}},
		},
	}
	vpns := &mockVPNSource{
		profiles: []*vpn.VPNProfile{
			{Name: "rbx.contoso.com", InterfaceName: "wg-sv-stale"},
		},
	}
	doh := &mockDoH{
		data: map[string][]string{
			"awg-sv-rbxswi|example.com|CNAME":   {},
			"awg-sv-rbxswi|example.com|A":       {"203.0.113.10"},
			"awg-sv-rbxswi|example.com|AAAA":    {},
			"wg-sv-rbxswi9ac|example.com|CNAME": {},
			"wg-sv-rbxswi9ac|example.com|A":     {"203.0.113.20"},
			"wg-sv-rbxswi9ac|example.com|AAAA":  {},
		},
	}
	ipset := &mockIPSet{}

	worker, err := NewWorker(groups, vpns, doh, ipset, WorkerOptions{
		InterfaceActive: func(name string) (bool, error) {
			return name == "awg-sv-rbxswi" || name == "wg-sv-rbxswi9ac", nil
		},
		InterfaceList: func() ([]string, error) {
			return []string{"br0", "awg-sv-rbxswi", "wg-sv-rbxswi9ac"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	stats, err := worker.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if stats.DomainsDone != 1 {
		t.Fatalf("expected one processed domain, got %d", stats.DomainsDone)
	}
	callSet := make(map[string]struct{}, len(doh.calls))
	for _, call := range doh.calls {
		callSet[call] = struct{}{}
	}
	if _, ok := callSet["awg-sv-rbxswi|example.com|A"]; !ok {
		t.Fatalf("expected fallback AmneziaWG interface to be used, calls=%#v", doh.calls)
	}
	if _, ok := callSet["wg-sv-rbxswi9ac|example.com|A"]; !ok {
		t.Fatalf("expected fallback interface to be used, calls=%#v", doh.calls)
	}
}
