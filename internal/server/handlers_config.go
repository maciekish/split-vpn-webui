package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
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
	name := chi.URLParam(r, "name")
	content, err := s.configManager.ReadConfigFile(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) handleWriteConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
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
	name := chi.URLParam(r, "name")
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	go s.startVPN(cfg)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "starting"})
}

func (s *Server) handleStopVPN(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	go s.stopVPN(cfg)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := s.configManager.SetAutostart(name, payload.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
