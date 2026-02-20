package routing

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const defaultIPSetTimeoutSeconds = 86400

var ipsetNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// IPSetOperator defines required ipset interactions.
type IPSetOperator interface {
	EnsureSet(name, family string) error
	AddIP(setName, ip string, timeoutSeconds int) error
	FlushSet(name string) error
	DestroySet(name string) error
	ListSets(prefix string) ([]string, error)
}

// IPSetManager executes ipset commands.
type IPSetManager struct {
	exec Executor
}

func NewIPSetManager(exec Executor) *IPSetManager {
	if exec == nil {
		exec = osExec{}
	}
	return &IPSetManager{exec: exec}
}

func (m *IPSetManager) EnsureSet(name, family string) error {
	if err := validateIPSetName(name); err != nil {
		return err
	}
	setType := "hash:ip"
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "inet":
		setType = "hash:ip"
	case "inet6":
		setType = "hash:ip6"
	default:
		return fmt.Errorf("invalid ipset family %q", family)
	}
	if err := m.exec.Run("ipset", "create", name, setType, "family", family, "timeout", strconv.Itoa(defaultIPSetTimeoutSeconds), "-exist"); err != nil {
		return fmt.Errorf("ipset create %s: %w", name, err)
	}
	return nil
}

func (m *IPSetManager) AddIP(setName, ip string, timeoutSeconds int) error {
	if err := validateIPSetName(setName); err != nil {
		return err
	}
	if net.ParseIP(strings.TrimSpace(ip)) == nil {
		return fmt.Errorf("invalid IP address %q", ip)
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultIPSetTimeoutSeconds
	}
	if err := m.exec.Run("ipset", "add", setName, ip, "timeout", strconv.Itoa(timeoutSeconds), "-exist"); err != nil {
		return fmt.Errorf("ipset add %s %s: %w", setName, ip, err)
	}
	return nil
}

func (m *IPSetManager) FlushSet(name string) error {
	if err := validateIPSetName(name); err != nil {
		return err
	}
	if err := m.exec.Run("ipset", "flush", name); err != nil {
		return fmt.Errorf("ipset flush %s: %w", name, err)
	}
	return nil
}

func (m *IPSetManager) DestroySet(name string) error {
	if err := validateIPSetName(name); err != nil {
		return err
	}
	if err := m.exec.Run("ipset", "destroy", name); err != nil {
		return fmt.Errorf("ipset destroy %s: %w", name, err)
	}
	return nil
}

func (m *IPSetManager) ListSets(prefix string) ([]string, error) {
	output, err := m.exec.Output("ipset", "list", "-name")
	if err != nil {
		return nil, fmt.Errorf("ipset list -name: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	sets := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		sets = append(sets, name)
	}
	sort.Strings(sets)
	return sets, nil
}

func validateIPSetName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("ipset name is required")
	}
	if len(trimmed) > maxIPSetNameLen {
		return fmt.Errorf("ipset name %q exceeds max length %d", trimmed, maxIPSetNameLen)
	}
	if !ipsetNamePattern.MatchString(trimmed) {
		return fmt.Errorf("invalid ipset name %q", trimmed)
	}
	return nil
}
