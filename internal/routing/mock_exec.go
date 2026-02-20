package routing

import (
	"errors"
	"strings"
	"sync"
)

// MockExec is a deterministic executor used by unit tests.
type MockExec struct {
	mu sync.Mutex

	RunCalls    [][]string
	OutputCalls [][]string

	RunErrors    map[string]error
	OutputErrors map[string]error
	Outputs      map[string][]byte
}

func (m *MockExec) Run(name string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{name}, args...)
	m.RunCalls = append(m.RunCalls, call)
	key := strings.Join(call, " ")
	if err, ok := m.RunErrors[key]; ok {
		return err
	}
	return nil
}

func (m *MockExec) Output(name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{name}, args...)
	m.OutputCalls = append(m.OutputCalls, call)
	key := strings.Join(call, " ")
	out := m.Outputs[key]
	if err, ok := m.OutputErrors[key]; ok {
		return out, err
	}
	if out == nil {
		return nil, errors.New("mock output not configured")
	}
	return out, nil
}
