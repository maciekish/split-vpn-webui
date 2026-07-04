package routing

import (
	"fmt"
	"sort"
	"strings"
)

const (
	markChainName = "SVPN_MARK"
	natChainName  = "SVPN_NAT"
	mssChainName  = "SVPN_MSS"

	markChainA = "SVPN_MARK_A"
	markChainB = "SVPN_MARK_B"
	natChainA  = "SVPN_NAT_A"
	natChainB  = "SVPN_NAT_B"
	mssChainA  = "SVPN_MSS_A"
	mssChainB  = "SVPN_MSS_B"

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
			return sorted[i].RuleIndex < sorted[j].RuleIndex
		}
		return sorted[i].GroupName < sorted[j].GroupName
	})

	activeVariant := m.detectActiveVariant()
	workingMark, workingNAT, workingMSS, staleMark, staleNAT, staleMSS := selectWorkingVariant(activeVariant)
	for _, prep := range []struct {
		tool       string
		table      string
		root       string
		parent     string
		generation string
	}{
		{tool: "iptables", table: "mangle", root: markChainName, parent: "PREROUTING", generation: workingMark},
		{tool: "iptables", table: "mangle", root: mssChainName, parent: "FORWARD", generation: workingMSS},
		{tool: "iptables", table: "nat", root: natChainName, parent: "POSTROUTING", generation: workingNAT},
		{tool: "ip6tables", table: "mangle", root: markChainName, parent: "PREROUTING", generation: workingMark},
		{tool: "ip6tables", table: "mangle", root: mssChainName, parent: "FORWARD", generation: workingMSS},
		{tool: "ip6tables", table: "nat", root: natChainName, parent: "POSTROUTING", generation: workingNAT},
	} {
		if err := m.prepareGenerationChain(prep.tool, prep.table, prep.root, prep.parent, prep.generation); err != nil {
			return err
		}
	}
	if activeVariant == "" {
		// Legacy migration path: clear root chains once so old in-chain rules
		// cannot conflict with generation-chain forwarding.
		for _, root := range []struct {
			tool  string
			table string
			chain string
		}{
			{tool: "iptables", table: "mangle", chain: markChainName},
			{tool: "iptables", table: "mangle", chain: mssChainName},
			{tool: "iptables", table: "nat", chain: natChainName},
			{tool: "ip6tables", table: "mangle", chain: markChainName},
			{tool: "ip6tables", table: "mangle", chain: mssChainName},
			{tool: "ip6tables", table: "nat", chain: natChainName},
		} {
			if err := m.exec.Run(root.tool, "-t", root.table, "-F", root.chain); err != nil {
				return fmt.Errorf("flush %s/%s chain %s during migration: %w", root.tool, root.table, root.chain, err)
			}
		}
	}

	desiredRules := make(map[uint32]int)
	seenNATRules := make(map[string]struct{})
	mssByInterface := make(map[string]mssClamp)
	for bindingIndex, binding := range sorted {
		if binding.Mark < 200 {
			return fmt.Errorf("invalid fwmark %d for group %s", binding.Mark, binding.GroupName)
		}
		if binding.RouteTable < 200 {
			return fmt.Errorf("invalid route table %d for group %s", binding.RouteTable, binding.GroupName)
		}
		if strings.TrimSpace(binding.Interface) == "" {
			return fmt.Errorf("missing interface for group %s", binding.GroupName)
		}

		if existingTable, exists := desiredRules[binding.Mark]; exists && existingTable != binding.RouteTable {
			return fmt.Errorf(
				"conflicting route table for fwmark 0x%x: %d and %d",
				binding.Mark,
				existingTable,
				binding.RouteTable,
			)
		}
		desiredRules[binding.Mark] = binding.RouteTable

		markHex := fmt.Sprintf("0x%x", binding.Mark)
		if err := m.addMarkRules(binding, bindingIndex, workingMark, markHex); err != nil {
			return err
		}

		if clamp := (mssClamp{v4: binding.MSSClampV4, v6: binding.MSSClampV6}); clamp.enabled() {
			// Interface maps 1:1 to a VPN, so every binding sharing an interface
			// carries identical clamp settings; last write is a harmless no-op.
			mssByInterface[binding.Interface] = clamp
		}

		natKey := markHex + ":" + binding.Interface
		if _, seen := seenNATRules[natKey]; seen {
			continue
		}
		seenNATRules[natKey] = struct{}{}
		if err := m.addNATRule("iptables", workingNAT, markHex, binding.Interface, binding.GroupName); err != nil {
			return err
		}
		if err := m.addNATRule("ip6tables", workingNAT, markHex, binding.Interface, binding.GroupName); err != nil {
			return err
		}
	}

	for _, iface := range sortedInterfaces(mssByInterface) {
		if err := m.addMSSRules(workingMSS, iface, mssByInterface[iface]); err != nil {
			return err
		}
	}

	for _, sw := range []struct {
		tool  string
		table string
		root  string
		next  string
		stale string
	}{
		{tool: "iptables", table: "mangle", root: markChainName, next: workingMark, stale: staleMark},
		{tool: "iptables", table: "mangle", root: mssChainName, next: workingMSS, stale: staleMSS},
		{tool: "iptables", table: "nat", root: natChainName, next: workingNAT, stale: staleNAT},
		{tool: "ip6tables", table: "mangle", root: markChainName, next: workingMark, stale: staleMark},
		{tool: "ip6tables", table: "mangle", root: mssChainName, next: workingMSS, stale: staleMSS},
		{tool: "ip6tables", table: "nat", root: natChainName, next: workingNAT, stale: staleNAT},
	} {
		if err := m.switchRootJump(sw.tool, sw.table, sw.root, sw.next, sw.stale); err != nil {
			return err
		}
	}

	if err := m.reconcileManagedIPRules(desiredRules, false); err != nil {
		return err
	}
	if err := m.reconcileManagedIPRules(desiredRules, true); err != nil {
		return err
	}
	return nil
}

