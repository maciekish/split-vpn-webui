package routing

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
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
	ifaceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,14}$`)

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
	SourceInterfaces []string    `json:"sourceInterfaces,omitempty"`
	SourceCIDRs      []string    `json:"sourceCidrs,omitempty"`
	SourceMACs       []string    `json:"sourceMacs,omitempty"`
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
	SourceInterfaces []string
	SourceSetV4      string
	SourceSetV6      string
	SourceMACs       []string
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
	rule.SourceInterfaces, err = normalizeInterfaces(raw.SourceInterfaces)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.SourceCIDRs, err = normalizeCIDRs(raw.SourceCIDRs, "source")
	if err != nil {
		return RoutingRule{}, err
	}
	rule.SourceMACs, err = normalizeMACs(raw.SourceMACs)
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
