package routing

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"split-vpn-webui/internal/database"
	"split-vpn-webui/internal/vpn"
)

type mockDNSManager struct {
	lastGenerated []DomainGroup
	lastWritten   string
	writeCount    int
	reloadCount   int
	err           error
}

func (m *mockDNSManager) GenerateDnsmasqConf(groups []DomainGroup) string {
	m.lastGenerated = append([]DomainGroup(nil), groups...)
	builder := "# test\n"
	for _, g := range groups {
		v4, v6 := GroupSetNames(g.Name)
		for _, d := range g.Domains {
			builder += "ipset=/" + d + "/" + v4 + "," + v6 + "\n"
		}
	}
	return builder
}

func (m *mockDNSManager) WriteDnsmasqConf(content string) error {
	m.writeCount++
	m.lastWritten = content
	return m.err
}

func (m *mockDNSManager) ReloadDnsmasq() error {
	m.reloadCount++
	return m.err
}

type mockRuleApplier struct {
	applyCount int
	flushCount int
	bindings   []RouteBinding
	err        error
}

func (m *mockRuleApplier) ApplyRules(bindings []RouteBinding) error {
	m.applyCount++
	m.bindings = append([]RouteBinding(nil), bindings...)
	return m.err
}

func (m *mockRuleApplier) FlushRules() error {
	m.flushCount++
	return m.err
}

type mockVPNLister struct {
	profiles []*vpn.VPNProfile
	err      error
}

func (m *mockVPNLister) List() ([]*vpn.VPNProfile, error) {
	if m.err != nil {
		return nil, m.err
	}
	return append([]*vpn.VPNProfile(nil), m.profiles...), nil
}

func newRoutingTestManager(t *testing.T, lister VPNLister) (*Manager, *MockIPSet, *mockDNSManager, *mockRuleApplier) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ipset := &MockIPSet{Sets: map[string]string{}}
	dns := &mockDNSManager{}
	rules := &mockRuleApplier{}
	manager, err := NewManagerWithDeps(store, ipset, dns, rules, lister)
	if err != nil {
		t.Fatalf("new manager with deps: %v", err)
	}
	return manager, ipset, dns, rules
}

func TestManagerCreateGroupAppliesRoutingState(t *testing.T) {
	ctx := context.Background()
	manager, ipset, dns, rules := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}})

	group, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "wg-sgp",
		Domains:   []string{"max.com", "hbo.com"},
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if group.ID <= 0 {
		t.Fatalf("expected created id > 0")
	}

	v4, v6 := GroupSetNames("Streaming-SG")
	if _, ok := ipset.Sets[v4]; !ok {
		t.Fatalf("expected ipset %s to be ensured", v4)
	}
	if _, ok := ipset.Sets[v6]; !ok {
		t.Fatalf("expected ipset %s to be ensured", v6)
	}
	if rules.applyCount != 1 {
		t.Fatalf("expected ApplyRules once, got %d", rules.applyCount)
	}
	if len(rules.bindings) != 1 {
		t.Fatalf("expected one route binding, got %d", len(rules.bindings))
	}
	if rules.bindings[0].Mark != 0x169 || rules.bindings[0].RouteTable != 201 {
		t.Fatalf("unexpected binding: %+v", rules.bindings[0])
	}
	if dns.writeCount != 1 || dns.reloadCount != 1 {
		t.Fatalf("expected dnsmasq write+reload once, got write=%d reload=%d", dns.writeCount, dns.reloadCount)
	}
}

func TestManagerDeleteLastGroupFlushesRules(t *testing.T) {
	ctx := context.Background()
	manager, _, _, rules := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}})

	group, err := manager.CreateGroup(ctx, DomainGroup{Name: "Gaming", EgressVPN: "wg-sgp", Domains: []string{"roblox.com"}})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := manager.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}
	if rules.flushCount == 0 {
		t.Fatalf("expected FlushRules to be called when no groups remain")
	}
}

func TestManagerApplyFailsForMissingVPN(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _ := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{}})

	if _, err := manager.store.Create(ctx, DomainGroup{Name: "Streaming-SG", EgressVPN: "wg-sgp", Domains: []string{"max.com"}}); err != nil {
		t.Fatalf("store.Create failed: %v", err)
	}
	if err := manager.Apply(ctx); err == nil {
		t.Fatalf("expected Apply to fail when egress vpn is missing")
	}
}

func TestManagerCreateGroupRejectsUnknownEgressBeforePersist(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _ := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{}})

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "missing-vpn",
		Domains:   []string{"max.com"},
	}); err == nil || !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected ErrGroupValidation, got: %v", err)
	}

	groups, err := manager.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected no persisted groups after validation failure, got %d", len(groups))
	}
}

