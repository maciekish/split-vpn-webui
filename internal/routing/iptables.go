package routing

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	markChainName   = "SVPN_MARK"
	natChainName    = "SVPN_NAT"
	rulePriority    = "100"
	deleteLoopLimit = 64
)

// RuleManager applies iptables/ip6tables and ip rule state.
type RuleManager struct {
	exec Executor
}

func NewRuleManager(exec Executor) *RuleManager {
	if exec == nil {
		exec = osExec{}
	}
	return &RuleManager{exec: exec}
}

// ApplyRules refreshes custom chains and policy rules from the provided bindings.
func (m *RuleManager) ApplyRules(bindings []RouteBinding) error {
	sorted := append([]RouteBinding(nil), bindings...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GroupName == sorted[j].GroupName {
			return sorted[i].SetV4 < sorted[j].SetV4
		}
		return sorted[i].GroupName < sorted[j].GroupName
	})

	if err := m.ensureChain("iptables", "mangle", markChainName, "PREROUTING"); err != nil {
		return err
	}
	if err := m.ensureChain("iptables", "nat", natChainName, "POSTROUTING"); err != nil {
		return err
	}
	if err := m.ensureChain("ip6tables", "mangle", markChainName, "PREROUTING"); err != nil {
		return err
	}
	if err := m.ensureChain("ip6tables", "nat", natChainName, "POSTROUTING"); err != nil {
		return err
	}

	seenRules := make(map[string]struct{})
	for _, binding := range sorted {
		if binding.Mark < 200 {
			return fmt.Errorf("invalid fwmark %d for group %s", binding.Mark, binding.GroupName)
		}
		if binding.RouteTable < 200 {
			return fmt.Errorf("invalid route table %d for group %s", binding.RouteTable, binding.GroupName)
		}
		if strings.TrimSpace(binding.Interface) == "" {
			return fmt.Errorf("missing interface for group %s", binding.GroupName)
		}
		markHex := fmt.Sprintf("0x%x", binding.Mark)

		if err := m.exec.Run("iptables", "-t", "mangle", "-A", markChainName, "-m", "set", "--match-set", binding.SetV4, "dst", "-j", "MARK", "--set-mark", markHex); err != nil {
			return fmt.Errorf("add ipv4 mark rule for %s: %w", binding.GroupName, err)
		}
		if err := m.exec.Run("ip6tables", "-t", "mangle", "-A", markChainName, "-m", "set", "--match-set", binding.SetV6, "dst", "-j", "MARK", "--set-mark", markHex); err != nil {
			return fmt.Errorf("add ipv6 mark rule for %s: %w", binding.GroupName, err)
		}
		if err := m.exec.Run("iptables", "-t", "nat", "-A", natChainName, "-m", "mark", "--mark", markHex, "-o", binding.Interface, "-j", "MASQUERADE"); err != nil {
			return fmt.Errorf("add ipv4 nat rule for %s: %w", binding.GroupName, err)
		}
		if err := m.exec.Run("ip6tables", "-t", "nat", "-A", natChainName, "-m", "mark", "--mark", markHex, "-o", binding.Interface, "-j", "MASQUERADE"); err != nil {
			return fmt.Errorf("add ipv6 nat rule for %s: %w", binding.GroupName, err)
		}

		uniqueRule := markHex + ":" + strconv.Itoa(binding.RouteTable)
		if _, seen := seenRules[uniqueRule]; seen {
			continue
		}
		seenRules[uniqueRule] = struct{}{}
		if err := m.refreshIPRule(markHex, binding.RouteTable, false); err != nil {
			return err
		}
		if err := m.refreshIPRule(markHex, binding.RouteTable, true); err != nil {
			return err
		}
	}
	return nil
}

