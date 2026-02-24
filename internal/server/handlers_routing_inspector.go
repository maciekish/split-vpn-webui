package server

import (
	"net/http"
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
	RuleID                   int64                       `json:"ruleId,omitempty"`
	RuleIndex                int                         `json:"ruleIndex"`
	RuleName                 string                      `json:"ruleName"`
	SourceInterfaces         []string                    `json:"sourceInterfaces,omitempty"`
	ExcludedSourceCIDRs      []string                    `json:"excludedSourceCidrs,omitempty"`
	SourceMACs               []routingInspectorMAC       `json:"sourceMacs,omitempty"`
	DestinationPorts         []routing.PortRange         `json:"destinationPorts,omitempty"`
	ExcludedDestinationPorts []routing.PortRange         `json:"excludedDestinationPorts,omitempty"`
	DestinationASNs          []string                    `json:"destinationAsns,omitempty"`
	ExcludedDestinationASNs  []string                    `json:"excludedDestinationAsns,omitempty"`
	ExcludedDestinationCIDRs []string                    `json:"excludedDestinationCidrs,omitempty"`
	ExcludeMulticast         bool                        `json:"excludeMulticast"`
	Domains                  []string                    `json:"domains,omitempty"`
	WildcardDomains          []string                    `json:"wildcardDomains,omitempty"`
	SourceSetV4              routingInspectorSetSnapshot `json:"sourceSetV4,omitempty"`
	SourceSetV6              routingInspectorSetSnapshot `json:"sourceSetV6,omitempty"`
	ExcludedSourceSetV4      routingInspectorSetSnapshot `json:"excludedSourceSetV4,omitempty"`
	ExcludedSourceSetV6      routingInspectorSetSnapshot `json:"excludedSourceSetV6,omitempty"`
	DestinationSetV4         routingInspectorSetSnapshot `json:"destinationSetV4,omitempty"`
	DestinationSetV6         routingInspectorSetSnapshot `json:"destinationSetV6,omitempty"`
	ExcludedDestinationSetV4 routingInspectorSetSnapshot `json:"excludedDestinationSetV4,omitempty"`
	ExcludedDestinationSetV6 routingInspectorSetSnapshot `json:"excludedDestinationSetV6,omitempty"`
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
				RuleID:                   rule.ID,
				RuleIndex:                ruleIndex + 1,
				RuleName:                 rule.Name,
				SourceInterfaces:         append([]string(nil), rule.SourceInterfaces...),
				ExcludedSourceCIDRs:      append([]string(nil), rule.ExcludedSourceCIDRs...),
				SourceMACs:               mapRuleSourceMACs(rule.SourceMACs, devices),
				DestinationPorts:         append([]routing.PortRange(nil), rule.DestinationPorts...),
				ExcludedDestinationPorts: append([]routing.PortRange(nil), rule.ExcludedDestinationPorts...),
				DestinationASNs:          append([]string(nil), rule.DestinationASNs...),
				ExcludedDestinationASNs:  append([]string(nil), rule.ExcludedDestinationASNs...),
				ExcludedDestinationCIDRs: append([]string(nil), rule.ExcludedDestinationCIDRs...),
				ExcludeMulticast:         routing.RuleExcludeMulticastEnabled(rule),
				Domains:                  append([]string(nil), rule.Domains...),
				WildcardDomains:          append([]string(nil), rule.WildcardDomains...),
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
			if ruleNeedsExcludedSourceSet(rule) {
				sourceProvenance := sourceExcludeSetProvenance(rule)
				sourceEntriesV4, sourceEntriesV6 := splitRawMembersByFamily(rule.ExcludedSourceCIDRs)
				ruleView.ExcludedSourceSetV4 = buildRoutingInspectorSet(
					pair.ExcludedSourceV4,
					"inet",
					setSnapshots[pair.ExcludedSourceV4],
					sourceEntriesV4,
					sourceProvenance,
					devices,
					true,
				)
				ruleView.ExcludedSourceSetV6 = buildRoutingInspectorSet(
					pair.ExcludedSourceV6,
					"inet6",
					setSnapshots[pair.ExcludedSourceV6],
					sourceEntriesV6,
					sourceProvenance,
					devices,
					true,
				)
				response.RoutingV4Size += ruleView.ExcludedSourceSetV4.EntryCount
				response.RoutingV6Size += ruleView.ExcludedSourceSetV6.EntryCount
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
			if ruleNeedsExcludedDestinationSet(rule) {
				destV4Provenance := destinationExcludeSetProvenance(rule, "inet", resolved)
				destV6Provenance := destinationExcludeSetProvenance(rule, "inet6", resolved)
				destEntries := destinationExcludedRawMembers(rule, resolved)
				destEntriesV4, destEntriesV6 := splitRawMembersByFamily(destEntries)
				ruleView.ExcludedDestinationSetV4 = buildRoutingInspectorSet(
					pair.ExcludedDestinationV4,
					"inet",
					setSnapshots[pair.ExcludedDestinationV4],
					destEntriesV4,
					destV4Provenance,
					devices,
					false,
				)
				ruleView.ExcludedDestinationSetV6 = buildRoutingInspectorSet(
					pair.ExcludedDestinationV6,
					"inet6",
					setSnapshots[pair.ExcludedDestinationV6],
					destEntriesV6,
					destV6Provenance,
					devices,
					false,
				)
				response.RoutingV4Size += ruleView.ExcludedDestinationSetV4.EntryCount
				response.RoutingV6Size += ruleView.ExcludedDestinationSetV6.EntryCount
			}
			groupView.Rules = append(groupView.Rules, ruleView)
		}
		response.Groups = append(response.Groups, groupView)
	}
	return response, nil
}
