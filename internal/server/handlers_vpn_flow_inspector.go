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
	if s.diagLog != nil {
		s.diagLog.Debugf("flow_inspector start request vpn=%s remote=%s", vpnName, r.RemoteAddr)
	}
	if _, err := s.configManager.Get(vpnName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	samples, interfaceName, err := s.collectVPNFlowSamples(r.Context(), vpnName)
	if err != nil {
		if s.diagLog != nil {
			s.diagLog.Errorf("flow_inspector start collection failed vpn=%s err=%v", vpnName, err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sessionID, err := s.flowInspector.startSession(vpnName, interfaceName)
	if err != nil {
		if s.diagLog != nil {
			s.diagLog.Errorf("flow_inspector start session create failed vpn=%s err=%v", vpnName, err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := s.flowInspector.updateAndSnapshot(vpnName, sessionID, samples)
	if err != nil {
		if s.diagLog != nil {
			s.diagLog.Errorf("flow_inspector start initial snapshot failed vpn=%s session=%s err=%v", vpnName, sessionID, err)
		}
		writeFlowInspectorError(w, err)
		return
	}
	if s.diagLog != nil {
		s.diagLog.Infof(
			"flow_inspector session started vpn=%s session=%s iface=%s samples=%d flows=%d",
			vpnName,
			sessionID,
			interfaceName,
			len(samples),
			snapshot.FlowCount,
		)
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
	if s.diagLog != nil {
		s.diagLog.Debugf("flow_inspector poll request vpn=%s session=%s", vpnName, sessionID)
	}
	samples, _, err := s.collectVPNFlowSamples(r.Context(), vpnName)
	if err != nil {
		if s.diagLog != nil {
			s.diagLog.Errorf("flow_inspector poll collection failed vpn=%s session=%s err=%v", vpnName, sessionID, err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := s.flowInspector.updateAndSnapshot(vpnName, sessionID, samples)
	if err != nil {
		if s.diagLog != nil {
			s.diagLog.Warnf("flow_inspector poll failed vpn=%s session=%s err=%v", vpnName, sessionID, err)
		}
		writeFlowInspectorError(w, err)
		return
	}
	if s.diagLog != nil {
		s.diagLog.Debugf(
			"flow_inspector poll ok vpn=%s session=%s samples=%d flows=%d totals=%d",
			vpnName,
			sessionID,
			len(samples),
			snapshot.FlowCount,
			snapshot.Totals.TotalBytes,
		)
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
		if s.diagLog != nil {
			s.diagLog.Warnf("flow_inspector stop failed vpn=%s session=%s err=%v", vpnName, sessionID, err)
		}
		writeFlowInspectorError(w, err)
		return
	}
	if s.diagLog != nil {
		s.diagLog.Infof("flow_inspector session stopped vpn=%s session=%s", vpnName, sessionID)
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
