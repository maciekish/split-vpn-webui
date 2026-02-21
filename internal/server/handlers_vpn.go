package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"split-vpn-webui/internal/vpn"
)

func (s *Server) handleListVPNs(w http.ResponseWriter, r *http.Request) {
	if s.vpnManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "vpn manager unavailable"})
		return
	}
	profiles, err := s.vpnManager.List()
	if err != nil {
		writeVPNError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vpns": profiles})
}

func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if s.vpnManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "vpn manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	profile, err := s.vpnManager.Get(name)
	if err != nil {
		writeVPNError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vpn": profile})
}

func (s *Server) handleCreateVPN(w http.ResponseWriter, r *http.Request) {
	if s.vpnManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "vpn manager unavailable"})
		return
	}
	var payload vpn.UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	profile, err := s.vpnManager.Create(payload)
	if err != nil {
		writeVPNError(w, err)
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"vpn": profile})
}

func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	if s.vpnManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "vpn manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	var payload vpn.UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	profile, err := s.vpnManager.Update(name, payload)
	if err != nil {
		writeVPNError(w, err)
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]any{"vpn": profile})
}

func (s *Server) handleDeleteVPN(w http.ResponseWriter, r *http.Request) {
	if s.vpnManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "vpn manager unavailable"})
		return
	}
	name, ok := s.requireVPNNameParam(w, r)
	if !ok {
		return
	}
	if err := s.vpnManager.Delete(name); err != nil {
		writeVPNError(w, err)
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func writeVPNError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, vpn.ErrVPNValidation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, vpn.ErrVPNNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, vpn.ErrVPNAlreadyExists), errors.Is(err, vpn.ErrAllocationConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, vpn.ErrAllocationExhausted):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
