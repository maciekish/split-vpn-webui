package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/routing"
)

type groupUpsertPayload struct {
	Name      string   `json:"name"`
	EgressVPN string   `json:"egressVpn"`
	Domains   []string `json:"domains"`
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		return routing.DomainGroup{}, errors.New("invalid JSON body")
	}
	return routing.DomainGroup{
		Name:      payload.Name,
		EgressVPN: payload.EgressVPN,
		Domains:   payload.Domains,
	}, nil
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
