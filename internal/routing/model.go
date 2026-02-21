package routing

import (
	"fmt"
	"hash/fnv"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"split-vpn-webui/internal/vpn"
)

const (
	setPrefix       = "svpn_"
	setSuffixV4     = "_v4"
	setSuffixV6     = "_v6"
	maxIPSetNameLen = 31
)

var (
	groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

	// ErrGroupNotFound indicates the requested group id does not exist.
	ErrGroupNotFound = fmt.Errorf("domain group not found")
	// ErrGroupValidation indicates invalid input payload.
	ErrGroupValidation = fmt.Errorf("domain group validation failed")
)

// DomainGroup is a persisted routing group assigned to one egress VPN.
type DomainGroup struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	EgressVPN string        `json:"egressVpn"`
	Rules     []RoutingRule `json:"rules"`
	// Domains is a legacy compatibility field. New clients should use Rules.
	Domains   []string `json:"domains,omitempty"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

// RoutingRule defines one AND-combined selector rule inside a group.
type RoutingRule struct {
	ID               int64       `json:"id,omitempty"`
	Name             string      `json:"name,omitempty"`
	SourceCIDRs      []string    `json:"sourceCidrs,omitempty"`
	DestinationCIDRs []string    `json:"destinationCidrs,omitempty"`
	DestinationPorts []PortRange `json:"destinationPorts,omitempty"`
	DestinationASNs  []string    `json:"destinationAsns,omitempty"`
	Domains          []string    `json:"domains,omitempty"`
	WildcardDomains  []string    `json:"wildcardDomains,omitempty"`
}

// PortRange matches one destination port/range for a specific L4 protocol.
type PortRange struct {
	Protocol string `json:"protocol"`
	Start    int    `json:"start"`
	End      int    `json:"end,omitempty"`
}

// RouteBinding describes ipset/routing state derived from a group rule and VPN.
type RouteBinding struct {
	GroupName        string
	RuleIndex        int
	RuleName         string
	SourceSetV4      string
	SourceSetV6      string
	DestinationSetV4 string
	DestinationSetV6 string
	HasSource        bool
	HasDestination   bool
	DestinationPorts []PortRange
	Mark             uint32
	RouteTable       int
	Interface        string
	EgressVPN        string
}

// NormalizeAndValidate validates a group and returns a canonical version.
func NormalizeAndValidate(group DomainGroup) (DomainGroup, error) {
	trimmedName := strings.TrimSpace(group.Name)
	if trimmedName == "" {
		return DomainGroup{}, fmt.Errorf("%w: group name is required", ErrGroupValidation)
	}
	if !groupNamePattern.MatchString(trimmedName) {
		return DomainGroup{}, fmt.Errorf("%w: group name %q is invalid", ErrGroupValidation, group.Name)
	}
	egress := strings.TrimSpace(group.EgressVPN)
	if err := vpn.ValidateName(egress); err != nil {
		return DomainGroup{}, fmt.Errorf("%w: invalid egress vpn: %v", ErrGroupValidation, err)
	}

	rules := append([]RoutingRule(nil), group.Rules...)
	if len(rules) == 0 && len(group.Domains) > 0 {
		// Legacy payload compatibility.
		rules = []RoutingRule{{Domains: append([]string(nil), group.Domains...)}}
	}
	if len(rules) == 0 {
		return DomainGroup{}, fmt.Errorf("%w: at least one rule is required", ErrGroupValidation)
	}
	normalizedRules, err := normalizeRules(rules)
	if err != nil {
		return DomainGroup{}, err
	}

	group.Name = trimmedName
	group.EgressVPN = egress
	group.Rules = normalizedRules
	group.Domains = legacyDomainsFromRules(normalizedRules)
	return group, nil
}

func normalizeRules(raw []RoutingRule) ([]RoutingRule, error) {
	out := make([]RoutingRule, 0, len(raw))
	for idx, entry := range raw {
		rule, err := normalizeRule(entry, idx)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

func normalizeRule(raw RoutingRule, idx int) (RoutingRule, error) {
	var err error
	rule := RoutingRule{
		ID:   raw.ID,
		Name: strings.TrimSpace(raw.Name),
	}
	if rule.Name == "" {
		rule.Name = fmt.Sprintf("Rule %d", idx+1)
	}
	rule.SourceCIDRs, err = normalizeCIDRs(raw.SourceCIDRs, "source")
	if err != nil {
		return RoutingRule{}, err
	}
	rule.DestinationCIDRs, err = normalizeCIDRs(raw.DestinationCIDRs, "destination")
	if err != nil {
		return RoutingRule{}, err
	}
	rule.DestinationPorts, err = normalizePorts(raw.DestinationPorts)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.DestinationASNs, err = normalizeASNs(raw.DestinationASNs)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.Domains, err = normalizeDomains(raw.Domains, false)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.WildcardDomains, err = normalizeDomains(raw.WildcardDomains, true)
	if err != nil {
		return RoutingRule{}, err
	}
	if !ruleHasSelectors(rule) {
		return RoutingRule{}, fmt.Errorf("%w: rule %d must include at least one selector", ErrGroupValidation, idx+1)
	}
	return rule, nil
}

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
	sort.Strings(out)
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
		if protocol != "tcp" && protocol != "udp" {
			return nil, fmt.Errorf("%w: protocol must be tcp or udp", ErrGroupValidation)
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Protocol == out[j].Protocol {
			if out[i].Start == out[j].Start {
				return out[i].End < out[j].End
			}
			return out[i].Start < out[j].Start
		}
		return out[i].Protocol < out[j].Protocol
	})
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
	sort.Strings(out)
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
	sort.Strings(domains)
	return domains, nil
}

func ruleHasSelectors(rule RoutingRule) bool {
	return len(rule.SourceCIDRs) > 0 ||
		len(rule.DestinationCIDRs) > 0 ||
		len(rule.DestinationPorts) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0
}

func legacyDomainsFromRules(rules []RoutingRule) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, rule := range rules {
		for _, domain := range rule.Domains {
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
		for _, wildcard := range rule.WildcardDomains {
			if _, exists := seen[wildcard]; exists {
				continue
			}
			seen[wildcard] = struct{}{}
			out = append(out, wildcard)
		}
	}
	sort.Strings(out)
	return out
}

// RuleDomains returns exact + wildcard domains for resolver pipelines.
func RuleDomains(group DomainGroup) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, rule := range group.Rules {
		for _, domain := range rule.Domains {
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
		for _, wildcard := range rule.WildcardDomains {
			trimmed := strings.TrimPrefix(wildcard, "*.")
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		for _, legacy := range group.Domains {
			trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(legacy), "*."))
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

// GroupSetNames derives deterministic ipset names for a group.
func GroupSetNames(groupName string) (string, string) {
	rule := RuleSetNames(groupName, 0)
	return rule.DestinationV4, rule.DestinationV6
}

// RuleSetPair is deterministic per-group per-rule source+destination ipset names.
type RuleSetPair struct {
	SourceV4      string
	SourceV6      string
	DestinationV4 string
	DestinationV6 string
}

// RuleSetNames returns deterministic source/destination set names for one rule.
func RuleSetNames(groupName string, ruleIndex int) RuleSetPair {
	base := normalizeSetBase(groupName)
	if ruleIndex < 0 {
		ruleIndex = 0
	}
	seed := strings.ToLower(fmt.Sprintf("%s:%d", groupName, ruleIndex))
	return RuleSetPair{
		SourceV4:      compactSetName(base, fmt.Sprintf("r%ds4", ruleIndex+1), seed+":src4"),
		SourceV6:      compactSetName(base, fmt.Sprintf("r%ds6", ruleIndex+1), seed+":src6"),
		DestinationV4: compactSetName(base, fmt.Sprintf("r%dd4", ruleIndex+1), seed+":dst4"),
		DestinationV6: compactSetName(base, fmt.Sprintf("r%dd6", ruleIndex+1), seed+":dst6"),
	}
}

func compactSetName(base, suffix, seed string) string {
	name := setPrefix + base + "_" + suffix
	if len(name) <= maxIPSetNameLen {
		return name
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	hash := fmt.Sprintf("%08x", h.Sum32())
	maxBase := maxIPSetNameLen - len(setPrefix) - len(suffix) - len(hash) - 2
	if maxBase < 3 {
		maxBase = 3
	}
	shortBase := base
	if len(shortBase) > maxBase {
		shortBase = shortBase[:maxBase]
	}
	return setPrefix + shortBase + "_" + hash + "_" + suffix
}

func normalizeSetBase(groupName string) string {
	input := strings.ToLower(strings.TrimSpace(groupName))
	if input == "" {
		return "group"
	}
	builder := strings.Builder{}
	builder.Grow(len(input))
	lastUnderscore := false
	for _, r := range input {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteRune('_')
			lastUnderscore = true
		}
	}
	base := strings.Trim(builder.String(), "_")
	if base == "" {
		base = "group"
	}
	return base
}
