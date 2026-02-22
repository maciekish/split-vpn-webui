package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs, statuses, errMap := s.collectConfigStatuses()
	writeJSON(w, http.StatusOK, map[string]any{
		"configs":  configs,
		"statuses": statuses,
		"errors":   errMap,
	})
}

func (s *Server) handleReadConfig(w http.ResponseWriter, r *http.Request) {
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	content, err := s.configManager.ReadConfigFile(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) handleWriteConfig(w http.ResponseWriter, r *http.Request) {
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if payload.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content must not be empty"})
		return
	}
	if err := s.configManager.WriteConfigFile(name, payload.Content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStartVPN(w http.ResponseWriter, r *http.Request) {
	if s.systemd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "systemd manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.systemd.Start(vpnServiceUnitName(cfg.Name)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleStopVPN(w http.ResponseWriter, r *http.Request) {
	if s.systemd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "systemd manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.systemd.Stop(vpnServiceUnitName(cfg.Name)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	if s.systemd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "systemd manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if _, err := s.configManager.Get(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.configManager.SetAutostart(name, payload.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRestartVPN(w http.ResponseWriter, r *http.Request) {
	if s.systemd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "systemd manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	if _, err := s.configManager.Get(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.systemd.Restart(vpnServiceUnitName(name)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	payload := s.createPayload(nil)
	writeJSON(w, http.StatusOK, payload)
}
