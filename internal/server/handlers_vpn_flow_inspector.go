package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleStartVPNFlowInspector(w http.ResponseWriter, r *http.Request) {
	if s.flowInspector == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "flow inspector unavailable"})
		return
	}
	vpnName, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	if _, err := s.configManager.Get(vpnName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	samples, interfaceName, err := s.collectVPNFlowSamples(r.Context(), vpnName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sessionID, err := s.flowInspector.startSession(vpnName, interfaceName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := s.flowInspector.updateAndSnapshot(vpnName, sessionID, samples)
	if err != nil {
		writeFlowInspectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":            sessionID,
		"pollIntervalSeconds":  flowInspectorPollIntervalSeconds,
		"idleRetentionSeconds": int(flowInspectorIdleRetention.Seconds()),
		"snapshot":             snapshot,
	})
}

func (s *Server) handlePollVPNFlowInspector(w http.ResponseWriter, r *http.Request) {
	if s.flowInspector == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "flow inspector unavailable"})
		return
	}
	vpnName, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	if _, err := s.configManager.Get(vpnName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing flow inspector session id"})
		return
	}
	samples, _, err := s.collectVPNFlowSamples(r.Context(), vpnName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := s.flowInspector.updateAndSnapshot(vpnName, sessionID, samples)
	if err != nil {
		writeFlowInspectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": snapshot})
}

func (s *Server) handleStopVPNFlowInspector(w http.ResponseWriter, r *http.Request) {
	if s.flowInspector == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "flow inspector unavailable"})
		return
	}
	vpnName, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	if _, err := s.configManager.Get(vpnName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing flow inspector session id"})
		return
	}
	if err := s.flowInspector.stopSession(vpnName, sessionID); err != nil {
		writeFlowInspectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func writeFlowInspectorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errFlowInspectorSessionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errFlowInspectorVPNMismatch):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
