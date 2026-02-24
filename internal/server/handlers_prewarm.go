package server

import (
	"errors"
	"net/http"

	"split-vpn-webui/internal/prewarm"
)

func (s *Server) handlePrewarmRun(w http.ResponseWriter, r *http.Request) {
	if s.prewarm == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "prewarm scheduler unavailable"})
		return
	}
	if err := s.prewarm.TriggerNow(); err != nil {
		switch {
		case errors.Is(err, prewarm.ErrRunInProgress):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handlePrewarmStatus(w http.ResponseWriter, r *http.Request) {
	if s.prewarm == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "prewarm scheduler unavailable"})
		return
	}
	status, err := s.prewarm.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handlePrewarmClearRun(w http.ResponseWriter, r *http.Request) {
	if s.prewarm == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "prewarm scheduler unavailable"})
		return
	}
	if err := s.prewarm.ClearCacheAndRun(); err != nil {
		switch {
		case errors.Is(err, prewarm.ErrRunInProgress):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handlePrewarmStop(w http.ResponseWriter, r *http.Request) {
	if s.prewarm == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "prewarm scheduler unavailable"})
		return
	}
	if err := s.prewarm.CancelRun(); err != nil {
		switch {
		case errors.Is(err, prewarm.ErrRunNotActive):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}
