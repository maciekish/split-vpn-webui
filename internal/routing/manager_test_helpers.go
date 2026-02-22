package routing

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"split-vpn-webui/internal/database"
)

type orderedIPSetMock struct {
	sets          map[string]string
	ips           map[string][]string
	destroyed     []string
	beforeDestroy func() error
}

func (m *orderedIPSetMock) EnsureSet(name, family string) error {
	if m.sets == nil {
		m.sets = map[string]string{}
	}
	m.sets[name] = family
	return nil
}

func (m *orderedIPSetMock) AddIP(setName, ip string, timeoutSeconds int) error {
	if m.ips == nil {
		m.ips = map[string][]string{}
	}
	m.ips[setName] = append(m.ips[setName], ip)
	return nil
}

func (m *orderedIPSetMock) FlushSet(name string) error {
	if m.ips != nil {
		delete(m.ips, name)
	}
	return nil
}

func (m *orderedIPSetMock) SwapSets(setA, setB string) error {
	if m.sets == nil {
		m.sets = map[string]string{}
	}
	if m.ips == nil {
		m.ips = map[string][]string{}
	}
	m.sets[setA], m.sets[setB] = m.sets[setB], m.sets[setA]
	m.ips[setA], m.ips[setB] = m.ips[setB], m.ips[setA]
	return nil
}

func (m *orderedIPSetMock) DestroySet(name string) error {
	if m.beforeDestroy != nil && !strings.HasSuffix(name, "_n") {
		if err := m.beforeDestroy(); err != nil {
			return err
		}
	}
	delete(m.sets, name)
	m.destroyed = append(m.destroyed, name)
	return nil
}

func (m *orderedIPSetMock) ListSets(prefix string) ([]string, error) {
	sets := make([]string, 0, len(m.sets))
	for name := range m.sets {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		sets = append(sets, name)
	}
	sort.Strings(sets)
	return sets, nil
}

type orderedRuleApplierMock struct {
	applyCalled bool
	flushCalled bool
	onApply     func()
	onFlush     func()
}

func (m *orderedRuleApplierMock) ApplyRules(bindings []RouteBinding) error {
	m.applyCalled = true
	if m.onApply != nil {
		m.onApply()
	}
	return nil
}

func (m *orderedRuleApplierMock) FlushRules() error {
	m.flushCalled = true
	if m.onFlush != nil {
		m.onFlush()
	}
	return nil
}

type concurrencyRuleApplier struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (m *concurrencyRuleApplier) ApplyRules(bindings []RouteBinding) error {
	m.mu.Lock()
	m.inFlight++
	if m.inFlight > m.maxInFlight {
		m.maxInFlight = m.inFlight
	}
	m.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	m.mu.Lock()
	m.inFlight--
	m.mu.Unlock()
	return nil
}

func (m *concurrencyRuleApplier) FlushRules() error {
	return nil
}

func newRoutingTestManagerWithDeps(t *testing.T, ipset IPSetOperator, dns DNSManager, rules RuleApplier, lister VPNLister) *Manager {
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
	manager, err := NewManagerWithDeps(store, ipset, dns, rules, lister)
	if err != nil {
		t.Fatalf("new manager with deps: %v", err)
	}
	return manager
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
