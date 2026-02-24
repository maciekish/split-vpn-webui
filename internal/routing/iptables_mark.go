package routing

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func (m *RuleManager) addMarkRules(binding RouteBinding, bindingIndex int, chain string, markHex string) error {
	if binding.HasSource {
		if strings.TrimSpace(binding.SourceSetV4) == "" || strings.TrimSpace(binding.SourceSetV6) == "" {
			return fmt.Errorf("missing source set names for group %s rule %d", binding.GroupName, binding.RuleIndex+1)
		}
	}
	if binding.HasExcludedSource {
		if strings.TrimSpace(binding.ExcludedSourceSetV4) == "" || strings.TrimSpace(binding.ExcludedSourceSetV6) == "" {
			return fmt.Errorf("missing excluded source set names for group %s rule %d", binding.GroupName, binding.RuleIndex+1)
		}
	}
	if binding.HasDestination {
		if strings.TrimSpace(binding.DestinationSetV4) == "" || strings.TrimSpace(binding.DestinationSetV6) == "" {
			return fmt.Errorf("missing destination set names for group %s rule %d", binding.GroupName, binding.RuleIndex+1)
		}
	}
	if binding.HasExcludedDestination {
		if strings.TrimSpace(binding.ExcludedDestinationSetV4) == "" || strings.TrimSpace(binding.ExcludedDestinationSetV6) == "" {
			return fmt.Errorf("missing excluded destination set names for group %s rule %d", binding.GroupName, binding.RuleIndex+1)
		}
	}

	if err := m.addMarkRulesByFamily("iptables", chain, binding, bindingIndex, markHex); err != nil {
		return err
	}
	if err := m.addMarkRulesByFamily("ip6tables", chain, binding, bindingIndex, markHex); err != nil {
		return err
	}
	return nil
}

func (m *RuleManager) addMarkRulesByFamily(
	tool string,
	chain string,
	binding RouteBinding,
	bindingIndex int,
	markHex string,
) error {
	isIPv6 := tool == "ip6tables"
	ruleChain := generationRuleChainName(chain, bindingIndex, isIPv6)
	if err := m.exec.Run(tool, "-t", "mangle", "-N", ruleChain); err != nil {
		_ = m.exec.Run(tool, "-t", "mangle", "-F", ruleChain)
	}
	if err := m.exec.Run(tool, "-t", "mangle", "-F", ruleChain); err != nil {
		return fmt.Errorf("flush %s rule chain %s: %w", tool, ruleChain, err)
	}
	if err := m.exec.Run(tool, "-t", "mangle", "-A", chain, "-j", ruleChain); err != nil {
		return fmt.Errorf("link %s chain %s -> %s: %w", tool, chain, ruleChain, err)
	}

	ports := expandPortSelectors(binding.DestinationPorts)
	excludedPorts := expandPortSelectors(binding.ExcludedDestinationPorts)
	sourceInterfaces := expandSelectorValues(binding.SourceInterfaces)
	sourceMACs := expandSelectorValues(binding.SourceMACs)
	for _, sourceIface := range sourceInterfaces {
		for _, sourceMAC := range sourceMACs {
			for _, port := range ports {
				baseArgs := m.baseMarkRuleArgs(tool, ruleChain, binding, port, sourceIface, sourceMAC)
				if err := m.addExclusionRulesByFamily(tool, binding, port, excludedPorts, baseArgs); err != nil {
					return err
				}
				markArgs := append(append([]string(nil), baseArgs...), "-j", "MARK", "--set-mark", markHex)
				if err := m.exec.Run(tool, markArgs...); err != nil {
					family := "ipv4"
					if isIPv6 {
						family = "ipv6"
					}
					return fmt.Errorf("add %s mark rule for %s: %w", family, binding.GroupName, err)
				}
			}
		}
	}
	return nil
}

