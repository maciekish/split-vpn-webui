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
	ID                       int64             `json:"id,omitempty"`
	Name                     string            `json:"name,omitempty"`
	SourceInterfaces         []string          `json:"sourceInterfaces,omitempty"`
	SourceCIDRs              []string          `json:"sourceCidrs,omitempty"`
	ExcludedSourceCIDRs      []string          `json:"excludedSourceCidrs,omitempty"`
	SourceMACs               []string          `json:"sourceMacs,omitempty"`
	DestinationCIDRs         []string          `json:"destinationCidrs,omitempty"`
	ExcludedDestinationCIDRs []string          `json:"excludedDestinationCidrs,omitempty"`
	DestinationPorts         []PortRange       `json:"destinationPorts,omitempty"`
	ExcludedDestinationPorts []PortRange       `json:"excludedDestinationPorts,omitempty"`
	DestinationASNs          []string          `json:"destinationAsns,omitempty"`
	ExcludedDestinationASNs  []string          `json:"excludedDestinationAsns,omitempty"`
	ExcludeMulticast         *bool             `json:"excludeMulticast,omitempty"`
	Domains                  []string          `json:"domains,omitempty"`
	WildcardDomains          []string          `json:"wildcardDomains,omitempty"`
	RawSelectors             *RuleRawSelectors `json:"rawSelectors,omitempty"`
}

// RuleRawSelectors preserves user-entered selector lines (including comments).
type RuleRawSelectors struct {
	SourceInterfaces         []string `json:"sourceInterfaces,omitempty"`
	SourceCIDRs              []string `json:"sourceCidrs,omitempty"`
	ExcludedSourceCIDRs      []string `json:"excludedSourceCidrs,omitempty"`
	SourceMACs               []string `json:"sourceMacs,omitempty"`
	DestinationCIDRs         []string `json:"destinationCidrs,omitempty"`
	ExcludedDestinationCIDRs []string `json:"excludedDestinationCidrs,omitempty"`
	DestinationPorts         []string `json:"destinationPorts,omitempty"`
	ExcludedDestinationPorts []string `json:"excludedDestinationPorts,omitempty"`
	DestinationASNs          []string `json:"destinationAsns,omitempty"`
	ExcludedDestinationASNs  []string `json:"excludedDestinationAsns,omitempty"`
	Domains                  []string `json:"domains,omitempty"`
	WildcardDomains          []string `json:"wildcardDomains,omitempty"`
}

// PortRange matches one destination port/range for a specific L4 protocol.
type PortRange struct {
	Protocol string `json:"protocol"`
	Start    int    `json:"start"`
	End      int    `json:"end,omitempty"`
}

