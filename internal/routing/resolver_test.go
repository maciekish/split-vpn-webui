package routing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"split-vpn-webui/internal/database"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/vpn"
)

type fakeDomainResolver struct {
	values map[string]ResolverValues
}

func (f *fakeDomainResolver) Resolve(ctx context.Context, domain string) (ResolverValues, error) {
	return f.values[domain], nil
}

type fakeASNResolver struct {
	values map[string]ResolverValues
}

func (f *fakeASNResolver) Resolve(ctx context.Context, asn string) (ResolverValues, error) {
	return f.values[asn], nil
}

type fakeWildcardResolver struct {
	values map[string][]string
}

func (f *fakeWildcardResolver) Resolve(ctx context.Context, wildcard string) ([]string, error) {
	return append([]string(nil), f.values[wildcard]...), nil
}

func TestCollectResolverJobsDedupesSelectors(t *testing.T) {
	groups := []DomainGroup{
		{
			Name: "A",
			Rules: []RoutingRule{
				{Domains: []string{"example.com"}, WildcardDomains: []string{"*.apple.com"}, DestinationASNs: []string{"AS13335"}},
				{Domains: []string{"example.com"}, WildcardDomains: []string{"*.apple.com"}, DestinationASNs: []string{"13335"}},
			},
		},
	}
	jobs := collectResolverJobs(groups)
	if len(jobs) != 3 {
		t.Fatalf("expected 3 deduped jobs, got %d (%#v)", len(jobs), jobs)
	}
}

func TestResolverSchedulerRunUpdatesSnapshotAndReappliesRouting(t *testing.T) {
	manager, ipset, _, rules := newRoutingTestManager(t, &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}})

	ctx := context.Background()
	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Streaming",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Domains:         []string{"example.com"},
			WildcardDomains: []string{"*.apple.com"},
			DestinationASNs: []string{"AS13335"},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	settingsManager := settings.NewManager(settingsPath)
	if err := settingsManager.Save(settings.Settings{
		ResolverParallelism:     2,
		ResolverTimeoutSeconds:  5,
		ResolverIntervalSeconds: 300,
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	scheduler, err := NewResolverSchedulerWithDeps(
		manager,
		settingsManager,
		&fakeDomainResolver{values: map[string]ResolverValues{
			"example.com":   {V4: []string{"1.1.1.1/32"}},
			"api.apple.com": {V4: []string{"17.253.144.10/32"}},
		}},
		&fakeASNResolver{values: map[string]ResolverValues{
			"AS13335": {V4: []string{"104.16.0.0/12"}, V6: []string{"2400:cb00::/32"}},
		}},
		&fakeWildcardResolver{values: map[string][]string{
			"*.apple.com": {"api.apple.com"},
		}},
	)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}

	if err := scheduler.TriggerNow(); err != nil {
		t.Fatalf("TriggerNow failed: %v", err)
	}
	waitResolverIdle(t, scheduler)

	status, err := scheduler.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.LastRun == nil {
		t.Fatalf("expected last run to be populated")
	}
	if status.LastRun.SelectorsTotal != 3 {
		t.Fatalf("expected 3 selectors total, got %d", status.LastRun.SelectorsTotal)
	}

	snapshot, err := manager.store.LoadResolverSnapshot(ctx)
	if err != nil {
		t.Fatalf("LoadResolverSnapshot failed: %v", err)
	}
	if len(snapshot) != 3 {
		t.Fatalf("expected 3 selectors in snapshot, got %d", len(snapshot))
	}

	sets := RuleSetNames("Streaming", 0)
	v4Entries := ipset.IPs[sets.DestinationV4]
	if len(v4Entries) == 0 {
		t.Fatalf("expected destination v4 set entries after resolver run")
	}
	if rules.applyCount < 2 {
		t.Fatalf("expected routing rules to be applied at least twice (create + resolver), got %d", rules.applyCount)
	}
}

func waitResolverIdle(t *testing.T, scheduler *ResolverScheduler) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := scheduler.Status(context.Background())
		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}
		if !status.Running {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("resolver scheduler did not complete in time")
}

func TestResolverSnapshotReplaceRemovesStaleValues(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "resolver.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	ctx := context.Background()
	first := map[ResolverSelector]ResolverValues{
		{Type: "domain", Key: "example.com"}: {V4: []string{"1.1.1.1/32"}},
	}
	if err := store.ReplaceResolverSnapshot(ctx, first); err != nil {
		t.Fatalf("replace first snapshot: %v", err)
	}
	second := map[ResolverSelector]ResolverValues{
		{Type: "asn", Key: "AS13335"}: {V4: []string{"104.16.0.0/12"}},
	}
	if err := store.ReplaceResolverSnapshot(ctx, second); err != nil {
		t.Fatalf("replace second snapshot: %v", err)
	}

	loaded, err := store.LoadResolverSnapshot(ctx)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if _, exists := loaded[ResolverSelector{Type: "domain", Key: "example.com"}]; exists {
		t.Fatalf("expected stale domain selector to be removed")
	}
	if _, exists := loaded[ResolverSelector{Type: "asn", Key: "AS13335"}]; !exists {
		t.Fatalf("expected updated selector to be present")
	}
}
