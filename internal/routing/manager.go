package routing

import (
	"context"
	"database/sql"
	"fmt"
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

// Manager orchestrates domain group persistence and runtime routing state.
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
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if ipset == nil {
		return nil, fmt.Errorf("ipset manager is required")
	}
	if dnsmasq == nil {
		return nil, fmt.Errorf("dnsmasq manager is required")
	}
	if rules == nil {
		return nil, fmt.Errorf("rule manager is required")
	}
	if vpnLister == nil {
		return nil, fmt.Errorf("vpn lister is required")
	}
	return &Manager{store: store, ipset: ipset, dnsmasq: dnsmasq, rules: rules, vpnLister: vpnLister}, nil
}

func (m *Manager) ListGroups(ctx context.Context) ([]DomainGroup, error) {
	return m.store.List(ctx)
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

// Apply makes runtime routing state match the persisted domain groups.
func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

	vpnProfiles, err := m.vpnLister.List()
	if err != nil {
		return err
	}
	vpnByName := make(map[string]*vpn.VPNProfile, len(vpnProfiles))
	for _, profile := range vpnProfiles {
		if profile == nil {
			continue
		}
		vpnByName[profile.Name] = profile
	}

	activeSets := make(map[string]struct{})
	bindings := make([]RouteBinding, 0, len(groups))
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

		v4Set, v6Set := GroupSetNames(group.Name)
		if err := m.ipset.EnsureSet(v4Set, "inet"); err != nil {
			return err
		}
		if err := m.ipset.EnsureSet(v6Set, "inet6"); err != nil {
			return err
		}
		activeSets[v4Set] = struct{}{}
		activeSets[v6Set] = struct{}{}

		bindings = append(bindings, RouteBinding{
			GroupName:   group.Name,
			SetV4:       v4Set,
			SetV6:       v6Set,
			Mark:        profile.FWMark,
			RouteTable:  profile.RouteTable,
			Interface:   profile.InterfaceName,
			EgressVPN:   group.EgressVPN,
			DomainCount: len(group.Domains),
		})
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

func (m *Manager) cleanupStaleSets(active map[string]struct{}) error {
	existing, err := m.ipset.ListSets(setPrefix)
	if err != nil {
		return err
	}
	for _, setName := range existing {
		if _, keep := active[setName]; keep {
			continue
		}
		if !(strings.HasSuffix(setName, setSuffixV4) || strings.HasSuffix(setName, setSuffixV6)) {
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