func TestManagerApplyDestroysStaleSetsAfterRulesApply(t *testing.T) {
	ctx := context.Background()
	applied := false
	ipset := &orderedIPSetMock{
		sets: map[string]string{
			"svpn_old_v4": "inet",
			"svpn_old_v6": "inet6",
		},
		beforeDestroy: func() error {
			if !applied {
				return fmt.Errorf("destroy called before rules apply")
			}
			return nil
		},
	}
	rules := &orderedRuleApplierMock{
		onApply: func() {
			applied = true
		},
	}
	manager := newRoutingTestManagerWithDeps(t, ipset, &mockDNSManager{}, rules, &mockVPNLister{
		profiles: []*vpn.VPNProfile{{
			Name:          "wg-sgp",
			RouteTable:    201,
			FWMark:        0x169,
			InterfaceName: "wg-sgp",
		}},
	})

	if _, err := manager.store.Create(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "wg-sgp",
		Domains:   []string{"max.com"},
	}); err != nil {
		t.Fatalf("seed group failed: %v", err)
	}
	if err := manager.Apply(ctx); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !rules.applyCalled {
		t.Fatalf("expected ApplyRules to be called")
	}
	if containsString(ipset.destroyed, "svpn_old_v4") == false || containsString(ipset.destroyed, "svpn_old_v6") == false {
		t.Fatalf("expected stale sets to be destroyed, destroyed=%v", ipset.destroyed)
	}
}

func TestManagerApplyFlushesRulesBeforeDestroyWhenNoGroups(t *testing.T) {
	ctx := context.Background()
	flushed := false
	ipset := &orderedIPSetMock{
		sets: map[string]string{
			"svpn_old_v4": "inet",
			"svpn_old_v6": "inet6",
		},
		beforeDestroy: func() error {
			if !flushed {
				return fmt.Errorf("destroy called before rules flush")
			}
			return nil
		},
	}
	rules := &orderedRuleApplierMock{
		onFlush: func() {
			flushed = true
		},
	}
	manager := newRoutingTestManagerWithDeps(t, ipset, &mockDNSManager{}, rules, &mockVPNLister{})

	if err := manager.Apply(ctx); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !rules.flushCalled {
		t.Fatalf("expected FlushRules to be called")
	}
	if len(ipset.destroyed) != 2 {
		t.Fatalf("expected both stale sets to be destroyed, got %v", ipset.destroyed)
	}
}

func TestManagerApplySerializesConcurrentCalls(t *testing.T) {
	ctx := context.Background()
	rules := &concurrencyRuleApplier{}
	manager, _, _, _ := newRoutingTestManager(t, &mockVPNLister{
		profiles: []*vpn.VPNProfile{{
			Name:          "wg-sgp",
			RouteTable:    201,
			FWMark:        0x169,
			InterfaceName: "wg-sgp",
		}},
	})
	manager.rules = rules

	if _, err := manager.store.Create(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "wg-sgp",
		Domains:   []string{"max.com"},
	}); err != nil {
		t.Fatalf("seed group failed: %v", err)
	}

	const workers = 6
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errCh <- manager.Apply(ctx)
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
	}
	if rules.maxInFlight != 1 {
		t.Fatalf("expected serialized Apply calls, max in-flight=%d", rules.maxInFlight)
	}
}

func TestManagerCreateGroupSupportsSourceInterfaceAndMACSelectors(t *testing.T) {
	ctx := context.Background()
	manager, ipset, _, rules := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}})

	_, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "LAN-Only",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:             "Device",
				SourceInterfaces: []string{"br6"},
				SourceMACs:       []string{"00:30:93:10:0a:12"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if rules.applyCount != 1 || len(rules.bindings) != 1 {
		t.Fatalf("expected one binding apply call, apply=%d bindings=%d", rules.applyCount, len(rules.bindings))
	}
	binding := rules.bindings[0]
	if len(binding.SourceInterfaces) != 1 || binding.SourceInterfaces[0] != "br6" {
		t.Fatalf("unexpected source interfaces in binding: %#v", binding.SourceInterfaces)
	}
	if len(binding.SourceMACs) != 1 || binding.SourceMACs[0] != "00:30:93:10:0a:12" {
		t.Fatalf("unexpected source macs in binding: %#v", binding.SourceMACs)
	}
	if binding.HasSource {
		t.Fatalf("expected HasSource=false when no source CIDRs are configured")
	}
	if len(ipset.Sets) != 0 {
		t.Fatalf("expected no ipsets for interface/mac-only selector, got %#v", ipset.Sets)
	}
}

func TestManagerUpsertPrewarmSnapshotUpdatesDestinationSetsWithoutRuleReapply(t *testing.T) {
	ctx := context.Background()
	manager, ipset, _, rules := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}})

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Streaming",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Domains: []string{"example.com"},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if rules.applyCount != 1 {
		t.Fatalf("expected one initial rule apply, got %d", rules.applyCount)
	}

	sets := RuleSetNames("Streaming", 0)
	if err := manager.UpsertPrewarmSnapshot(ctx, map[string]ResolverValues{
		sets.DestinationV4: {V4: []string{"203.0.113.10/32"}},
	}); err != nil {
		t.Fatalf("UpsertPrewarmSnapshot failed: %v", err)
	}
	if rules.applyCount != 1 {
		t.Fatalf("expected no extra rule apply during prewarm cache update, got %d", rules.applyCount)
	}
	if !containsString(ipset.IPs[sets.DestinationV4], "203.0.113.10/32") {
		t.Fatalf("expected destination set to include prewarm prefix, got %#v", ipset.IPs[sets.DestinationV4])
	}

	loaded, err := manager.LoadPrewarmSnapshot(ctx)
	if err != nil {
		t.Fatalf("LoadPrewarmSnapshot failed: %v", err)
	}
	if len(loaded[sets.DestinationV4].V4) != 1 || loaded[sets.DestinationV4].V4[0] != "203.0.113.10/32" {
		t.Fatalf("unexpected loaded prewarm snapshot: %#v", loaded[sets.DestinationV4].V4)
	}
}
