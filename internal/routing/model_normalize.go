package routing

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"split-vpn-webui/internal/vpn"
)

func normalizeCIDRs(raw []string, label string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		canonical, err := canonicalCIDROrIP(trimmed)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid %s selector %q: %v", ErrGroupValidation, label, entry, err)
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

func normalizeInterfaces(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.ToLower(strings.TrimSpace(entry))
		if trimmed == "" {
			continue
		}
		if !ifaceNamePattern.MatchString(trimmed) {
			return nil, fmt.Errorf("%w: invalid source interface selector %q", ErrGroupValidation, entry)
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

func normalizeMACs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		hw, err := net.ParseMAC(trimmed)
		if err != nil || len(hw) != 6 {
			return nil, fmt.Errorf("%w: invalid source mac selector %q", ErrGroupValidation, entry)
		}
		canonical := strings.ToLower(hw.String())
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

func canonicalCIDROrIP(value string) (string, error) {
	if ip := net.ParseIP(value); ip != nil {
		if ip.To4() != nil {
			return ip.String() + "/32", nil
		}
		return ip.String() + "/128", nil
	}
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		return "", err
	}
	prefix, bits := network.Mask.Size()
	if ip.To4() != nil && bits == 32 {
		return network.IP.To4().String() + "/" + strconv.Itoa(prefix), nil
	}
	return network.IP.String() + "/" + strconv.Itoa(prefix), nil
}

func normalizePorts(raw []PortRange) ([]PortRange, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]PortRange, 0, len(raw))
	for _, entry := range raw {
		protocol := strings.ToLower(strings.TrimSpace(entry.Protocol))
		if protocol != "tcp" && protocol != "udp" && protocol != "both" {
			return nil, fmt.Errorf("%w: protocol must be tcp, udp, or both", ErrGroupValidation)
		}
		start := entry.Start
		end := entry.End
		if start <= 0 || start > 65535 {
			return nil, fmt.Errorf("%w: destination port start %d is invalid", ErrGroupValidation, start)
		}
		if end <= 0 {
			end = start
		}
		if end < start || end > 65535 {
			return nil, fmt.Errorf("%w: destination port range %d-%d is invalid", ErrGroupValidation, start, end)
		}
		key := fmt.Sprintf("%s:%d:%d", protocol, start, end)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, PortRange{Protocol: protocol, Start: start, End: end})
	}
	return out, nil
}

func normalizeASNs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.ToUpper(strings.TrimSpace(entry))
		trimmed = strings.TrimPrefix(trimmed, "AS")
		if trimmed == "" {
			continue
		}
		value, err := strconv.Atoi(trimmed)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%w: invalid ASN %q", ErrGroupValidation, entry)
		}
		normalized := "AS" + strconv.Itoa(value)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeDomains(raw []string, wildcard bool) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	domains := make([]string, 0, len(raw))
	for _, domain := range raw {
		trimmed := strings.ToLower(strings.TrimSpace(domain))
		if trimmed == "" {
			continue
		}
		if err := vpn.ValidateDomain(trimmed); err != nil {
			return nil, fmt.Errorf("%w: invalid domain %q: %v", ErrGroupValidation, domain, err)
		}
		if wildcard && !strings.HasPrefix(trimmed, "*.") {
			trimmed = "*." + strings.TrimPrefix(trimmed, "*.")
		}
		if !wildcard {
			trimmed = strings.TrimPrefix(trimmed, "*.")
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		domains = append(domains, trimmed)
	}
	return domains, nil
}

func ruleHasSelectors(rule RoutingRule) bool {
	return len(rule.SourceInterfaces) > 0 ||
		len(rule.SourceCIDRs) > 0 ||
		len(rule.ExcludedSourceCIDRs) > 0 ||
		len(rule.SourceMACs) > 0 ||
		len(rule.DestinationCIDRs) > 0 ||
		len(rule.ExcludedDestinationCIDRs) > 0 ||
		len(rule.DestinationPorts) > 0 ||
		len(rule.ExcludedDestinationPorts) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.ExcludedDestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0
}

// RuleExcludeMulticastEnabled returns whether multicast traffic should be excluded for a rule.
// Nil means enabled by default for backward compatibility and safer behavior.
func RuleExcludeMulticastEnabled(rule RoutingRule) bool {
	if rule.ExcludeMulticast == nil {
		return true
	}
	return *rule.ExcludeMulticast
}
