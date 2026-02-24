package server

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"split-vpn-webui/internal/routing"
)

const flowInspectorIPSetTimeout = 4 * time.Second

type compiledFlowRule struct {
	SourcePrefixes            []netip.Prefix
	DestinationPrefixes       []netip.Prefix
	SourceInterfaces          map[string]struct{}
	SourceMACs                map[string]struct{}
	DestinationPorts          []routing.PortRange
	RequiresSourcePrefix      bool
	RequiresDestinationPrefix bool
	DomainHints               []string
}

type domainPrefixHint struct {
	Prefix netip.Prefix
	Domain string
}

type interfacePrefix struct {
	Name   string
	Prefix netip.Prefix
}

func (s *Server) collectVPNFlowSamples(ctx context.Context, vpnName string) ([]flowInspectorSample, string, error) {
	if s.routingManager == nil || s.flowRunner == nil {
		return nil, "", nil
	}
	groups, err := s.routingManager.ListGroups(ctx)
	if err != nil {
		return nil, "", err
	}
	resolved, err := s.routingManager.LoadResolverSnapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	prewarmed, err := s.routingManager.LoadPrewarmSnapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	setSnapshots, err := readIPSetSnapshots(flowInspectorIPSetTimeout)
	if err != nil {
		return nil, "", err
	}
	conntrackFlows, err := s.flowRunner.Snapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	if s.diagLog != nil {
		s.diagLog.Debugf("flow_inspector collect snapshot vpn=%s conntrack_flows=%d groups=%d", vpnName, len(conntrackFlows), len(groups))
	}

	interfaceName := ""
	vpnMark := uint32(0)
	if cfg, cfgErr := s.configManager.Get(vpnName); cfgErr == nil && cfg != nil {
		interfaceName = strings.TrimSpace(cfg.InterfaceName)
	}
	if s.vpnManager != nil {
		if profile, profileErr := s.vpnManager.Get(vpnName); profileErr == nil && profile != nil {
			vpnMark = profile.FWMark
		}
	}
	compiledRules := compileFlowRules(vpnName, groups, setSnapshots, resolved, prewarmed)
	if len(compiledRules) == 0 {
		if s.diagLog != nil {
			s.diagLog.Warnf("flow_inspector collect vpn=%s has no compiled routing rules", vpnName)
		}
		return nil, interfaceName, nil
	}
	domainHints := buildDomainPrefixHints(resolved)
	localInterfacePrefixes := listLocalInterfacePrefixes()
	devices := loadDeviceDirectory(ctx)
	result := make([]flowInspectorSample, 0, len(conntrackFlows))
	seen := make(map[string]struct{}, len(conntrackFlows))
	sourceParsed := 0
	matched := 0
	matchedByMark := 0

	for _, flow := range conntrackFlows {
		sourceAddr, sourceOK := parseIPToAddr(flow.SourceIP)
		destinationAddr, destinationOK := parseIPToAddr(flow.DestinationIP)
		if !sourceOK || !destinationOK {
			continue
		}
		sourceParsed++
		sourceMAC := strings.ToLower(strings.TrimSpace(devices.lookupIPMAC(flow.SourceIP)))
		sourceDevice := strings.TrimSpace(devices.lookupIP(flow.SourceIP))
		if sourceDevice == "" && sourceMAC != "" {
			if name, _ := devices.lookupMAC(sourceMAC); strings.TrimSpace(name) != "" {
				sourceDevice = strings.TrimSpace(name)
			}
		}
		sourceInterface := resolveSourceInterface(localInterfacePrefixes, sourceAddr)
		matchedRule := matchFlowRule(compiledRules, flow, sourceAddr, destinationAddr, sourceMAC, sourceInterface)
		matchedViaMark := false
		if matchedRule == nil && vpnMark >= 200 && flow.Mark == vpnMark {
			matchedViaMark = true
		}
		if matchedRule == nil && !matchedViaMark {
			continue
		}
		matched++
		if matchedViaMark {
			matchedByMark++
		}

		destinationDomain := lookupDestinationDomain(domainHints, destinationAddr)
		if matchedRule != nil && destinationDomain == "" && len(matchedRule.DomainHints) > 0 {
			destinationDomain = matchedRule.DomainHints[0]
		}

		if _, exists := seen[flow.Key]; exists {
			continue
		}
		seen[flow.Key] = struct{}{}
		result = append(result, flowInspectorSample{
			Key:               flow.Key,
			Protocol:          flow.Protocol,
			SourceIP:          flow.SourceIP,
			SourcePort:        flow.SourcePort,
			SourceMAC:         sourceMAC,
			SourceDeviceName:  sourceDevice,
			SourceInterface:   sourceInterface,
			DestinationIP:     flow.DestinationIP,
			DestinationPort:   flow.DestinationPort,
			DestinationDomain: destinationDomain,
			UploadBytes:       flow.UploadBytes,
			DownloadBytes:     flow.DownloadBytes,
		})
	}
	if s.diagLog != nil {
		s.diagLog.Debugf(
			"flow_inspector collect vpn=%s interface=%s compiled_rules=%d parsed=%d matched=%d emitted=%d",
			vpnName,
			interfaceName,
			len(compiledRules),
			sourceParsed,
			matched,
			len(result),
		)
		if matchedByMark > 0 {
			s.diagLog.Debugf("flow_inspector collect vpn=%s matched_via_conntrack_mark=%d mark=0x%x", vpnName, matchedByMark, vpnMark)
		}
	}
	return result, interfaceName, nil
}

