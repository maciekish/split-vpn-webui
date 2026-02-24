package server

import (
	"net"
	"sort"
	"strings"

	"split-vpn-webui/internal/routing"
)

func mapRuleSourceMACs(macs []string, devices deviceDirectory) []routingInspectorMAC {
	if len(macs) == 0 {
		return nil
	}
	out := make([]routingInspectorMAC, 0, len(macs))
	for _, mac := range macs {
		name, hints := devices.lookupMAC(mac)
		out = append(out, routingInspectorMAC{
			MAC:        mac,
			DeviceName: name,
			IPHints:    hints,
		})
	}
	return out
}

func sourceSetProvenance(rule routing.RoutingRule) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, source := range rule.SourceCIDRs {
		addSetProvenance(result, "any", source, "source CIDR: "+source)
	}
	return result
}

func sourceExcludeSetProvenance(rule routing.RoutingRule) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, source := range rule.ExcludedSourceCIDRs {
		addSetProvenance(result, "any", source, "excluded source CIDR: "+source)
	}
	return result
}

func destinationSetProvenance(
	rule routing.RoutingRule,
	setName string,
	family string,
	resolved map[routing.ResolverSelector]routing.ResolverValues,
	prewarmed map[string]routing.ResolverValues,
) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, cidr := range rule.DestinationCIDRs {
		addSetProvenance(result, family, cidr, "destination CIDR: "+cidr)
	}
	for _, asn := range rule.DestinationASNs {
		key := normalizeASNSelector(asn)
		values := resolved[routing.ResolverSelector{Type: "asn", Key: key}]
		addResolverProvenance(result, family, values, "ASN "+key+" (resolver)")
	}
	for _, domain := range rule.Domains {
		values := resolved[routing.ResolverSelector{Type: "domain", Key: domain}]
		addResolverProvenance(result, family, values, "domain "+domain+" (resolver)")
	}
	for _, wildcard := range rule.WildcardDomains {
		values := resolved[routing.ResolverSelector{Type: "wildcard", Key: wildcard}]
		addResolverProvenance(result, family, values, "wildcard "+wildcard+" (resolver)")
	}
	prewarmLabel := "pre-warm cache"
	if len(rule.Domains) > 0 || len(rule.WildcardDomains) > 0 {
		prewarmLabel = "pre-warm cache (rule domains)"
	}
	addResolverProvenance(result, family, prewarmed[setName], prewarmLabel)
	return result
}

func destinationExcludeSetProvenance(
	rule routing.RoutingRule,
	family string,
	resolved map[routing.ResolverSelector]routing.ResolverValues,
) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, cidr := range rule.ExcludedDestinationCIDRs {
		addSetProvenance(result, family, cidr, "excluded destination CIDR: "+cidr)
	}
	for _, asn := range rule.ExcludedDestinationASNs {
		key := normalizeASNSelector(asn)
		values := resolved[routing.ResolverSelector{Type: "asn", Key: key}]
		addResolverProvenance(result, family, values, "excluded ASN "+key+" (resolver)")
	}
	return result
}

func addResolverProvenance(
	target map[string]map[string]struct{},
	family string,
	values routing.ResolverValues,
	label string,
) {
	if strings.EqualFold(family, "inet6") {
		for _, cidr := range values.V6 {
			addSetProvenance(target, family, cidr, label)
		}
		return
	}
	for _, cidr := range values.V4 {
		addSetProvenance(target, family, cidr, label)
	}
}

func addSetProvenance(
	target map[string]map[string]struct{},
	family string,
	value string,
	label string,
) {
	canonical := canonicalizeSetValue(value, family)
	if canonical == "" {
		return
	}
	bucket := target[canonical]
	if bucket == nil {
		bucket = make(map[string]struct{})
		target[canonical] = bucket
	}
	bucket[label] = struct{}{}
}

