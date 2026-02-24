package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/routing"
)

type groupUpsertPayload struct {
	Name      string              `json:"name"`
	EgressVPN string              `json:"egressVpn"`
	Domains   []string            `json:"domains,omitempty"`
	Rules     []ruleUpsertPayload `json:"rules,omitempty"`
}

type ruleUpsertPayload struct {
	Name                     string                  `json:"name"`
	SourceInterfaces         []string                `json:"sourceInterfaces,omitempty"`
	SourceCIDRs              []string                `json:"sourceCidrs,omitempty"`
	ExcludedSourceCIDRs      []string                `json:"excludedSourceCidrs,omitempty"`
	SourceMACs               []string                `json:"sourceMacs,omitempty"`
	DestinationCIDRs         []string                `json:"destinationCidrs,omitempty"`
	ExcludedDestinationCIDRs []string                `json:"excludedDestinationCidrs,omitempty"`
	DestinationPorts         []portUpsertPayload     `json:"destinationPorts,omitempty"`
	ExcludedDestinationPorts []portUpsertPayload     `json:"excludedDestinationPorts,omitempty"`
	DestinationASNs          []string                `json:"destinationAsns,omitempty"`
	ExcludedDestinationASNs  []string                `json:"excludedDestinationAsns,omitempty"`
	ExcludeMulticast         *bool                   `json:"excludeMulticast,omitempty"`
	Domains                  []string                `json:"domains,omitempty"`
	WildcardDomains          []string                `json:"wildcardDomains,omitempty"`
	RawSelectors             ruleRawSelectorsPayload `json:"rawSelectors,omitempty"`
}

type ruleRawSelectorsPayload struct {
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

type portUpsertPayload struct {
	Protocol string `json:"protocol"`
	Start    int    `json:"start"`
	End      int    `json:"end,omitempty"`
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	groups, err := s.routingManager.ListGroups(r.Context())
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	id, err := parseGroupID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	group, err := s.routingManager.GetGroup(r.Context(), id)
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	payload, err := decodeGroupPayload(r)
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	created, err := s.routingManager.CreateGroup(r.Context(), payload)
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"group": created})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	id, err := parseGroupID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	payload, err := decodeGroupPayload(r)
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	updated, err := s.routingManager.UpdateGroup(r.Context(), id, payload)
	if err != nil {
		writeRoutingError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]any{"group": updated})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if s.routingManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "routing manager unavailable"})
		return
	}
	id, err := parseGroupID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.routingManager.DeleteGroup(r.Context(), id); err != nil {
		writeRoutingError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func decodeGroupPayload(r *http.Request) (routing.DomainGroup, error) {
	var payload groupUpsertPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return routing.DomainGroup{}, fmt.Errorf("%w: invalid JSON body", routing.ErrGroupValidation)
	}
	rules := make([]routing.RoutingRule, 0, len(payload.Rules))
	for _, rule := range payload.Rules {
		ports := make([]routing.PortRange, 0, len(rule.DestinationPorts))
		for _, port := range rule.DestinationPorts {
			ports = append(ports, routing.PortRange{
				Protocol: port.Protocol,
				Start:    port.Start,
				End:      port.End,
			})
		}
		excludedPorts := make([]routing.PortRange, 0, len(rule.ExcludedDestinationPorts))
		for _, port := range rule.ExcludedDestinationPorts {
			excludedPorts = append(excludedPorts, routing.PortRange{
				Protocol: port.Protocol,
				Start:    port.Start,
				End:      port.End,
			})
		}
		rules = append(rules, routing.RoutingRule{
			Name:                     rule.Name,
			SourceInterfaces:         append([]string(nil), rule.SourceInterfaces...),
			SourceCIDRs:              append([]string(nil), rule.SourceCIDRs...),
			ExcludedSourceCIDRs:      append([]string(nil), rule.ExcludedSourceCIDRs...),
			SourceMACs:               append([]string(nil), rule.SourceMACs...),
			DestinationCIDRs:         append([]string(nil), rule.DestinationCIDRs...),
			ExcludedDestinationCIDRs: append([]string(nil), rule.ExcludedDestinationCIDRs...),
			DestinationPorts:         ports,
			ExcludedDestinationPorts: excludedPorts,
			DestinationASNs:          append([]string(nil), rule.DestinationASNs...),
			ExcludedDestinationASNs:  append([]string(nil), rule.ExcludedDestinationASNs...),
			ExcludeMulticast:         rule.ExcludeMulticast,
			Domains:                  append([]string(nil), rule.Domains...),
			WildcardDomains:          append([]string(nil), rule.WildcardDomains...),
			RawSelectors: &routing.RuleRawSelectors{
				SourceInterfaces:         append([]string(nil), rule.RawSelectors.SourceInterfaces...),
				SourceCIDRs:              append([]string(nil), rule.RawSelectors.SourceCIDRs...),
				ExcludedSourceCIDRs:      append([]string(nil), rule.RawSelectors.ExcludedSourceCIDRs...),
				SourceMACs:               append([]string(nil), rule.RawSelectors.SourceMACs...),
				DestinationCIDRs:         append([]string(nil), rule.RawSelectors.DestinationCIDRs...),
				ExcludedDestinationCIDRs: append([]string(nil), rule.RawSelectors.ExcludedDestinationCIDRs...),
				DestinationPorts:         append([]string(nil), rule.RawSelectors.DestinationPorts...),
				ExcludedDestinationPorts: append([]string(nil), rule.RawSelectors.ExcludedDestinationPorts...),
				DestinationASNs:          append([]string(nil), rule.RawSelectors.DestinationASNs...),
				ExcludedDestinationASNs:  append([]string(nil), rule.RawSelectors.ExcludedDestinationASNs...),
				Domains:                  append([]string(nil), rule.RawSelectors.Domains...),
				WildcardDomains:          append([]string(nil), rule.RawSelectors.WildcardDomains...),
			},
		})
	}
	return routing.NormalizeAndValidate(routing.DomainGroup{
		Name:      payload.Name,
		EgressVPN: payload.EgressVPN,
		Domains:   payload.Domains,
		Rules:     rules,
	})
}

func parseGroupID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid group id")
	}
	return id, nil
}

func writeRoutingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, routing.ErrGroupValidation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, routing.ErrGroupNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "unique"):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
