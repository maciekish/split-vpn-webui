package routing

import (
	"fmt"
	"sort"
)

// MockIPSet is an in-memory IPSetOperator for tests.
type MockIPSet struct {
	Sets    map[string]string
	IPs     map[string][]string
	Calls   []string
	ListErr error
	RunErr  error
}

func (m *MockIPSet) EnsureSet(name, family string) error {
	m.Calls = append(m.Calls, fmt.Sprintf("ensure:%s:%s", name, family))
	if m.RunErr != nil {
		return m.RunErr
	}
	if m.Sets == nil {
		m.Sets = map[string]string{}
	}
	m.Sets[name] = family
	return nil
}

func (m *MockIPSet) AddIP(setName, ip string, timeoutSeconds int) error {
	m.Calls = append(m.Calls, fmt.Sprintf("add:%s:%s:%d", setName, ip, timeoutSeconds))
	if m.RunErr != nil {
		return m.RunErr
	}
	if m.IPs == nil {
		m.IPs = map[string][]string{}
	}
	m.IPs[setName] = append(m.IPs[setName], ip)
	return nil
}

func (m *MockIPSet) FlushSet(name string) error {
	m.Calls = append(m.Calls, "flush:"+name)
	if m.RunErr != nil {
		return m.RunErr
	}
	delete(m.IPs, name)
	return nil
}

func (m *MockIPSet) DestroySet(name string) error {
	m.Calls = append(m.Calls, "destroy:"+name)
	if m.RunErr != nil {
		return m.RunErr
	}
	delete(m.Sets, name)
	delete(m.IPs, name)
	return nil
}

func (m *MockIPSet) ListSets(prefix string) ([]string, error) {
	m.Calls = append(m.Calls, "list:"+prefix)
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	sets := make([]string, 0)
	for name := range m.Sets {
		if prefix != "" && (len(name) < len(prefix) || name[:len(prefix)] != prefix) {
			continue
		}
		sets = append(sets, name)
	}
	sort.Strings(sets)
	return sets, nil
}
