package routing

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"split-vpn-webui/internal/vpn"
)

// VPNLister provides VPN profiles for routing decisions.
type VPNLister interface {
	List() ([]*vpn.VPNProfile, error)
}

// DNSManager abstracts dnsmasq operations for tests.
type DNSManager interface {
	GenerateDnsmasqConf(groups []DomainGroup) string
	WriteDnsmasqConf(content string) error
	ReloadDnsmasq() error
}

// RuleApplier abstracts iptables/ip rule operations for tests.
type RuleApplier interface {
	ApplyRules(bindings []RouteBinding) error
	FlushRules() error
}

// Manager orchestrates group persistence and runtime routing state.
type Manager struct {
	store     *Store
	ipset     IPSetOperator
	dnsmasq   DNSManager
	rules     RuleApplier
	vpnLister VPNLister
	mu        sync.Mutex
}

// NewManager creates a routing manager with concrete dependencies.
func NewManager(db *sql.DB, vpnLister VPNLister) (*Manager, error) {
	store, err := NewStore(db)
	if err != nil {
		return nil, err
	}
	dnsmasq, err := NewDnsmasqManager(nil)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store:     store,
		ipset:     NewIPSetManager(nil),
		dnsmasq:   dnsmasq,
		rules:     NewRuleManager(nil),
		vpnLister: vpnLister,
	}, nil
}

// NewManagerWithDeps creates a manager with injected dependencies for tests.
func NewManagerWithDeps(store *Store, ipset IPSetOperator, dnsmasq DNSManager, rules RuleApplier, vpnLister VPNLister) (*Manager, error) {
	switch {
	case store == nil:
		return nil, fmt.Errorf("store is required")
	case ipset == nil:
		return nil, fmt.Errorf("ipset manager is required")
	case dnsmasq == nil:
		return nil, fmt.Errorf("dnsmasq manager is required")
	case rules == nil:
		return nil, fmt.Errorf("rule manager is required")
	case vpnLister == nil:
		return nil, fmt.Errorf("vpn lister is required")
	}
	return &Manager{store: store, ipset: ipset, dnsmasq: dnsmasq, rules: rules, vpnLister: vpnLister}, nil
}

func (m *Manager) ListGroups(ctx context.Context) ([]DomainGroup, error) {
	return m.store.List(ctx)
}

func (m *Manager) LoadResolverSnapshot(ctx context.Context) (map[ResolverSelector]ResolverValues, error) {
	return m.store.LoadResolverSnapshot(ctx)
}

func (m *Manager) GetGroup(ctx context.Context, id int64) (*DomainGroup, error) {
	return m.store.Get(ctx, id)
}