func buildRoutingInspectorSet(
	name string,
	family string,
	snapshot ipsetSnapshot,
	rawMembers []string,
	provenance map[string]map[string]struct{},
	devices deviceDirectory,
	includeDevice bool,
) routingInspectorSetSnapshot {
	members := rawMembers
	if len(members) == 0 {
		members = snapshot.Members
	}
	out := routingInspectorSetSnapshot{
		Name:       name,
		EntryCount: snapshot.Count,
		Entries:    make([]routingInspectorSetEntry, 0, len(members)),
	}
	for _, value := range members {
		canonical := canonicalizeSetValue(value, family)
		entry := routingInspectorSetEntry{
			Value:     value,
			Canonical: canonical,
		}
		if includeDevice {
			entry.DeviceName = devices.lookupIP(value)
		}
		labels := sortedSetKeys(provenance[canonical])
		if len(labels) == 0 {
			labels = []string{"runtime set member"}
		}
		entry.Provenance = labels
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		left := out.Entries[i].Canonical
		right := out.Entries[j].Canonical
		if left == right {
			return out.Entries[i].Value < out.Entries[j].Value
		}
		return left < right
	})
	return out
}

func destinationRawMembers(
	rule routing.RoutingRule,
	pair routing.RuleSetPair,
	resolved map[routing.ResolverSelector]routing.ResolverValues,
	prewarmed map[string]routing.ResolverValues,
) []string {
	entries := make([]string, 0, len(rule.DestinationCIDRs))
	entries = append(entries, rule.DestinationCIDRs...)
	for _, asn := range rule.DestinationASNs {
		key := normalizeASNSelector(asn)
		values := resolved[routing.ResolverSelector{Type: "asn", Key: key}]
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	for _, domain := range rule.Domains {
		values := resolved[routing.ResolverSelector{Type: "domain", Key: domain}]
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	for _, wildcard := range rule.WildcardDomains {
		values := resolved[routing.ResolverSelector{Type: "wildcard", Key: wildcard}]
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	if values, ok := prewarmed[pair.DestinationV4]; ok {
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	if values, ok := prewarmed[pair.DestinationV6]; ok {
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	return dedupeAndSortMembers(entries)
}

func destinationExcludedRawMembers(
	rule routing.RoutingRule,
	resolved map[routing.ResolverSelector]routing.ResolverValues,
) []string {
	entries := make([]string, 0, len(rule.ExcludedDestinationCIDRs))
	entries = append(entries, rule.ExcludedDestinationCIDRs...)
	for _, asn := range rule.ExcludedDestinationASNs {
		key := normalizeASNSelector(asn)
		values := resolved[routing.ResolverSelector{Type: "asn", Key: key}]
		entries = append(entries, values.V4...)
		entries = append(entries, values.V6...)
	}
	return dedupeAndSortMembers(entries)
}

func splitRawMembersByFamily(entries []string) (v4 []string, v6 []string) {
	v4 = make([]string, 0, len(entries))
	v6 = make([]string, 0, len(entries))
	for _, value := range entries {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if canonicalizeSetValue(trimmed, "inet6") != "" {
			v6 = append(v6, trimmed)
			continue
		}
		if canonicalizeSetValue(trimmed, "inet") != "" {
			v4 = append(v4, trimmed)
		}
	}
	return dedupeAndSortMembers(v4), dedupeAndSortMembers(v6)
}

func dedupeAndSortMembers(entries []string) []string {
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func canonicalizeSetValue(value string, family string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if ip, network, err := net.ParseCIDR(trimmed); err == nil {
		normalized := ip
		if ip4 := ip.To4(); ip4 != nil {
			normalized = ip4
		}
		network.IP = normalized
		return network.String()
	}
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return ""
	}
	if strings.EqualFold(family, "inet6") {
		if ip4 := ip.To4(); ip4 != nil {
			return ""
		}
		mask := net.CIDRMask(128, 128)
		return (&net.IPNet{IP: ip, Mask: mask}).String()
	}
	if ip4 := ip.To4(); ip4 != nil {
		mask := net.CIDRMask(32, 32)
		return (&net.IPNet{IP: ip4, Mask: mask}).String()
	}
	return ""
}

func normalizeASNSelector(value string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	trimmed = strings.TrimPrefix(trimmed, "AS")
	if trimmed == "" {
		return ""
	}
	for _, char := range trimmed {
		if char < '0' || char > '9' {
			return ""
		}
	}
	trimmed = strings.TrimLeft(trimmed, "0")
	if trimmed == "" {
		return ""
	}
	return "AS" + trimmed
}

func sortedSetKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
