package routing

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
)

type desiredSetDefinition struct {
	Family  string
	Entries []string
}

func (m *Manager) applyResolverSnapshotLocked(ctx context.Context, snapshot map[ResolverSelector]ResolverValues) error {
	if err := m.store.ReplaceResolverSnapshot(ctx, snapshot); err != nil {
		return err
	}

	groups, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	desiredSets := make(map[string]desiredSetDefinition)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	for _, group := range groups {
		for ruleIndex, rule := range group.Rules {
			if !ruleNeedsDestinationSet(rule) {
				continue
			}
			pair := RuleSetNames(group.Name, ruleIndex)
			destEntries := dedupeSortedStrings(mergeResolvedDestinations(rule, snapshot))
			destV4, destV6 := splitCIDRsByFamily(destEntries)
			queueDesiredSet(desiredSets, nil, pair.DestinationV4, "inet", destV4)
			queueDesiredSet(desiredSets, nil, pair.DestinationV6, "inet6", destV6)
		}
	}
	return m.applyDesiredSets(desiredSets)
}

func (m *Manager) applyDesiredSets(desiredSets map[string]desiredSetDefinition) error {
	if len(desiredSets) == 0 {
		return nil
	}
	setNames := make([]string, 0, len(desiredSets))
	for name := range desiredSets {
		setNames = append(setNames, name)
	}
	sort.Strings(setNames)

	for _, setName := range setNames {
		def := desiredSets[setName]
		family := strings.ToLower(strings.TrimSpace(def.Family))
		switch family {
		case "inet", "inet6":
		default:
			return fmt.Errorf("invalid set family %q for %s", def.Family, setName)
		}
		entries := dedupeSortedStrings(def.Entries)
		if err := m.applySetAtomically(setName, family, entries); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) applySetAtomically(setName, family string, entries []string) error {
	if err := m.ipset.EnsureSet(setName, family); err != nil {
		return err
	}
	stagedSet := stagedSetName(setName)
	if err := m.ipset.EnsureSet(stagedSet, family); err != nil {
		return err
	}
	if err := m.ipset.FlushSet(stagedSet); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := m.ipset.AddIP(stagedSet, entry, defaultIPSetTimeoutSeconds); err != nil {
			return err
		}
	}
	if err := m.ipset.SwapSets(setName, stagedSet); err != nil {
		return err
	}
	if err := m.ipset.DestroySet(stagedSet); err != nil {
		return err
	}
	return nil
}

func queueDesiredSet(
	desiredSets map[string]desiredSetDefinition,
	activeSets map[string]struct{},
	setName string,
	family string,
	entries []string,
) {
	def := desiredSets[setName]
	if def.Family == "" {
		def.Family = family
	}
	def.Entries = append(def.Entries, entries...)
	desiredSets[setName] = def
	if activeSets != nil {
		activeSets[setName] = struct{}{}
	}
}

func splitCIDRsByFamily(entries []string) (v4 []string, v6 []string) {
	v4 = make([]string, 0, len(entries))
	v6 = make([]string, 0, len(entries))
	for _, entry := range entries {
		if isIPv6CIDR(entry) {
			v6 = append(v6, entry)
		} else {
			v4 = append(v4, entry)
		}
	}
	return v4, v6
}

func mergeResolvedDestinations(rule RoutingRule, resolved map[ResolverSelector]ResolverValues) []string {
	destEntries := make([]string, 0, len(rule.DestinationCIDRs))
	destEntries = append(destEntries, rule.DestinationCIDRs...)
	for _, asn := range rule.DestinationASNs {
		entry := resolved[ResolverSelector{Type: "asn", Key: asn}]
		destEntries = append(destEntries, entry.V4...)
		destEntries = append(destEntries, entry.V6...)
	}
	for _, domain := range rule.Domains {
		entry := resolved[ResolverSelector{Type: "domain", Key: domain}]
		destEntries = append(destEntries, entry.V4...)
		destEntries = append(destEntries, entry.V6...)
	}
	for _, wildcard := range rule.WildcardDomains {
		entry := resolved[ResolverSelector{Type: "wildcard", Key: wildcard}]
		destEntries = append(destEntries, entry.V4...)
		destEntries = append(destEntries, entry.V6...)
	}
	return destEntries
}

func ruleNeedsDestinationSet(rule RoutingRule) bool {
	return len(rule.DestinationCIDRs) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0
}

func stagedSetName(setName string) string {
	const suffix = "_n"
	candidate := setName + suffix
	if len(candidate) <= maxIPSetNameLen {
		return candidate
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(setName))
	hash := fmt.Sprintf("%08x", h.Sum32())
	maxBase := maxIPSetNameLen - len(suffix) - len(hash) - 1
	if maxBase < 1 {
		maxBase = 1
	}
	base := setName
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return base + "_" + hash + suffix
}