func (m *Manager) CreateGroup(ctx context.Context, group DomainGroup) (*DomainGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.validateEgressVPN(group.EgressVPN); err != nil {
		return nil, err
	}

	created, err := m.store.Create(ctx, group)
	if err != nil {
		return nil, err
	}
	if err := m.applyLocked(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (m *Manager) UpdateGroup(ctx context.Context, id int64, group DomainGroup) (*DomainGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.validateEgressVPN(group.EgressVPN); err != nil {
		return nil, err
	}

	updated, err := m.store.Update(ctx, id, group)
	if err != nil {
		return nil, err
	}
	if err := m.applyLocked(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

func (m *Manager) DeleteGroup(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
	if err := m.applyLocked(ctx); err != nil {
		return err
	}
	return nil
}

// Apply makes runtime routing state match the persisted groups.
func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyLocked(ctx)
}

// ReplaceState replaces persisted groups and resolver snapshot, then applies runtime state once.
func (m *Manager) ReplaceState(
	ctx context.Context,
	groups []DomainGroup,
	snapshot map[ResolverSelector]ResolverValues,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, group := range groups {
		if err := m.validateEgressVPN(group.EgressVPN); err != nil {
			return err
		}
	}
	if err := m.store.ReplaceAll(ctx, groups, snapshot); err != nil {
		return err
	}
	return m.applyLocked(ctx)
}

func (m *Manager) applyLocked(ctx context.Context) error {
	groups, err := m.store.List(ctx)
	if err != nil {
		return err
	}

	if len(groups) == 0 {
		if err := m.rules.FlushRules(); err != nil {
			return err
		}
		if err := m.cleanupStaleSets(map[string]struct{}{}); err != nil {
			return err
		}
		content := m.dnsmasq.GenerateDnsmasqConf(groups)
		if err := m.dnsmasq.WriteDnsmasqConf(content); err != nil {
			return err
		}
		if err := m.dnsmasq.ReloadDnsmasq(); err != nil {
			return err
		}
		return nil
	}

	profiles, err := m.vpnLister.List()
	if err != nil {
		return err
	}
	vpnByName := make(map[string]*vpn.VPNProfile, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		vpnByName[profile.Name] = profile
	}

	resolved, err := m.store.LoadResolverSnapshot(ctx)
	if err != nil {
		return err
	}

	activeSets := make(map[string]struct{})
	bindings := make([]RouteBinding, 0)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	for _, group := range groups {
		profile, ok := vpnByName[group.EgressVPN]
		if !ok {
			return fmt.Errorf("group %q references missing egress vpn %q", group.Name, group.EgressVPN)
		}
		if profile.RouteTable < 200 {
			return fmt.Errorf("group %q references vpn %q with invalid route table %d", group.Name, profile.Name, profile.RouteTable)
		}
		if profile.FWMark < 200 {
			return fmt.Errorf("group %q references vpn %q with invalid fwmark %d", group.Name, profile.Name, profile.FWMark)
		}
		if strings.TrimSpace(profile.InterfaceName) == "" {
			return fmt.Errorf("group %q references vpn %q with empty interface", group.Name, profile.Name)
		}

		for ruleIndex, rule := range group.Rules {
			binding, err := m.buildBinding(group, rule, ruleIndex, profile, resolved, activeSets)
			if err != nil {
				return err
			}
			bindings = append(bindings, binding)
		}
	}

	content := m.dnsmasq.GenerateDnsmasqConf(groups)
	if err := m.dnsmasq.WriteDnsmasqConf(content); err != nil {
		return err
	}
	if err := m.dnsmasq.ReloadDnsmasq(); err != nil {
		return err
	}
	if err := m.rules.ApplyRules(bindings); err != nil {
		return err
	}
	if err := m.cleanupStaleSets(activeSets); err != nil {
		return err
	}
	return nil
}

func (m *Manager) buildBinding(
	group DomainGroup,
	rule RoutingRule,
	ruleIndex int,
	profile *vpn.VPNProfile,
	resolved map[ResolverSelector]ResolverValues,
	activeSets map[string]struct{},
) (RouteBinding, error) {
	pair := RuleSetNames(group.Name, ruleIndex)
	needsSource := len(rule.SourceCIDRs) > 0
	needsDestination := len(rule.DestinationCIDRs) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0

	if needsSource {
		if err := m.ipset.EnsureSet(pair.SourceV4, "inet"); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.EnsureSet(pair.SourceV6, "inet6"); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.FlushSet(pair.SourceV4); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.FlushSet(pair.SourceV6); err != nil {
			return RouteBinding{}, err
		}
		for _, cidr := range rule.SourceCIDRs {
			setName := pair.SourceV4
			if isIPv6CIDR(cidr) {
				setName = pair.SourceV6
			}
			if err := m.ipset.AddIP(setName, cidr, defaultIPSetTimeoutSeconds); err != nil {
				return RouteBinding{}, err
			}
		}
		activeSets[pair.SourceV4] = struct{}{}
		activeSets[pair.SourceV6] = struct{}{}
	}

	if needsDestination {
		if err := m.ipset.EnsureSet(pair.DestinationV4, "inet"); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.EnsureSet(pair.DestinationV6, "inet6"); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.FlushSet(pair.DestinationV4); err != nil {
			return RouteBinding{}, err
		}
		if err := m.ipset.FlushSet(pair.DestinationV6); err != nil {
			return RouteBinding{}, err
		}

		destEntries := make([]string, 0)
		destEntries = append(destEntries, rule.DestinationCIDRs...)
		for _, asn := range rule.DestinationASNs {
			entry := resolved[ResolverSelector{Type: "asn", Key: asn}]
			destEntries = append(destEntries, entry.V4...)
			destEntries = append(destEntries, entry.V6...)
		}
		for _, domain := range rule.Domains {
			entry := resolved[ResolverSelector{Type: "domain", Key: domain}]
			destEntries = append(destEntries, entry.V4...)
			destEntries = append(destEntries, entry.V6...)
		}
		for _, wildcard := range rule.WildcardDomains {
			entry := resolved[ResolverSelector{Type: "wildcard", Key: wildcard}]
			destEntries = append(destEntries, entry.V4...)
			destEntries = append(destEntries, entry.V6...)
		}
		destEntries = dedupeSortedStrings(destEntries)
		for _, cidr := range destEntries {
			setName := pair.DestinationV4
			if isIPv6CIDR(cidr) {
				setName = pair.DestinationV6
			}
			if err := m.ipset.AddIP(setName, cidr, defaultIPSetTimeoutSeconds); err != nil {
				return RouteBinding{}, err
			}
		}
		activeSets[pair.DestinationV4] = struct{}{}
		activeSets[pair.DestinationV6] = struct{}{}
	}

	return RouteBinding{
		GroupName:        group.Name,
		RuleIndex:        ruleIndex,
		RuleName:         rule.Name,
		SourceInterfaces: append([]string(nil), rule.SourceInterfaces...),
		SourceSetV4:      pair.SourceV4,
		SourceSetV6:      pair.SourceV6,
		SourceMACs:       append([]string(nil), rule.SourceMACs...),
		DestinationSetV4: pair.DestinationV4,
		DestinationSetV6: pair.DestinationV6,
		HasSource:        needsSource,
		HasDestination:   needsDestination,
		DestinationPorts: append([]PortRange(nil), rule.DestinationPorts...),
		Mark:             profile.FWMark,
		RouteTable:       profile.RouteTable,
		Interface:        profile.InterfaceName,
		EgressVPN:        group.EgressVPN,
	}, nil
}

func (m *Manager) cleanupStaleSets(active map[string]struct{}) error {
	existing, err := m.ipset.ListSets(setPrefix)
	if err != nil {
		return err
	}
	for _, setName := range existing {
		if _, keep := active[setName]; keep {
			continue
		}
		if err := m.ipset.DestroySet(setName); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) validateEgressVPN(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: egress vpn is required", ErrGroupValidation)
	}
	vpns, err := m.vpnLister.List()
	if err != nil {
		return err
	}
	for _, profile := range vpns {
		if profile == nil || profile.Name != trimmed {
			continue
		}
		if profile.RouteTable < 200 {
			return fmt.Errorf("%w: egress vpn %q has invalid route table %d", ErrGroupValidation, trimmed, profile.RouteTable)
		}
		if profile.FWMark < 200 {
			return fmt.Errorf("%w: egress vpn %q has invalid fwmark %d", ErrGroupValidation, trimmed, profile.FWMark)
		}
		if strings.TrimSpace(profile.InterfaceName) == "" {
			return fmt.Errorf("%w: egress vpn %q has empty interface", ErrGroupValidation, trimmed)
		}
		return nil
	}
	return fmt.Errorf("%w: egress vpn %q not found", ErrGroupValidation, trimmed)
}

func isIPv6CIDR(value string) bool {
	if strings.Contains(value, ":") {
		return true
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.To4() == nil
	}
	ip, _, err := net.ParseCIDR(value)
	if err != nil {
		return false
	}
	return ip.To4() == nil
}

func dedupeSortedStrings(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
