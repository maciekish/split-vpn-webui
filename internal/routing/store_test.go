package routing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"split-vpn-webui/internal/database"
)

func newTestStore(t *testing.T) *Store {
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
	return store
}

func TestStoreCRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "wg-sgp",
		Domains:   []string{"*.Example.com", "api.example.com", "example.com"},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("expected created id > 0, got %d", created.ID)
	}
	if len(created.Domains) != 2 {
		t.Fatalf("expected normalized deduped domains length 2, got %d", len(created.Domains))
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if fetched.Name != "Streaming-SG" || fetched.EgressVPN != "wg-sgp" {
		t.Fatalf("unexpected fetched group: %+v", fetched)
	}

	updated, err := store.Update(ctx, created.ID, DomainGroup{
		Name:      "Streaming-EU",
		EgressVPN: "ovpn-web",
		Domains:   []string{"hbo.com", "max.com"},
	})
	if err != nil {
		t.Fatalf("update group: %v", err)
	}
	if updated.Name != "Streaming-EU" {
		t.Fatalf("expected updated name Streaming-EU, got %s", updated.Name)
	}
	if len(updated.Domains) != 2 {
		t.Fatalf("expected updated domains length 2, got %d", len(updated.Domains))
	}

	groups, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound after delete, got %v", err)
	}
}

func TestStoreValidationAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.Create(ctx, DomainGroup{
		Name:      "bad name with spaces",
		EgressVPN: "wg-sgp",
		Domains:   []string{"example.com"},
	}); !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected validation error for bad name, got %v", err)
	}

	if _, err := store.Update(ctx, 9999, DomainGroup{Name: "Gaming", EgressVPN: "wg-sgp", Domains: []string{"example.com"}}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound on update, got %v", err)
	}
	if err := store.Delete(ctx, 9999); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound on delete, got %v", err)
	}
}