func (m *RuleManager) detectActiveVariant() string {
	active := m.detectActiveGeneration("iptables", "mangle", markChainName)
	if active == "" {
		active = m.detectActiveGeneration("ip6tables", "mangle", markChainName)
	}
	return active
}

func selectWorkingVariant(active string) (workingMark, workingNAT, workingMSS, staleMark, staleNAT, staleMSS string) {
	if active == markChainA {
		return markChainB, natChainB, mssChainB, markChainA, natChainA, mssChainA
	}
	return markChainA, natChainA, mssChainA, markChainB, natChainB, mssChainB
}

// mssClamp holds the per-family MSS clamp settings for a tunnel interface.
// A value of "" disables clamping for that family, "pmtu" clamps to the path
// MTU, and any other value is a fixed MSS passed to --set-mss.
type mssClamp struct {
	v4 string
	v6 string
}

func (c mssClamp) enabled() bool {
	return strings.TrimSpace(c.v4) != "" || strings.TrimSpace(c.v6) != ""
}

func sortedInterfaces(byInterface map[string]mssClamp) []string {
	names := make([]string, 0, len(byInterface))
	for name := range byInterface {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// addMSSRules clamps the TCP MSS on SYN packets leaving the tunnel interface so
// segments fit the tunnel MTU without relying on PMTUD.
//
// The rule matches egress (-o tunnel) only, matching peacey/split-vpn and
// OpenWrt's mtu_fix. This deliberately targets the direction where PMTUD is
// unreliable: a client's SYN egressing the tunnel is clamped so the remote peer
// pre-sizes the download data (the peer would otherwise depend on an ICMP
// PTB surviving the public internet, which is frequently black-holed). The
// reverse (upload) direction stays safe via ordinary PMTUD, because the gateway
// drops oversized packets at its own tunnel interface and returns the ICMP over
// the LAN — a local, reliable path. Auto mode uses --clamp-mss-to-pmtu, which is
// output-route based and would be a no-op on an ingress rule anyway.
func (m *RuleManager) addMSSRules(chain, iface string, clamp mssClamp) error {
	if v4 := strings.TrimSpace(clamp.v4); v4 != "" {
		if err := m.addMSSRuleForFamily("iptables", chain, iface, v4); err != nil {
			return err
		}
	}
	if v6 := strings.TrimSpace(clamp.v6); v6 != "" {
		if err := m.addMSSRuleForFamily("ip6tables", chain, iface, v6); err != nil {
			return err
		}
	}
	return nil
}

func (m *RuleManager) addMSSRuleForFamily(tool, chain, iface, value string) error {
	args := []string{
		"-t", "mangle", "-A", chain,
		"-o", iface,
		"-p", "tcp", "-m", "tcp", "--tcp-flags", "SYN,RST", "SYN",
		"-j", "TCPMSS",
	}
	if strings.EqualFold(value, "pmtu") {
		args = append(args, "--clamp-mss-to-pmtu")
	} else {
		args = append(args, "--set-mss", value)
	}
	if err := m.exec.Run(tool, args...); err != nil {
		family := "ipv4"
		if tool == "ip6tables" {
			family = "ipv6"
		}
		return fmt.Errorf("add %s mss clamp rule for %s: %w", family, iface, err)
	}
	return nil
}

func (m *RuleManager) detectActiveGeneration(tool, table, root string) string {
	output, err := m.exec.Output(tool, "-t", table, "-S", root)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) < 4 {
			continue
		}
		if fields[0] != "-A" || fields[1] != root || fields[2] != "-j" {
			continue
		}
		switch fields[3] {
		case markChainA, markChainB:
			return fields[3]
		}
	}
	return ""
}

