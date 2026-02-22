package prewarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/vpn"
)

type mockGroupSource struct {
	groups []routing.DomainGroup
	err    error
}

func (m *mockGroupSource) ListGroups(ctx context.Context) ([]routing.DomainGroup, error) {
	if m.err != nil {
		return nil, m.err
	}
	return append([]routing.DomainGroup(nil), m.groups...), nil
}

type mockVPNSource struct {
	profiles []*vpn.VPNProfile
	err      error
}

func (m *mockVPNSource) List() ([]*vpn.VPNProfile, error) {
	if m.err != nil {
		return nil, m.err
	}
	return append([]*vpn.VPNProfile(nil), m.profiles...), nil
}

type mockDoH struct {
	mu    sync.Mutex
	data  map[string][]string
	calls []string
}

func (m *mockDoH) QueryA(ctx context.Context, domain, iface string) ([]string, error) {
	return m.query(ctx, "A", domain, iface)
}

func (m *mockDoH) QueryAAAA(ctx context.Context, domain, iface string) ([]string, error) {
	return m.query(ctx, "AAAA", domain, iface)
}

func (m *mockDoH) QueryCNAME(ctx context.Context, domain, iface string) ([]string, error) {
	return m.query(ctx, "CNAME", domain, iface)
}

func (m *mockDoH) query(ctx context.Context, qType, domain, iface string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s|%s|%s", strings.ToLower(strings.TrimSpace(iface)), strings.ToLower(strings.TrimSpace(domain)), qType)
	m.mu.Lock()
	m.calls = append(m.calls, key)
	values := append([]string(nil), m.data[key]...)
	m.mu.Unlock()
	return values, nil
}

type mockIPSet struct {
	mu    sync.Mutex
	added map[string][]string
}

func (m *mockIPSet) EnsureSet(name, family string) error { return nil }

func (m *mockIPSet) AddIP(setName, ip string, timeoutSeconds int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.added == nil {
		m.added = map[string][]string{}
	}
	m.added[setName] = append(m.added[setName], ip)
	return nil
}

func (m *mockIPSet) FlushSet(name string) error { return nil }

func (m *mockIPSet) DestroySet(name string) error { return nil }

func (m *mockIPSet) ListSets(prefix string) ([]string, error) { return nil, nil }

func TestWorkerQueriesAllActiveVPNInterfacesAndAddsIPs(t *testing.T) {
	groups := &mockGroupSource{
		groups: []routing.DomainGroup{
			{Name: "Streaming-SG", EgressVPN: "wg-a", Domains: []string{"max.com"}},
		},
	}
	vpns := &mockVPNSource{
		profiles: []*vpn.VPNProfile{
			{Name: "wg-a", InterfaceName: "wg-a"},
			{Name: "wg-b", InterfaceName: "wg-b"},
		},
	}
	doh := &mockDoH{
		data: map[string][]string{
			"wg-a|max.com|CNAME":     {"edge.max.com."},
			"wg-b|max.com|CNAME":     {},
			"wg-a|max.com|A":         {"1.1.1.1"},
			"wg-b|max.com|A":         {"1.1.1.2"},
			"wg-a|max.com|AAAA":      {},
			"wg-b|max.com|AAAA":      {},
			"wg-a|edge.max.com|A":    {"1.1.1.1"},
			"wg-b|edge.max.com|A":    {"1.1.1.3"},
			"wg-a|edge.max.com|AAAA": {},
			"wg-b|edge.max.com|AAAA": {"2001:db8::1"},
		},
	}
	ipset := &mockIPSet{}

	worker, err := NewWorker(groups, vpns, doh, ipset, WorkerOptions{
		Parallelism: 2,
		InterfaceActive: func(name string) (bool, error) {
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	stats, err := worker.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if stats.DomainsTotal != 1 || stats.DomainsDone != 1 {
		t.Fatalf("unexpected domain stats: %+v", stats)
	}
	if stats.IPsInserted != 4 {
		t.Fatalf("expected 4 unique IPs inserted, got %d", stats.IPsInserted)
	}

	v4Set, v6Set := routing.GroupSetNames("Streaming-SG")
	gotV4 := append([]string(nil), ipset.added[v4Set]...)
	gotV6 := append([]string(nil), ipset.added[v6Set]...)
	sort.Strings(gotV4)
	sort.Strings(gotV6)
	if strings.Join(gotV4, ",") != "1.1.1.1,1.1.1.2,1.1.1.3" {
		t.Fatalf("unexpected v4 insertions: %#v", gotV4)
	}
	if strings.Join(gotV6, ",") != "2001:db8::1" {
		t.Fatalf("unexpected v6 insertions: %#v", gotV6)
	}

	callSet := make(map[string]struct{}, len(doh.calls))
	for _, call := range doh.calls {
		callSet[call] = struct{}{}
	}
	for _, expected := range []string{
		"wg-a|max.com|CNAME",
		"wg-b|max.com|CNAME",
		"wg-a|max.com|A",
		"wg-b|max.com|A",
		"wg-a|edge.max.com|A",
		"wg-b|edge.max.com|A",
	} {
		if _, ok := callSet[expected]; !ok {
			t.Fatalf("expected DoH call %q; calls=%#v", expected, doh.calls)
		}
	}
}

func TestWorkerRespectsContextCancellation(t *testing.T) {
	groups := &mockGroupSource{
		groups: []routing.DomainGroup{
			{Name: "Gaming", EgressVPN: "wg-a", Domains: []string{"roblox.com"}},
		},
	}
	vpns := &mockVPNSource{
		profiles: []*vpn.VPNProfile{{Name: "wg-a", InterfaceName: "wg-a"}},
	}
	doh := &mockDoH{data: map[string][]string{}}
	ipset := &mockIPSet{}

	worker, err := NewWorker(groups, vpns, doh, ipset, WorkerOptions{
		InterfaceActive: func(name string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := worker.Run(ctx); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestWorkerFallsBackToActiveManagedWireGuardInterfaces(t *testing.T) {
	groups := &mockGroupSource{
		groups: []routing.DomainGroup{
			{Name: "Fallback", EgressVPN: "rbx.swic.name", Domains: []string{"example.com"}},
		},
	}
	vpns := &mockVPNSource{
		profiles: []*vpn.VPNProfile{
			{Name: "rbx.swic.name", InterfaceName: "wg-sv-stale"},
		},
	}
	doh := &mockDoH{
		data: map[string][]string{
			"wg-sv-rbxswi9ac|example.com|CNAME": {},
			"wg-sv-rbxswi9ac|example.com|A":     {"203.0.113.10"},
			"wg-sv-rbxswi9ac|example.com|AAAA":  {},
		},
	}
	ipset := &mockIPSet{}

	worker, err := NewWorker(groups, vpns, doh, ipset, WorkerOptions{
		InterfaceActive: func(name string) (bool, error) {
			return name == "wg-sv-rbxswi9ac", nil
		},
		InterfaceList: func() ([]string, error) {
			return []string{"br0", "wg-sv-rbxswi9ac"}, nil
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
	if _, ok := callSet["wg-sv-rbxswi9ac|example.com|A"]; !ok {
		t.Fatalf("expected fallback interface to be used, calls=%#v", doh.calls)
	}
}