func compileFlowRules(
	vpnName string,
	groups []routing.DomainGroup,
	snapshots map[string]ipsetSnapshot,
	resolved map[routing.ResolverSelector]routing.ResolverValues,
	prewarmed map[string]routing.ResolverValues,
) []compiledFlowRule {
	rules := make([]compiledFlowRule, 0)
	for _, group := range groups {
		if strings.TrimSpace(group.EgressVPN) != strings.TrimSpace(vpnName) {
			continue
		}
		for ruleIndex, rule := range group.Rules {
			if !ruleHasAnySelectors(rule) {
				continue
			}
			pair := routing.RuleSetNames(group.Name, ruleIndex)
			compiled := compiledFlowRule{
				SourcePrefixes:            nil,
				DestinationPrefixes:       nil,
				SourceInterfaces:          makeSelectorSet(rule.SourceInterfaces),
				SourceMACs:                makeMACSet(rule.SourceMACs),
				DestinationPorts:          append([]routing.PortRange(nil), rule.DestinationPorts...),
				RequiresSourcePrefix:      len(rule.SourceCIDRs) > 0,
				RequiresDestinationPrefix: len(rule.DestinationCIDRs) > 0 || len(rule.DestinationASNs) > 0 || len(rule.Domains) > 0 || len(rule.WildcardDomains) > 0,
				DomainHints:               collectRuleDomainHints(rule),
			}

			sourceCandidates := append([]string(nil), snapshots[pair.SourceV4].Members...)
			sourceCandidates = append(sourceCandidates, snapshots[pair.SourceV6].Members...)
			if len(sourceCandidates) == 0 {
				sourceCandidates = append(sourceCandidates, rule.SourceCIDRs...)
			}
			compiled.SourcePrefixes = parsePrefixList(sourceCandidates)

			destinationCandidates := append([]string(nil), snapshots[pair.DestinationV4].Members...)
			destinationCandidates = append(destinationCandidates, snapshots[pair.DestinationV6].Members...)
			if len(destinationCandidates) == 0 {
				destinationCandidates = append(destinationCandidates, destinationRawMembers(rule, pair, resolved, prewarmed)...)
			}
			compiled.DestinationPrefixes = parsePrefixList(destinationCandidates)
			rules = append(rules, compiled)
		}
	}
	return rules
}

func ruleHasAnySelectors(rule routing.RoutingRule) bool {
	return len(rule.SourceInterfaces) > 0 ||
		len(rule.SourceCIDRs) > 0 ||
		len(rule.SourceMACs) > 0 ||
		len(rule.DestinationCIDRs) > 0 ||
		len(rule.DestinationPorts) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0
}

func makeSelectorSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func makeMACSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectRuleDomainHints(rule routing.RoutingRule) []string {
	hints := make([]string, 0, len(rule.Domains)+len(rule.WildcardDomains))
	seen := make(map[string]struct{}, len(rule.Domains)+len(rule.WildcardDomains))
	for _, domain := range rule.Domains {
		trimmed := strings.TrimSpace(domain)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		hints = append(hints, trimmed)
	}
	for _, wildcard := range rule.WildcardDomains {
		trimmed := strings.TrimSpace(wildcard)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		hints = append(hints, trimmed)
	}
	return hints
}

func parsePrefixList(values []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		prefix, ok := parsePrefix(value)
		if !ok {
			continue
		}
		key := prefix.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Bits() == prefixes[j].Bits() {
			return prefixes[i].String() < prefixes[j].String()
		}
		return prefixes[i].Bits() > prefixes[j].Bits()
	})
	return prefixes
}

func parsePrefix(raw string) (netip.Prefix, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return netip.Prefix{}, false
	}
	if parsed, err := netip.ParsePrefix(trimmed); err == nil {
		return parsed.Masked(), true
	}
	addr, ok := parseIPToAddr(trimmed)
	if !ok {
		return netip.Prefix{}, false
	}
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), true
	}
	return netip.PrefixFrom(addr, 128), true
}