// RouteBinding describes ipset/routing state derived from a group rule and VPN.
type RouteBinding struct {
	GroupName                string
	RuleIndex                int
	RuleName                 string
	SourceInterfaces         []string
	SourceSetV4              string
	SourceSetV6              string
	ExcludedSourceSetV4      string
	ExcludedSourceSetV6      string
	SourceMACs               []string
	DestinationSetV4         string
	DestinationSetV6         string
	ExcludedDestinationSetV4 string
	ExcludedDestinationSetV6 string
	HasSource                bool
	HasExcludedSource        bool
	HasDestination           bool
	HasExcludedDestination   bool
	DestinationPorts         []PortRange
	ExcludedDestinationPorts []PortRange
	ExcludeMulticast         bool
	Mark                     uint32
	RouteTable               int
	Interface                string
	EgressVPN                string
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
	rawSelectors := normalizeRuleRawSelectors(raw.RawSelectors)
	rawSelectors = hydrateRuleRawSelectorsFromRule(rawSelectors, raw)
	var err error
	rule := RoutingRule{
		ID:   raw.ID,
		Name: strings.TrimSpace(raw.Name),
	}
	if rule.Name == "" {
		rule.Name = fmt.Sprintf("Rule %d", idx+1)
	}
	sourceInterfaces := selectorValuesFromRaw(rawSelectors.SourceInterfaces)
	rule.SourceInterfaces, err = normalizeInterfaces(sourceInterfaces)
	if err != nil {
		return RoutingRule{}, err
	}
	sourceCIDRs := selectorValuesFromRaw(rawSelectors.SourceCIDRs)
	rule.SourceCIDRs, err = normalizeCIDRs(sourceCIDRs, "source")
	if err != nil {
		return RoutingRule{}, err
	}
	excludedSourceCIDRs := selectorValuesFromRaw(rawSelectors.ExcludedSourceCIDRs)
	rule.ExcludedSourceCIDRs, err = normalizeCIDRs(excludedSourceCIDRs, "excluded source")
	if err != nil {
		return RoutingRule{}, err
	}
	sourceMACs := selectorValuesFromRaw(rawSelectors.SourceMACs)
	rule.SourceMACs, err = normalizeMACs(sourceMACs)
	if err != nil {
		return RoutingRule{}, err
	}
	destinationCIDRs := selectorValuesFromRaw(rawSelectors.DestinationCIDRs)
	rule.DestinationCIDRs, err = normalizeCIDRs(destinationCIDRs, "destination")
	if err != nil {
		return RoutingRule{}, err
	}
	excludedDestinationCIDRs := selectorValuesFromRaw(rawSelectors.ExcludedDestinationCIDRs)
	rule.ExcludedDestinationCIDRs, err = normalizeCIDRs(excludedDestinationCIDRs, "excluded destination")
	if err != nil {
		return RoutingRule{}, err
	}
	destinationPorts := append([]PortRange(nil), raw.DestinationPorts...)
	if len(destinationPorts) == 0 {
		destinationPorts, err = parsePortSelectorStrings(selectorValuesFromRaw(rawSelectors.DestinationPorts))
		if err != nil {
			return RoutingRule{}, err
		}
	}
	rule.DestinationPorts, err = normalizePorts(destinationPorts)
	if err != nil {
		return RoutingRule{}, err
	}
	excludedDestinationPorts := append([]PortRange(nil), raw.ExcludedDestinationPorts...)
	if len(excludedDestinationPorts) == 0 {
		excludedDestinationPorts, err = parsePortSelectorStrings(selectorValuesFromRaw(rawSelectors.ExcludedDestinationPorts))
		if err != nil {
			return RoutingRule{}, err
		}
	}
	rule.ExcludedDestinationPorts, err = normalizePorts(excludedDestinationPorts)
	if err != nil {
		return RoutingRule{}, err
	}
	destinationASNs := selectorValuesFromRaw(rawSelectors.DestinationASNs)
	rule.DestinationASNs, err = normalizeASNs(destinationASNs)
	if err != nil {
		return RoutingRule{}, err
	}
	excludedDestinationASNs := selectorValuesFromRaw(rawSelectors.ExcludedDestinationASNs)
	rule.ExcludedDestinationASNs, err = normalizeASNs(excludedDestinationASNs)
	if err != nil {
		return RoutingRule{}, err
	}
	domains := selectorValuesFromRaw(rawSelectors.Domains)
	rule.Domains, err = normalizeDomains(domains, false)
	if err != nil {
		return RoutingRule{}, err
	}
	wildcards := selectorValuesFromRaw(rawSelectors.WildcardDomains)
	rule.WildcardDomains, err = normalizeDomains(wildcards, true)
	if err != nil {
		return RoutingRule{}, err
	}
	rule.ExcludeMulticast = boolPointer(true)
	if raw.ExcludeMulticast != nil {
		rule.ExcludeMulticast = boolPointer(*raw.ExcludeMulticast)
	}
	rawSelectors = finalizeRuleRawSelectors(rawSelectors, rule)
	if !ruleHasSelectors(rule) && !rawSelectors.hasAnyLine() {
		return RoutingRule{}, fmt.Errorf(
			"%w: rule %d must include at least one selector or comment line",
			ErrGroupValidation,
			idx+1,
		)
	}
	rule.RawSelectors = &rawSelectors
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
	SourceV4              string
	SourceV6              string
	ExcludedSourceV4      string
	ExcludedSourceV6      string
	DestinationV4         string
	DestinationV6         string
	ExcludedDestinationV4 string
	ExcludedDestinationV6 string
}

// RuleSetNames returns deterministic source/destination set names for one rule.
func RuleSetNames(groupName string, ruleIndex int) RuleSetPair {
	base := normalizeSetBase(groupName)
	if ruleIndex < 0 {
		ruleIndex = 0
	}
	seed := strings.ToLower(fmt.Sprintf("%s:%d", groupName, ruleIndex))
	return RuleSetPair{
		SourceV4:              compactSetName(base, fmt.Sprintf("r%ds4", ruleIndex+1), seed+":src4"),
		SourceV6:              compactSetName(base, fmt.Sprintf("r%ds6", ruleIndex+1), seed+":src6"),
		ExcludedSourceV4:      compactSetName(base, fmt.Sprintf("r%dxs4", ruleIndex+1), seed+":xsrc4"),
		ExcludedSourceV6:      compactSetName(base, fmt.Sprintf("r%dxs6", ruleIndex+1), seed+":xsrc6"),
		DestinationV4:         compactSetName(base, fmt.Sprintf("r%dd4", ruleIndex+1), seed+":dst4"),
		DestinationV6:         compactSetName(base, fmt.Sprintf("r%dd6", ruleIndex+1), seed+":dst6"),
		ExcludedDestinationV4: compactSetName(base, fmt.Sprintf("r%dxd4", ruleIndex+1), seed+":xdst4"),
		ExcludedDestinationV6: compactSetName(base, fmt.Sprintf("r%dxd6", ruleIndex+1), seed+":xdst6"),
	}
}

func boolPointer(value bool) *bool {
	v := value
	return &v
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
