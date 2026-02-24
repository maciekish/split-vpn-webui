package server

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"split-vpn-webui/internal/routing"
)

const routingInspectorIPSetTimeout = 8 * time.Second

type routingInspectorResponse struct {
	VPNName       string                  `json:"vpnName"`
	RoutingV4Size int                     `json:"routingV4Size"`
	RoutingV6Size int                     `json:"routingV6Size"`
	Groups        []routingInspectorGroup `json:"groups"`
	GeneratedAt   time.Time               `json:"generatedAt"`
}

type routingInspectorGroup struct {
	ID    int64                  `json:"id"`
	Name  string                 `json:"name"`
	Rules []routingInspectorRule `json:"rules"`
}

type routingInspectorRule struct {
	RuleID           int64                       `json:"ruleId,omitempty"`
	RuleIndex        int                         `json:"ruleIndex"`
	RuleName         string                      `json:"ruleName"`
	SourceInterfaces []string                    `json:"sourceInterfaces,omitempty"`
	SourceMACs       []routingInspectorMAC       `json:"sourceMacs,omitempty"`
	DestinationPorts []routing.PortRange         `json:"destinationPorts,omitempty"`
	DestinationASNs  []string                    `json:"destinationAsns,omitempty"`
	Domains          []string                    `json:"domains,omitempty"`
	WildcardDomains  []string                    `json:"wildcardDomains,omitempty"`
	SourceSetV4      routingInspectorSetSnapshot `json:"sourceSetV4,omitempty"`
	SourceSetV6      routingInspectorSetSnapshot `json:"sourceSetV6,omitempty"`
	DestinationSetV4 routingInspectorSetSnapshot `json:"destinationSetV4,omitempty"`
	DestinationSetV6 routingInspectorSetSnapshot `json:"destinationSetV6,omitempty"`
}

type routingInspectorMAC struct {
	MAC        string   `json:"mac"`
	DeviceName string   `json:"deviceName,omitempty"`
	IPHints    []string `json:"ipHints,omitempty"`
}

type routingInspectorSetSnapshot struct {
	Name       string                     `json:"name"`
	EntryCount int                        `json:"entryCount"`
	Entries    []routingInspectorSetEntry `json:"entries,omitempty"`
}

type routingInspectorSetEntry struct {
	Value      string   `json:"value"`
	Canonical  string   `json:"canonical,omitempty"`
	DeviceName string   `json:"deviceName,omitempty"`
	Provenance []string `json:"provenance,omitempty"`
}

func (s *Server) handleVPNRoutingInspector(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	vpnName, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	inspector, err := s.buildVPNRoutingInspector(r, vpnName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"inspector": inspector})
}

func (s *Server) buildVPNRoutingInspector(r *http.Request, vpnName string) (*routingInspectorResponse, error) {
	ctx := r.Context()
	groups, err := s.routingManager.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	resolved, err := s.routingManager.LoadResolverSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	prewarmed, err := s.routingManager.LoadPrewarmSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	setSnapshots, err := readIPSetSnapshots(routingInspectorIPSetTimeout)
	if err != nil {
		return nil, err
	}
	devices := loadDeviceDirectory(ctx)

	response := &routingInspectorResponse{
		VPNName:     vpnName,
		GeneratedAt: time.Now().UTC(),
		Groups:      make([]routingInspectorGroup, 0),
	}
	for _, group := range groups {
		if strings.TrimSpace(group.EgressVPN) != vpnName {
			continue
		}
		groupView := routingInspectorGroup{
			ID:    group.ID,
			Name:  group.Name,
			Rules: make([]routingInspectorRule, 0, len(group.Rules)),
		}
		for ruleIndex, rule := range group.Rules {
			pair := routing.RuleSetNames(group.Name, ruleIndex)
			ruleView := routingInspectorRule{
				RuleID:           rule.ID,
				RuleIndex:        ruleIndex + 1,
				RuleName:         rule.Name,
				SourceInterfaces: append([]string(nil), rule.SourceInterfaces...),
				SourceMACs:       mapRuleSourceMACs(rule.SourceMACs, devices),
				DestinationPorts: append([]routing.PortRange(nil), rule.DestinationPorts...),
				DestinationASNs:  append([]string(nil), rule.DestinationASNs...),
				Domains:          append([]string(nil), rule.Domains...),
				WildcardDomains:  append([]string(nil), rule.WildcardDomains...),
			}
			if ruleNeedsSourceSet(rule) {
				sourceProvenance := sourceSetProvenance(rule)
				sourceEntriesV4, sourceEntriesV6 := splitRawMembersByFamily(rule.SourceCIDRs)
				ruleView.SourceSetV4 = buildRoutingInspectorSet(
					pair.SourceV4,
					"inet",
					setSnapshots[pair.SourceV4],
					sourceEntriesV4,
					sourceProvenance,
					devices,
					true,
				)
				ruleView.SourceSetV6 = buildRoutingInspectorSet(
					pair.SourceV6,
					"inet6",
					setSnapshots[pair.SourceV6],
					sourceEntriesV6,
					sourceProvenance,
					devices,
					true,
				)
				response.RoutingV4Size += ruleView.SourceSetV4.EntryCount
				response.RoutingV6Size += ruleView.SourceSetV6.EntryCount
			}
			if ruleNeedsDestinationSet(rule) {
				destV4Provenance := destinationSetProvenance(rule, pair.DestinationV4, "inet", resolved, prewarmed)
				destV6Provenance := destinationSetProvenance(rule, pair.DestinationV6, "inet6", resolved, prewarmed)
				destEntries := destinationRawMembers(rule, pair, resolved, prewarmed)
				destEntriesV4, destEntriesV6 := splitRawMembersByFamily(destEntries)
				ruleView.DestinationSetV4 = buildRoutingInspectorSet(
					pair.DestinationV4,
					"inet",
					setSnapshots[pair.DestinationV4],
					destEntriesV4,
					destV4Provenance,
					devices,
					false,
				)
				ruleView.DestinationSetV6 = buildRoutingInspectorSet(
					pair.DestinationV6,
					"inet6",
					setSnapshots[pair.DestinationV6],
					destEntriesV6,
					destV6Provenance,
					devices,
					false,
				)
				response.RoutingV4Size += ruleView.DestinationSetV4.EntryCount
				response.RoutingV6Size += ruleView.DestinationSetV6.EntryCount
			}
			groupView.Rules = append(groupView.Rules, ruleView)
		}
		response.Groups = append(response.Groups, groupView)
	}
	return response, nil
}

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