func (m *RuleManager) prepareGenerationChain(tool, table, root, parent, generation string) error {
	_ = m.exec.Run(tool, "-t", table, "-N", root)
	if err := m.exec.Run(tool, "-t", table, "-C", parent, "-j", root); err != nil {
		if addErr := m.exec.Run(tool, "-t", table, "-A", parent, "-j", root); addErr != nil {
			return fmt.Errorf("link %s/%s %s->%s: %w", tool, table, parent, root, addErr)
		}
	}
	_ = m.exec.Run(tool, "-t", table, "-N", generation)
	if err := m.exec.Run(tool, "-t", table, "-F", generation); err != nil {
		return fmt.Errorf("flush %s/%s chain %s: %w", tool, table, generation, err)
	}
	if err := m.cleanupGenerationRuleChains(tool, table, generation); err != nil {
		return err
	}
	return nil
}

func (m *RuleManager) cleanupGenerationRuleChains(tool, table, generation string) error {
	prefix := generationRuleChainPrefix(generation)
	if prefix == "" {
		return nil
	}
	output, err := m.exec.Output(tool, "-t", table, "-S")
	if err != nil {
		return nil
	}
	lines := strings.Split(string(output), "\n")
	for _, raw := range lines {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) < 2 || fields[0] != "-N" {
			continue
		}
		chainName := strings.TrimSpace(fields[1])
		if chainName == "" || !strings.HasPrefix(chainName, prefix) {
			continue
		}
		_ = m.exec.Run(tool, "-t", table, "-F", chainName)
		_ = m.exec.Run(tool, "-t", table, "-X", chainName)
	}
	return nil
}

func (m *RuleManager) switchRootJump(tool, table, root, next, stale string) error {
	if err := m.exec.Run(tool, "-t", table, "-C", root, "-j", next); err != nil {
		if addErr := m.exec.Run(tool, "-t", table, "-I", root, "1", "-j", next); addErr != nil {
			return fmt.Errorf("switch %s/%s root %s -> %s: %w", tool, table, root, next, addErr)
		}
	}
	for i := 0; i < deleteLoopLimit; i++ {
		if err := m.exec.Run(tool, "-t", table, "-D", root, "-j", stale); err != nil {
			break
		}
	}
	return nil
}

func (m *RuleManager) addNATRule(tool, chain, markHex, iface, groupName string) error {
	if err := m.exec.Run(tool, "-t", "nat", "-A", chain, "-m", "mark", "--mark", markHex, "-o", iface, "-j", "MASQUERADE"); err != nil {
		family := "ipv4"
		if tool == "ip6tables" {
			family = "ipv6"
		}
		return fmt.Errorf("add %s nat rule for %s: %w", family, groupName, err)
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
		{tool: "iptables", table: "mangle", chain: mssChainName, parent: "FORWARD"},
		{tool: "iptables", table: "nat", chain: natChainName, parent: "POSTROUTING"},
		{tool: "ip6tables", table: "mangle", chain: markChainName, parent: "PREROUTING"},
		{tool: "ip6tables", table: "mangle", chain: mssChainName, parent: "FORWARD"},
		{tool: "ip6tables", table: "nat", chain: natChainName, parent: "POSTROUTING"},
		{tool: "iptables", table: "mangle", chain: markChainA},
		{tool: "iptables", table: "mangle", chain: markChainB},
		{tool: "iptables", table: "mangle", chain: mssChainA},
		{tool: "iptables", table: "mangle", chain: mssChainB},
		{tool: "iptables", table: "nat", chain: natChainA},
		{tool: "iptables", table: "nat", chain: natChainB},
		{tool: "ip6tables", table: "mangle", chain: markChainA},
		{tool: "ip6tables", table: "mangle", chain: markChainB},
		{tool: "ip6tables", table: "mangle", chain: mssChainA},
		{tool: "ip6tables", table: "mangle", chain: mssChainB},
		{tool: "ip6tables", table: "nat", chain: natChainA},
		{tool: "ip6tables", table: "nat", chain: natChainB},
	} {
		m.cleanupChain(command.tool, command.table, command.chain, command.parent)
	}
	for _, command := range []struct {
		tool       string
		table      string
		generation string
	}{
		{tool: "iptables", table: "mangle", generation: markChainA},
		{tool: "iptables", table: "mangle", generation: markChainB},
		{tool: "ip6tables", table: "mangle", generation: markChainA},
		{tool: "ip6tables", table: "mangle", generation: markChainB},
	} {
		_ = m.cleanupGenerationRuleChains(command.tool, command.table, command.generation)
	}
	if err := m.flushManagedIPRules(false); err != nil {
		return err
	}
	if err := m.flushManagedIPRules(true); err != nil {
		return err
	}
	return nil
}

func (m *RuleManager) cleanupChain(tool, table, chain, parent string) {
	if parent != "" {
		for i := 0; i < deleteLoopLimit; i++ {
			if err := m.exec.Run(tool, "-t", table, "-D", parent, "-j", chain); err != nil {
				break
			}
		}
	}
	_ = m.exec.Run(tool, "-t", table, "-F", chain)
	_ = m.exec.Run(tool, "-t", table, "-X", chain)
}