func TestStoreReadsLegacyDomainEntriesAsRule(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	result, err := store.db.ExecContext(ctx, `
		INSERT INTO domain_groups (name, egress_vpn) VALUES ('Legacy', 'wg-sgp')
	`)
	if err != nil {
		t.Fatalf("insert group: %v", err)
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO domain_entries (group_id, domain) VALUES (?, 'example.com')
	`, groupID); err != nil {
		t.Fatalf("insert domain entry: %v", err)
	}

	group, err := store.Get(ctx, groupID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one generated legacy rule, got %d", len(group.Rules))
	}
	if len(group.Rules[0].Domains) != 1 || group.Rules[0].Domains[0] != "example.com" {
		t.Fatalf("unexpected legacy rule domains: %#v", group.Rules[0].Domains)
	}
}

func TestStorePersistsSourceInterfaceAndMACSelectors(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, DomainGroup{
		Name:      "LanPolicy",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:             "MAC+Port",
				SourceInterfaces: []string{"br6", "br0"},
				SourceMACs:       []string{"00:30:93:10:0a:12", "00:30:93:10:0a:12"},
				DestinationPorts: []PortRange{{Protocol: "both", Start: 53}},
				DestinationCIDRs: []string{"1.1.1.1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if len(fetched.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(fetched.Rules))
	}
	rule := fetched.Rules[0]
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

func TestStoreAllowsExactAndWildcardSelectorsInSameRule(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, DomainGroup{
		Name:      "DomainOverlap",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:            "Mixed Domains",
				Domains:         []string{"domain.com", "asdf.domain.com"},
				WildcardDomains: []string{"*.domain.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create group with mixed selectors: %v", err)
	}
	if len(created.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(created.Rules))
	}
	rule := created.Rules[0]
	if len(rule.Domains) != 2 || rule.Domains[0] != "domain.com" || rule.Domains[1] != "asdf.domain.com" {
		t.Fatalf("unexpected exact domains: %#v", rule.Domains)
	}
	if len(rule.WildcardDomains) != 1 || rule.WildcardDomains[0] != "*.domain.com" {
		t.Fatalf("unexpected wildcard domains: %#v", rule.WildcardDomains)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if len(fetched.Rules) != 1 {
		t.Fatalf("expected one fetched rule, got %d", len(fetched.Rules))
	}
	fetchedRule := fetched.Rules[0]
	if len(fetchedRule.Domains) != 2 || fetchedRule.Domains[0] != "domain.com" || fetchedRule.Domains[1] != "asdf.domain.com" {
		t.Fatalf("unexpected fetched exact domains: %#v", fetchedRule.Domains)
	}
	if len(fetchedRule.WildcardDomains) != 1 || fetchedRule.WildcardDomains[0] != "*.domain.com" {
		t.Fatalf("unexpected fetched wildcard domains: %#v", fetchedRule.WildcardDomains)
	}
}

func TestStorePersistsRawSelectorLinesWithComments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, DomainGroup{
		Name:      "Commented",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:    "Rule 1",
				Domains: []string{"www.apple.com"},
				RawSelectors: &RuleRawSelectors{
					SourceMACs:       []string{"#00:11:22:33:44:55", "00:11:22:33:44:66#Apple TV"},
					Domains:          []string{"www.apple.com#enabled"},
					WildcardDomains:  []string{"#*.contoso.com"},
					DestinationPorts: []string{"tcp:443", "#udp:53"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if len(fetched.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(fetched.Rules))
	}
	raw := fetched.Rules[0].RawSelectors
	if raw == nil {
		t.Fatalf("expected raw selectors")
	}
	if len(raw.SourceMACs) != 2 || raw.SourceMACs[0] != "#00:11:22:33:44:55" || raw.SourceMACs[1] != "00:11:22:33:44:66#Apple TV" {
		t.Fatalf("unexpected source mac raw lines: %#v", raw.SourceMACs)
	}
	if len(raw.DestinationPorts) != 2 || raw.DestinationPorts[1] != "#udp:53" {
		t.Fatalf("unexpected destination port raw lines: %#v", raw.DestinationPorts)
	}
}

func TestStorePersistsExclusionSelectorsAndMulticastFlag(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	disabled := false

	created, err := store.Create(ctx, DomainGroup{
		Name:      "ExcludeRoundTrip",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{
			{
				Name:                     "Rule 1",
				SourceCIDRs:              []string{"10.0.0.0/24"},
				ExcludedSourceCIDRs:      []string{"10.0.0.10/32"},
				DestinationCIDRs:         []string{"1.1.1.0/24"},
				ExcludedDestinationCIDRs: []string{"17.0.0.0/8"},
				DestinationPorts:         []PortRange{{Protocol: "tcp", Start: 443}},
				ExcludedDestinationPorts: []PortRange{{Protocol: "udp", Start: 5353}},
				DestinationASNs:          []string{"AS15169"},
				ExcludedDestinationASNs:  []string{"AS13335"},
				ExcludeMulticast:         &disabled,
				RawSelectors: &RuleRawSelectors{
					ExcludedSourceCIDRs:      []string{"10.0.0.10/32#bypass host"},
					ExcludedDestinationCIDRs: []string{"17.0.0.0/8#bypass apple"},
					ExcludedDestinationPorts: []string{"udp:5353#mdns"},
					ExcludedDestinationASNs:  []string{"AS13335#cloudflare"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if len(fetched.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(fetched.Rules))
	}
	rule := fetched.Rules[0]
	if len(rule.ExcludedSourceCIDRs) != 1 || rule.ExcludedSourceCIDRs[0] != "10.0.0.10/32" {
		t.Fatalf("unexpected excluded source CIDRs: %#v", rule.ExcludedSourceCIDRs)
	}
	if len(rule.ExcludedDestinationCIDRs) != 1 || rule.ExcludedDestinationCIDRs[0] != "17.0.0.0/8" {
		t.Fatalf("unexpected excluded destination CIDRs: %#v", rule.ExcludedDestinationCIDRs)
	}
	if len(rule.ExcludedDestinationPorts) != 1 || rule.ExcludedDestinationPorts[0].Protocol != "udp" || rule.ExcludedDestinationPorts[0].Start != 5353 {
		t.Fatalf("unexpected excluded destination ports: %#v", rule.ExcludedDestinationPorts)
	}
	if len(rule.ExcludedDestinationASNs) != 1 || rule.ExcludedDestinationASNs[0] != "AS13335" {
		t.Fatalf("unexpected excluded destination ASNs: %#v", rule.ExcludedDestinationASNs)
	}
	if RuleExcludeMulticastEnabled(rule) {
		t.Fatalf("expected excludeMulticast to persist disabled")
	}
	if rule.RawSelectors == nil || len(rule.RawSelectors.ExcludedDestinationPorts) != 1 || rule.RawSelectors.ExcludedDestinationPorts[0] != "udp:5353#mdns" {
		t.Fatalf("unexpected raw excluded destination port lines: %#v", rule.RawSelectors)
	}
}