func parseIPToAddr(raw string) (netip.Addr, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return netip.Addr{}, false
	}
	if index := strings.Index(trimmed, "%"); index >= 0 {
		trimmed = trimmed[:index]
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		return addr.Unmap(), true
	}
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return netip.Addr{}, false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func matchFlowRule(
	rules []compiledFlowRule,
	flow conntrackFlowSample,
	sourceAddr netip.Addr,
	destinationAddr netip.Addr,
	sourceMAC string,
	sourceInterface string,
) *compiledFlowRule {
	sourceMAC = strings.ToLower(strings.TrimSpace(sourceMAC))
	sourceInterface = strings.ToLower(strings.TrimSpace(sourceInterface))
	for i := range rules {
		rule := &rules[i]
		if rule.RequiresSourcePrefix && !prefixContains(rule.SourcePrefixes, sourceAddr) {
			continue
		}
		if len(rule.SourceInterfaces) > 0 {
			if sourceInterface == "" {
				continue
			}
			if _, ok := rule.SourceInterfaces[sourceInterface]; !ok {
				continue
			}
		}
		if len(rule.SourceMACs) > 0 {
			if sourceMAC == "" {
				continue
			}
			if _, ok := rule.SourceMACs[sourceMAC]; !ok {
				continue
			}
		}
		if rule.RequiresDestinationPrefix && !prefixContains(rule.DestinationPrefixes, destinationAddr) {
			continue
		}
		if len(rule.DestinationPorts) > 0 && !matchDestinationPort(rule.DestinationPorts, flow.Protocol, flow.DestinationPort) {
			continue
		}
		return rule
	}
	return nil
}

func prefixContains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func matchDestinationPort(ports []routing.PortRange, protocol string, destinationPort int) bool {
	if destinationPort <= 0 {
		return false
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	for _, port := range ports {
		rangeProtocol := strings.ToLower(strings.TrimSpace(port.Protocol))
		if rangeProtocol == "" {
			rangeProtocol = "both"
		}
		if rangeProtocol != "both" && rangeProtocol != protocol {
			continue
		}
		start := port.Start
		end := port.End
		if end <= 0 {
			end = start
		}
		if destinationPort >= start && destinationPort <= end {
			return true
		}
	}
	return false
}

func buildDomainPrefixHints(snapshot map[routing.ResolverSelector]routing.ResolverValues) []domainPrefixHint {
	hints := make([]domainPrefixHint, 0, len(snapshot))
	seen := make(map[string]struct{})
	for selector, values := range snapshot {
		selectorType := strings.ToLower(strings.TrimSpace(selector.Type))
		if selectorType != "domain" && selectorType != "wildcard" {
			continue
		}
		label := strings.TrimSpace(selector.Key)
		if label == "" {
			continue
		}
		for _, cidr := range values.V4 {
			prefix, ok := parsePrefix(cidr)
			if !ok {
				continue
			}
			key := prefix.String() + "|" + label
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			hints = append(hints, domainPrefixHint{Prefix: prefix, Domain: label})
		}
		for _, cidr := range values.V6 {
			prefix, ok := parsePrefix(cidr)
			if !ok {
				continue
			}
			key := prefix.String() + "|" + label
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			hints = append(hints, domainPrefixHint{Prefix: prefix, Domain: label})
		}
	}
	sort.Slice(hints, func(i, j int) bool {
		if hints[i].Prefix.Bits() == hints[j].Prefix.Bits() {
			if hints[i].Prefix.String() == hints[j].Prefix.String() {
				return hints[i].Domain < hints[j].Domain
			}
			return hints[i].Prefix.String() < hints[j].Prefix.String()
		}
		return hints[i].Prefix.Bits() > hints[j].Prefix.Bits()
	})
	return hints
}

func lookupDestinationDomain(hints []domainPrefixHint, destination netip.Addr) string {
	for _, hint := range hints {
		if hint.Prefix.Contains(destination) {
			return hint.Domain
		}
	}
	return ""
}

func listLocalInterfacePrefixes() []interfacePrefix {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	prefixes := make([]interfacePrefix, 0, len(interfaces)*2)
	for _, iface := range interfaces {
		if strings.TrimSpace(iface.Name) == "" {
			continue
		}
		addresses, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok || network == nil {
				continue
			}
			ipAddr, ok := netip.AddrFromSlice(network.IP)
			if !ok {
				continue
			}
			bits, _ := network.Mask.Size()
			prefix := netip.PrefixFrom(ipAddr.Unmap(), bits).Masked()
			prefixes = append(prefixes, interfacePrefix{
				Name:   strings.ToLower(strings.TrimSpace(iface.Name)),
				Prefix: prefix,
			})
		}
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Prefix.Bits() == prefixes[j].Prefix.Bits() {
			return prefixes[i].Name < prefixes[j].Name
		}
		return prefixes[i].Prefix.Bits() > prefixes[j].Prefix.Bits()
	})
	return prefixes
}

func resolveSourceInterface(prefixes []interfacePrefix, address netip.Addr) string {
	for _, entry := range prefixes {
		if entry.Prefix.Contains(address) {
			return entry.Name
		}
	}
	return ""
}