// FlushRules removes this application's chains and managed ip rules.
func (m *RuleManager) FlushRules() error {
	for _, command := range []struct {
		tool   string
		table  string
		chain  string
		parent string
	}{
		{tool: "iptables", table: "mangle", chain: markChainName, parent: "PREROUTING"},
		{tool: "iptables", table: "nat", chain: natChainName, parent: "POSTROUTING"},
		{tool: "ip6tables", table: "mangle", chain: markChainName, parent: "PREROUTING"},
		{tool: "ip6tables", table: "nat", chain: natChainName, parent: "POSTROUTING"},
	} {
		m.cleanupChain(command.tool, command.table, command.chain, command.parent)
	}
	if err := m.flushManagedIPRules(false); err != nil {
		return err
	}
	if err := m.flushManagedIPRules(true); err != nil {
		return err
	}
	return nil
}

func (m *RuleManager) ensureChain(tool, table, chain, parent string) error {
	_ = m.exec.Run(tool, "-t", table, "-N", chain)
	if err := m.exec.Run(tool, "-t", table, "-F", chain); err != nil {
		return fmt.Errorf("flush %s/%s chain %s: %w", tool, table, chain, err)
	}
	if err := m.exec.Run(tool, "-t", table, "-C", parent, "-j", chain); err != nil {
		if addErr := m.exec.Run(tool, "-t", table, "-A", parent, "-j", chain); addErr != nil {
			return fmt.Errorf("link %s/%s %s->%s: %w", tool, table, parent, chain, addErr)
		}
	}
	return nil
}

func (m *RuleManager) cleanupChain(tool, table, chain, parent string) {
	for i := 0; i < deleteLoopLimit; i++ {
		if err := m.exec.Run(tool, "-t", table, "-D", parent, "-j", chain); err != nil {
			break
		}
	}
	_ = m.exec.Run(tool, "-t", table, "-F", chain)
	_ = m.exec.Run(tool, "-t", table, "-X", chain)
}

func (m *RuleManager) refreshIPRule(markHex string, routeTable int, ipv6 bool) error {
	argsDelete := []string{"rule", "del", "fwmark", markHex, "table", strconv.Itoa(routeTable), "priority", rulePriority}
	argsAdd := []string{"rule", "add", "fwmark", markHex, "table", strconv.Itoa(routeTable), "priority", rulePriority}
	if ipv6 {
		argsDelete = append([]string{"-6"}, argsDelete...)
		argsAdd = append([]string{"-6"}, argsAdd...)
	}
	for i := 0; i < deleteLoopLimit; i++ {
		if err := m.exec.Run("ip", argsDelete...); err != nil {
			break
		}
	}
	if err := m.exec.Run("ip", argsAdd...); err != nil {
		family := "ipv4"
		if ipv6 {
			family = "ipv6"
		}
		return fmt.Errorf("add %s ip rule for mark %s table %d: %w", family, markHex, routeTable, err)
	}
	return nil
}

func (m *RuleManager) flushManagedIPRules(ipv6 bool) error {
	args := []string{"rule", "show"}
	if ipv6 {
		args = append([]string{"-6"}, args...)
	}
	output, err := m.exec.Output("ip", args...)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		markToken, tableID, ok := parseIPRuleLine(line)
		if !ok {
			continue
		}
		if tableID < 200 {
			continue
		}
		markValue, err := strconv.ParseUint(markToken, 0, 32)
		if err != nil || markValue < 200 {
			continue
		}
		deleteArgs := []string{"rule", "del", "fwmark", fmt.Sprintf("0x%x", markValue), "table", strconv.Itoa(tableID), "priority", rulePriority}
		if ipv6 {
			deleteArgs = append([]string{"-6"}, deleteArgs...)
		}
		for i := 0; i < deleteLoopLimit; i++ {
			if delErr := m.exec.Run("ip", deleteArgs...); delErr != nil {
				break
			}
		}
	}
	return nil
}

func parseIPRuleLine(line string) (string, int, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 6 {
		return "", 0, false
	}
	mark := ""
	table := -1
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "fwmark":
			mark = strings.Split(strings.TrimSpace(fields[i+1]), "/")[0]
		case "lookup", "table":
			if n, err := strconv.Atoi(fields[i+1]); err == nil {
				table = n
			}
		}
	}
	if mark == "" || table < 0 {
		return "", 0, false
	}
	return mark, table, true
}
