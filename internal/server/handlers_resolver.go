package server

import (
	"errors"
	"net/http"

	"split-vpn-webui/internal/routing"
)

func (s *Server) handleResolverRun(w http.ResponseWriter, r *http.Request) {
	if s.resolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "resolver scheduler unavailable"})
		return
	}
	if err := s.resolver.TriggerNow(); err != nil {
		switch {
		case errors.Is(err, routing.ErrResolverRunInProgress):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handleResolverStatus(w http.ResponseWriter, r *http.Request) {
	if s.resolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "resolver scheduler unavailable"})
		return
	}
	status, err := s.resolver.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}
