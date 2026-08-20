package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/remotelist"
	"split-vpn-webui/internal/routing"
)

func (s *Server) handleListRemoteLists(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	lists, err := s.remoteLists.List(r.Context())
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lists": lists,
		"kinds": routing.RemoteListKinds(),
	})
}

func (s *Server) handleGetRemoteListEntries(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	id, err := parseRemoteListID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	list, err := s.remoteLists.Get(r.Context(), id)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	entries, err := s.remoteLists.Entries(r.Context(), id)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list, "entries": entries})
}

func (s *Server) handleCreateRemoteList(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	payload, err := decodeRemoteListPayload(r)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	created, result, err := s.remoteLists.Create(r.Context(), payload)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"list": created, "refresh": result})
}

func (s *Server) handleUpdateRemoteList(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	id, err := parseRemoteListID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	payload, err := decodeRemoteListPayload(r)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	existing, err := s.remoteLists.Get(r.Context(), id)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	if !strings.EqualFold(existing.Name, strings.TrimSpace(payload.Name)) {
		// Routing rules reference a list by name, so a rename would silently
		// detach them.
		if err := s.assertRemoteListUnreferenced(r.Context(), existing.Name, "renamed"); err != nil {
			writeRemoteListError(w, err)
			return
		}
	}
	updated, result, err := s.remoteLists.Update(r.Context(), id, payload)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]any{"list": updated, "refresh": result})
}

func (s *Server) handleDeleteRemoteList(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	id, err := parseRemoteListID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	existing, err := s.remoteLists.Get(r.Context(), id)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	if err := s.assertRemoteListUnreferenced(r.Context(), existing.Name, "deleted"); err != nil {
		writeRemoteListError(w, err)
		return
	}
	if err := s.remoteLists.Delete(r.Context(), id); err != nil {
		writeRemoteListError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleRefreshRemoteList(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	id, err := parseRemoteListID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := s.remoteLists.Refresh(r.Context(), id)
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]any{"refresh": result})
}

func (s *Server) handleRefreshAllRemoteLists(w http.ResponseWriter, r *http.Request) {
	if s.remoteLists == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remote lists unavailable"})
		return
	}
	results, err := s.remoteLists.RefreshAll(r.Context())
	if err != nil {
		writeRemoteListError(w, err)
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]any{"refresh": results})
}

func (s *Server) assertRemoteListUnreferenced(ctx context.Context, name string, action string) error {
	if s.routingManager == nil {
		return nil
	}
	groups, err := s.routingManager.RemoteListReferences(ctx, name)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: remote list %q cannot be %s while used by policy group(s): %s",
		remotelist.ErrReferenced,
		name,
		action,
		strings.Join(groups, ", "),
	)
}

func decodeRemoteListPayload(r *http.Request) (remotelist.UpsertRequest, error) {
	var payload remotelist.UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return remotelist.UpsertRequest{}, fmt.Errorf("%w: invalid JSON body", remotelist.ErrValidation)
	}
	return payload, nil
}

func parseRemoteListID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid remote list id")
	}
	return id, nil
}

func writeRemoteListError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, remotelist.ErrValidation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, remotelist.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, remotelist.ErrReferenced):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