func (m *RuleManager) addExclusionRulesByFamily(
	tool string,
	binding RouteBinding,
	includePort PortRange,
	excludedPorts []PortRange,
	baseArgs []string,
) error {
	isIPv6 := tool == "ip6tables"
	if binding.ExcludeMulticast {
		multicast := "224.0.0.0/4"
		if isIPv6 {
			multicast = "ff00::/8"
		}
		args := append(append([]string(nil), baseArgs...), "-d", multicast, "-j", "RETURN")
		if err := m.exec.Run(tool, args...); err != nil {
			return fmt.Errorf("exclude multicast for %s: %w", binding.GroupName, err)
		}
	}
	if binding.HasExcludedSource {
		setName := binding.ExcludedSourceSetV4
		if isIPv6 {
			setName = binding.ExcludedSourceSetV6
		}
		args := append(append([]string(nil), baseArgs...),
			"-m", "set", "--match-set", setName, "src",
			"-j", "RETURN",
		)
		if err := m.exec.Run(tool, args...); err != nil {
			return fmt.Errorf("exclude source set for %s: %w", binding.GroupName, err)
		}
	}
	if binding.HasExcludedDestination {
		setName := binding.ExcludedDestinationSetV4
		if isIPv6 {
			setName = binding.ExcludedDestinationSetV6
		}
		args := append(append([]string(nil), baseArgs...),
			"-m", "set", "--match-set", setName, "dst",
			"-j", "RETURN",
		)
		if err := m.exec.Run(tool, args...); err != nil {
			return fmt.Errorf("exclude destination set for %s: %w", binding.GroupName, err)
		}
	}
	for _, excludedPort := range excludedPorts {
		if !portsOverlapForExclusion(includePort, excludedPort) {
			continue
		}
		args := append([]string{}, baseArgs...)
		if includePort.Protocol == "" {
			args = append(args, "-p", excludedPort.Protocol)
		}
		args = append(args, "--dport", formatPortRange(excludedPort), "-j", "RETURN")
		if err := m.exec.Run(tool, args...); err != nil {
			return fmt.Errorf("exclude destination port for %s: %w", binding.GroupName, err)
		}
	}
	return nil
}

func (m *RuleManager) baseMarkRuleArgs(
	tool string,
	chain string,
	binding RouteBinding,
	port PortRange,
	sourceIface string,
	sourceMAC string,
) []string {
	isIPv6 := tool == "ip6tables"
	args := []string{"-t", "mangle", "-A", chain}
	if binding.HasSource {
		setName := binding.SourceSetV4
		if isIPv6 {
			setName = binding.SourceSetV6
		}
		args = append(args, "-m", "set", "--match-set", setName, "src")
	}
	if binding.HasDestination {
		setName := binding.DestinationSetV4
		if isIPv6 {
			setName = binding.DestinationSetV6
		}
		args = append(args, "-m", "set", "--match-set", setName, "dst")
	}
	if sourceIface != "" {
		args = append(args, "-i", sourceIface)
	}
	if sourceMAC != "" {
		args = append(args, "-m", "mac", "--mac-source", sourceMAC)
	}
	if port.Protocol != "" {
		args = append(args, "-p", port.Protocol, "--dport", formatPortRange(port))
	}
	return args
}

func expandPortSelectors(ports []PortRange) []PortRange {
	if len(ports) == 0 {
		return []PortRange{{}}
	}
	expanded := make([]PortRange, 0, len(ports)*2)
	for _, port := range ports {
		if strings.EqualFold(port.Protocol, "both") {
			expanded = append(expanded,
				PortRange{Protocol: "tcp", Start: port.Start, End: port.End},
				PortRange{Protocol: "udp", Start: port.Start, End: port.End},
			)
			continue
		}
		expanded = append(expanded, port)
	}
	return expanded
}

func expandSelectorValues(values []string) []string {
	if len(values) == 0 {
		return []string{""}
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return sorted
}

func formatPortRange(port PortRange) string {
	if port.End <= 0 || port.End == port.Start {
		return strconv.Itoa(port.Start)
	}
	return fmt.Sprintf("%d:%d", port.Start, port.End)
}

func generationRuleChainPrefix(generation string) string {
	switch generation {
	case markChainA:
		return "SVPNA_"
	case markChainB:
		return "SVPNB_"
	default:
		return ""
	}
}

func generationRuleChainName(generation string, bindingIndex int, isIPv6 bool) string {
	prefix := generationRuleChainPrefix(generation)
	if prefix == "" {
		prefix = "SVPNX_"
	}
	if bindingIndex < 0 {
		bindingIndex = 0
	}
	family := "4"
	if isIPv6 {
		family = "6"
	}
	return fmt.Sprintf("%s%03d_%s", prefix, bindingIndex+1, family)
}

func portsOverlapForExclusion(includePort PortRange, excludedPort PortRange) bool {
	includeProtocol := strings.ToLower(strings.TrimSpace(includePort.Protocol))
	excludedProtocol := strings.ToLower(strings.TrimSpace(excludedPort.Protocol))
	if excludedProtocol == "" {
		return false
	}
	if includeProtocol != "" && includeProtocol != excludedProtocol {
		return false
	}
	if includeProtocol == "" {
		return true
	}
	includeStart, includeEnd := portBounds(includePort)
	excludedStart, excludedEnd := portBounds(excludedPort)
	return includeStart <= excludedEnd && excludedStart <= includeEnd
}

func portBounds(port PortRange) (int, int) {
	start := port.Start
	end := port.End
	if end <= 0 {
		end = start
	}
	return start, end
}
