package routing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"split-vpn-webui/internal/vpn"
)

type mockRemoteListProvider struct {
	contents map[string]RemoteListContent
	names    map[string]struct{}
	err      error
	calls    int
}

func (m *mockRemoteListProvider) RemoteListContents(ctx context.Context) (map[string]RemoteListContent, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make(map[string]RemoteListContent, len(m.contents))
	for key, value := range m.contents {
		out[key] = RemoteListContent{Kind: value.Kind, Entries: append([]string(nil), value.Entries...)}
	}
	return out, nil
}

func (m *mockRemoteListProvider) RemoteListNames(ctx context.Context) (map[string]struct{}, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.names != nil {
		out := make(map[string]struct{}, len(m.names))
		for key := range m.names {
			out[key] = struct{}{}
		}
		return out, nil
	}
	names := make(map[string]struct{}, len(m.contents))
	for key := range m.contents {
		names[key] = struct{}{}
	}
	return names, nil
}

func remoteListTestLister() *mockVPNLister {
	return &mockVPNLister{profiles: []*vpn.VPNProfile{{
		Name:          "wg-sgp",
		RouteTable:    201,
		FWMark:        0x169,
		InterfaceName: "wg-sgp",
	}}}
}

func TestNormalizeRemoteListValuesSkipsUnusableEntries(t *testing.T) {
	entries, skipped, err := NormalizeRemoteListValues(RemoteListKindCIDR, []string{
		"91.108.56.0/22", "  ", "bogus", "91.108.56.0/22", "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !reflect.DeepEqual(entries, []string{"1.2.3.4/32", "91.108.56.0/22"}) {
		t.Fatalf("entries = %v", entries)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if _, _, err := NormalizeRemoteListValues("nope", []string{"1.2.3.4"}); err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestListGroupsExpandsRemoteListsByKind(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _ := newRoutingTestManager(t, remoteListTestLister())
	provider := &mockRemoteListProvider{contents: map[string]RemoteListContent{
		"telegram":  {Kind: RemoteListKindCIDR, Entries: []string{"91.108.56.0/22", "2001:b28:f23d::/48"}},
		"cdn-asns":  {Kind: RemoteListKindASN, Entries: []string{"AS13335"}},
		"streaming": {Kind: RemoteListKindDomain, Entries: []string{"api.example.com"}},
		"wilds":     {Kind: RemoteListKindWildcard, Entries: []string{"*.example.net"}},
	}}
	manager.SetRemoteListProvider(provider)

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Lists",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Name:             "Rule 1",
			DestinationCIDRs: []string{"10.10.0.0/16"},
			RemoteLists:      []string{"Telegram", "cdn-asns", "streaming", "wilds"},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	raw, err := manager.ListGroupsRaw(ctx)
	if err != nil {
		t.Fatalf("ListGroupsRaw: %v", err)
	}
	if got := raw[0].Rules[0].DestinationCIDRs; !reflect.DeepEqual(got, []string{"10.10.0.0/16"}) {
		t.Fatalf("raw groups must not be expanded, got %v", got)
	}
	if got := raw[0].Rules[0].RemoteLists; !reflect.DeepEqual(got, []string{"Telegram", "cdn-asns", "streaming", "wilds"}) {
		t.Fatalf("raw remote list references = %v", got)
	}

	expanded, err := manager.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	rule := expanded[0].Rules[0]
	wantCIDRs := []string{"10.10.0.0/16", "91.108.56.0/22", "2001:b28:f23d::/48"}
	if !reflect.DeepEqual(rule.DestinationCIDRs, wantCIDRs) {
		t.Fatalf("destination cidrs = %v, want %v", rule.DestinationCIDRs, wantCIDRs)
	}
	if !reflect.DeepEqual(rule.DestinationASNs, []string{"AS13335"}) {
		t.Fatalf("destination asns = %v", rule.DestinationASNs)
	}
	if !reflect.DeepEqual(rule.Domains, []string{"api.example.com"}) {
		t.Fatalf("domains = %v", rule.Domains)
	}
	if !reflect.DeepEqual(rule.WildcardDomains, []string{"*.example.net"}) {
		t.Fatalf("wildcard domains = %v", rule.WildcardDomains)
	}
}

func TestApplyPopulatesIPSetsFromRemoteList(t *testing.T) {
	ctx := context.Background()
	manager, ipset, dns, rules := newRoutingTestManager(t, remoteListTestLister())
	provider := &mockRemoteListProvider{contents: map[string]RemoteListContent{
		"telegram": {Kind: RemoteListKindCIDR, Entries: []string{"91.108.56.0/22", "2001:b28:f23d::/48"}},
		"hosts":    {Kind: RemoteListKindDomain, Entries: []string{"api.example.com"}},
	}}
	manager.SetRemoteListProvider(provider)

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Lists",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Name:        "Rule 1",
			RemoteLists: []string{"telegram", "hosts"},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	pair := RuleSetNames("Lists", 0)
	if got := ipset.IPs[pair.DestinationV4]; !reflect.DeepEqual(got, []string{"91.108.56.0/22"}) {
		t.Fatalf("v4 set = %v", got)
	}
	if got := ipset.IPs[pair.DestinationV6]; !reflect.DeepEqual(got, []string{"2001:b28:f23d::/48"}) {
		t.Fatalf("v6 set = %v", got)
	}
	if len(rules.bindings) != 1 || !rules.bindings[0].HasDestination {
		t.Fatalf("expected one binding with a destination set, got %+v", rules.bindings)
	}
	if !strings.Contains(dns.lastWritten, "api.example.com") {
		t.Fatalf("dnsmasq config missing remote list domain:\n%s", dns.lastWritten)
	}

	// Removing an entry upstream must shrink the set on the next apply.
	provider.contents["telegram"] = RemoteListContent{
		Kind:    RemoteListKindCIDR,
		Entries: []string{"2001:b28:f23d::/48"},
	}
	if err := manager.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := ipset.IPs[pair.DestinationV4]; len(got) != 0 {
		t.Fatalf("stale v4 entries survived a remote list change: %v", got)
	}
}

// A rule whose only destination selector is a remote list that has not been
// fetched must still produce an (empty) destination set, so it matches nothing
// instead of diverting every packet from the rule's source.
func TestEmptyRemoteListKeepsRuleFailClosed(t *testing.T) {
	ctx := context.Background()
	manager, ipset, _, rules := newRoutingTestManager(t, remoteListTestLister())
	manager.SetRemoteListProvider(&mockRemoteListProvider{contents: map[string]RemoteListContent{
		"pending": {Kind: RemoteListKindCIDR},
	}})

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Pending",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Name:             "Rule 1",
			SourceInterfaces: []string{"br0"},
			RemoteLists:      []string{"pending"},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if len(rules.bindings) != 1 {
		t.Fatalf("expected one binding, got %d", len(rules.bindings))
	}
	binding := rules.bindings[0]
	if !binding.HasDestination {
		t.Fatalf("binding must keep a destination match while the list is empty: %+v", binding)
	}
	pair := RuleSetNames("Pending", 0)
	if len(ipset.IPs[pair.DestinationV4]) != 0 || len(ipset.IPs[pair.DestinationV6]) != 0 {
		t.Fatalf("expected empty destination sets")
	}
	if _, ok := ipset.Sets[pair.DestinationV4]; !ok {
		t.Fatalf("expected destination set %s to exist", pair.DestinationV4)
	}
}

func TestCreateGroupRejectsUnknownRemoteList(t *testing.T) {
	ctx := context.Background()
	manager, _, _, rules := newRoutingTestManager(t, remoteListTestLister())
	manager.SetRemoteListProvider(&mockRemoteListProvider{contents: map[string]RemoteListContent{
		"known": {Kind: RemoteListKindCIDR, Entries: []string{"1.1.1.0/24"}},
	}})

	_, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Bad",
		EgressVPN: "wg-sgp",
		Rules:     []RoutingRule{{Name: "Rule 1", RemoteLists: []string{"missing"}}},
	})
	if !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if rules.applyCount != 0 {
		t.Fatalf("invalid group must not touch runtime state")
	}
}

func TestRemoteListReferencesReportsUsingGroups(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _ := newRoutingTestManager(t, remoteListTestLister())
	manager.SetRemoteListProvider(&mockRemoteListProvider{contents: map[string]RemoteListContent{
		"telegram": {Kind: RemoteListKindCIDR, Entries: []string{"1.1.1.0/24"}},
	}})

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Messaging",
		EgressVPN: "wg-sgp",
		Rules:     []RoutingRule{{Name: "Rule 1", RemoteLists: []string{"Telegram"}}},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	refs, err := manager.RemoteListReferences(ctx, "telegram")
	if err != nil {
		t.Fatalf("RemoteListReferences: %v", err)
	}
	if !reflect.DeepEqual(refs, []string{"Messaging"}) {
		t.Fatalf("references = %v", refs)
	}
	refs, err = manager.RemoteListReferences(ctx, "other")
	if err != nil {
		t.Fatalf("RemoteListReferences: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("unexpected references = %v", refs)
	}
}

func TestRemoteListSelectorLinesRoundTrip(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _ := newRoutingTestManager(t, remoteListTestLister())
	manager.SetRemoteListProvider(&mockRemoteListProvider{contents: map[string]RemoteListContent{
		"telegram": {Kind: RemoteListKindCIDR, Entries: []string{"1.1.1.0/24"}},
	}})

	if _, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Comments",
		EgressVPN: "wg-sgp",
		Rules: []RoutingRule{{
			Name: "Rule 1",
			RawSelectors: &RuleRawSelectors{
				RemoteLists: []string{"telegram#messaging", "#disabled-list"},
			},
		}},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	raw, err := manager.ListGroupsRaw(ctx)
	if err != nil {
		t.Fatalf("ListGroupsRaw: %v", err)
	}
	rule := raw[0].Rules[0]
	if !reflect.DeepEqual(rule.RemoteLists, []string{"telegram"}) {
		t.Fatalf("active remote lists = %v", rule.RemoteLists)
	}
	if rule.RawSelectors == nil ||
		!reflect.DeepEqual(rule.RawSelectors.RemoteLists, []string{"telegram#messaging", "#disabled-list"}) {
		t.Fatalf("raw selector lines were not preserved: %+v", rule.RawSelectors)
	}
}

// Disabling a list must not make groups that reference it unsaveable; the rule
// simply stops matching until the list is enabled again.
func TestDisabledRemoteListStaysReferenceable(t *testing.T) {
	ctx := context.Background()
	manager, ipset, _, _ := newRoutingTestManager(t, remoteListTestLister())
	provider := &mockRemoteListProvider{
		contents: map[string]RemoteListContent{
			"telegram": {Kind: RemoteListKindCIDR, Entries: []string{"91.108.56.0/22"}},
		},
		names: map[string]struct{}{"telegram": {}},
	}
	manager.SetRemoteListProvider(provider)

	group, err := manager.CreateGroup(ctx, DomainGroup{
		Name:      "Messaging",
		EgressVPN: "wg-sgp",
		Rules:     []RoutingRule{{Name: "Rule 1", RemoteLists: []string{"telegram"}}},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	pair := RuleSetNames("Messaging", 0)
	if len(ipset.IPs[pair.DestinationV4]) != 1 {
		t.Fatalf("expected the enabled list to populate the set")
	}

	// Disabling drops the list from contents but keeps the name configured.
	delete(provider.contents, "telegram")
	if _, err := manager.UpdateGroup(ctx, group.ID, DomainGroup{
		Name:      "Messaging",
		EgressVPN: "wg-sgp",
		Rules:     []RoutingRule{{Name: "Rule 1", RemoteLists: []string{"telegram"}}},
	}); err != nil {
		t.Fatalf("UpdateGroup with a disabled list must succeed, got %v", err)
	}
	if len(ipset.IPs[pair.DestinationV4]) != 0 {
		t.Fatalf("disabled list still contributes entries: %v", ipset.IPs[pair.DestinationV4])
	}
}
