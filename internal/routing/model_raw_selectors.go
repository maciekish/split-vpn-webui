package routing

import (
	"fmt"
	"strconv"
	"strings"
)

func (raw RuleRawSelectors) hasAnyLine() bool {
	for _, list := range [][]string{
		raw.SourceInterfaces,
		raw.SourceCIDRs,
		raw.SourceMACs,
		raw.DestinationCIDRs,
		raw.DestinationPorts,
		raw.DestinationASNs,
		raw.Domains,
		raw.WildcardDomains,
	} {
		for _, line := range list {
			if strings.TrimSpace(line) != "" {
				return true
			}
		}
	}
	return false
}

func normalizeRuleRawSelectors(in *RuleRawSelectors) RuleRawSelectors {
	if in == nil {
		return RuleRawSelectors{}
	}
	return RuleRawSelectors{
		SourceInterfaces: normalizeRawLines(in.SourceInterfaces),
		SourceCIDRs:      normalizeRawLines(in.SourceCIDRs),
		SourceMACs:       normalizeRawLines(in.SourceMACs),
		DestinationCIDRs: normalizeRawLines(in.DestinationCIDRs),
		DestinationPorts: normalizeRawLines(in.DestinationPorts),
		DestinationASNs:  normalizeRawLines(in.DestinationASNs),
		Domains:          normalizeRawLines(in.Domains),
		WildcardDomains:  normalizeRawLines(in.WildcardDomains),
	}
}

func hydrateRuleRawSelectorsFromRule(rawSelectors RuleRawSelectors, rule RoutingRule) RuleRawSelectors {
	if len(rawSelectors.SourceInterfaces) == 0 {
		rawSelectors.SourceInterfaces = cloneSelectorLines(rule.SourceInterfaces)
	}
	if len(rawSelectors.SourceCIDRs) == 0 {
		rawSelectors.SourceCIDRs = cloneSelectorLines(rule.SourceCIDRs)
	}
	if len(rawSelectors.SourceMACs) == 0 {
		rawSelectors.SourceMACs = cloneSelectorLines(rule.SourceMACs)
	}
	if len(rawSelectors.DestinationCIDRs) == 0 {
		rawSelectors.DestinationCIDRs = cloneSelectorLines(rule.DestinationCIDRs)
	}
	if len(rawSelectors.DestinationPorts) == 0 {
		rawSelectors.DestinationPorts = formatPortSelectorLines(rule.DestinationPorts)
	}
	if len(rawSelectors.DestinationASNs) == 0 {
		rawSelectors.DestinationASNs = cloneSelectorLines(rule.DestinationASNs)
	}
	if len(rawSelectors.Domains) == 0 {
		rawSelectors.Domains = cloneSelectorLines(rule.Domains)
	}
	if len(rawSelectors.WildcardDomains) == 0 {
		rawSelectors.WildcardDomains = cloneSelectorLines(rule.WildcardDomains)
	}
	return rawSelectors
}

func finalizeRuleRawSelectors(raw RuleRawSelectors, rule RoutingRule) RuleRawSelectors {
	if len(raw.SourceInterfaces) == 0 {
		raw.SourceInterfaces = cloneSelectorLines(rule.SourceInterfaces)
	}
	if len(raw.SourceCIDRs) == 0 {
		raw.SourceCIDRs = cloneSelectorLines(rule.SourceCIDRs)
	}
	if len(raw.SourceMACs) == 0 {
		raw.SourceMACs = cloneSelectorLines(rule.SourceMACs)
	}
	if len(raw.DestinationCIDRs) == 0 {
		raw.DestinationCIDRs = cloneSelectorLines(rule.DestinationCIDRs)
	}
	if len(raw.DestinationPorts) == 0 {
		raw.DestinationPorts = formatPortSelectorLines(rule.DestinationPorts)
	}
	if len(raw.DestinationASNs) == 0 {
		raw.DestinationASNs = cloneSelectorLines(rule.DestinationASNs)
	}
	if len(raw.Domains) == 0 {
		raw.Domains = cloneSelectorLines(rule.Domains)
	}
	if len(raw.WildcardDomains) == 0 {
		raw.WildcardDomains = cloneSelectorLines(rule.WildcardDomains)
	}
	return raw
}

func selectorValuesFromRaw(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		value, ok := activeSelectorValue(line)
		if !ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func activeSelectorValue(line string) (string, bool) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if index := strings.Index(trimmed, "#"); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func normalizeRawLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.ReplaceAll(line, "\r", ""))
	}
	return out
}

func cloneSelectorLines(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

func formatPortSelectorLines(ports []PortRange) []string {
	if len(ports) == 0 {
		return nil
	}
	out := make([]string, 0, len(ports))
	for _, port := range ports {
		proto := strings.ToLower(strings.TrimSpace(port.Protocol))
		if proto == "" || port.Start <= 0 {
			continue
		}
		if port.End > port.Start {
			out = append(out, proto+":"+strconv.Itoa(port.Start)+"-"+strconv.Itoa(port.End))
			continue
		}
		out = append(out, proto+":"+strconv.Itoa(port.Start))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parsePortSelectorStrings(values []string) ([]PortRange, error) {
	out := make([]PortRange, 0, len(values))
	for _, raw := range values {
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		if trimmed == "" {
			continue
		}
		parts := strings.FieldsFunc(trimmed, func(r rune) bool {
			return r == ':' || r == '/'
		})
		if len(parts) != 2 {
			return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
		}
		protocol := strings.TrimSpace(parts[0])
		rangeRaw := strings.TrimSpace(parts[1])
		if protocol != "tcp" && protocol != "udp" && protocol != "both" {
			return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
		}

		start := 0
		end := 0
		if strings.Contains(rangeRaw, "-") {
			bounds := strings.SplitN(rangeRaw, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
			}
			var err error
			start, err = strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
			}
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
			}
		} else {
			value, err := strconv.Atoi(rangeRaw)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid port selector %q", ErrGroupValidation, raw)
			}
			start = value
			end = value
		}
		out = append(out, PortRange{
			Protocol: protocol,
			Start:    start,
			End:      end,
		})
	}
	return out, nil
}
