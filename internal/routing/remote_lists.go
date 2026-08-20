package routing

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Remote list kinds. A list's kind decides which rule selector its entries feed.
const (
	RemoteListKindCIDR     = "cidr"
	RemoteListKindASN      = "asn"
	RemoteListKindDomain   = "domain"
	RemoteListKindWildcard = "wildcard"
)

// RemoteListContent holds one remote list's kind and canonical entries.
type RemoteListContent struct {
	Kind    string
	Entries []string
}

// RemoteListProvider supplies remote list state to the routing manager.
type RemoteListProvider interface {
	// RemoteListContents returns the entries of every *enabled* list, keyed by
	// lower-cased name. A disabled list is absent, so rules referencing it fall
	// back to an empty destination set.
	RemoteListContents(ctx context.Context) (map[string]RemoteListContent, error)
	// RemoteListNames returns the lower-cased name of every configured list,
	// enabled or not, for validating rule references.
	RemoteListNames(ctx context.Context) (map[string]struct{}, error)
}

// RemoteListKinds returns every supported remote list kind.
func RemoteListKinds() []string {
	return []string{RemoteListKindCIDR, RemoteListKindASN, RemoteListKindDomain, RemoteListKindWildcard}
}

// NormalizeRemoteListValues canonicalises raw list values for one kind, drops
// duplicates, and reports how many unusable entries were skipped. Published
// lists occasionally contain junk lines, so a bad entry is skipped rather than
// failing the whole list.
func NormalizeRemoteListValues(kind string, values []string) ([]string, int, error) {
	normalizeOne, err := remoteListValueNormalizer(kind)
	if err != nil {
		return nil, 0, err
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	skipped := 0
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		canonical, ok := normalizeOne(value)
		if !ok {
			skipped++
			continue
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out, skipped, nil
}

func remoteListValueNormalizer(kind string) (func(string) (string, bool), error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case RemoteListKindCIDR:
		return func(value string) (string, bool) {
			canonical, err := canonicalCIDROrIP(strings.TrimSpace(value))
			if err != nil {
				return "", false
			}
			return canonical, true
		}, nil
	case RemoteListKindASN:
		return func(value string) (string, bool) {
			return firstNormalized(normalizeASNs([]string{value}))
		}, nil
	case RemoteListKindDomain:
		return func(value string) (string, bool) {
			return firstNormalized(normalizeDomains([]string{value}, false))
		}, nil
	case RemoteListKindWildcard:
		return func(value string) (string, bool) {
			return firstNormalized(normalizeDomains([]string{value}, true))
		}, nil
	default:
		return nil, fmt.Errorf("unsupported remote list kind %q", kind)
	}
}

func firstNormalized(values []string, err error) (string, bool) {
	if err != nil || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// expandGroupsWithRemoteLists returns a copy of groups whose rules also carry
// the entries of every remote list they reference. The persisted groups are
// left untouched so the editor keeps showing the list reference, not its
// expansion.
func expandGroupsWithRemoteLists(groups []DomainGroup, contents map[string]RemoteListContent) []DomainGroup {
	if len(groups) == 0 {
		return groups
	}
	out := make([]DomainGroup, len(groups))
	copy(out, groups)
	for i := range out {
		if len(out[i].Rules) == 0 {
			continue
		}
		expanded := make([]RoutingRule, len(out[i].Rules))
		for j, rule := range out[i].Rules {
			expanded[j] = expandRuleWithRemoteLists(rule, contents)
		}
		out[i].Rules = expanded
		out[i].Domains = legacyDomainsFromRules(expanded)
	}
	return out
}

func expandRuleWithRemoteLists(rule RoutingRule, contents map[string]RemoteListContent) RoutingRule {
	if len(rule.RemoteLists) == 0 || len(contents) == 0 {
		return rule
	}
	var cidrs, asns, domains, wildcards []string
	for _, name := range rule.RemoteLists {
		content, ok := contents[strings.ToLower(strings.TrimSpace(name))]
		if !ok || len(content.Entries) == 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(content.Kind)) {
		case RemoteListKindCIDR:
			cidrs = append(cidrs, content.Entries...)
		case RemoteListKindASN:
			asns = append(asns, content.Entries...)
		case RemoteListKindDomain:
			domains = append(domains, content.Entries...)
		case RemoteListKindWildcard:
			wildcards = append(wildcards, content.Entries...)
		}
	}
	if len(cidrs) == 0 && len(asns) == 0 && len(domains) == 0 && len(wildcards) == 0 {
		return rule
	}
	expanded := rule
	expanded.DestinationCIDRs = mergeSelectorValues(rule.DestinationCIDRs, cidrs)
	expanded.DestinationASNs = mergeSelectorValues(rule.DestinationASNs, asns)
	expanded.Domains = mergeSelectorValues(rule.Domains, domains)
	expanded.WildcardDomains = mergeSelectorValues(rule.WildcardDomains, wildcards)
	return expanded
}

// mergeSelectorValues appends additions to base, keeping base order first and
// dropping duplicates. Returns base unchanged when there is nothing to add.
func mergeSelectorValues(base []string, additions []string) []string {
	if len(additions) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(additions))
	out := make([]string, 0, len(base)+len(additions))
	for _, value := range base {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
