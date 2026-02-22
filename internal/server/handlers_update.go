package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	updateRequestTimeout = 3 * time.Minute
)

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater unavailable"})
		return
	}
	status, err := s.updater.Status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater unavailable"})
		return
	}
	requestedTag, err := decodeUpdateTag(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), updateRequestTimeout)
	defer cancel()
	status, err := s.updater.Check(ctx, requestedTag)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastEvent("update", status)
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater unavailable"})
		return
	}
	requestedTag, err := decodeUpdateTag(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), updateRequestTimeout)
	defer cancel()
	status, err := s.updater.StartUpdate(ctx, requestedTag)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		} else if strings.Contains(strings.ToLower(err.Error()), "already in progress") {
			code = http.StatusConflict
		} else if strings.Contains(strings.ToLower(err.Error()), "missing checksum") || strings.Contains(strings.ToLower(err.Error()), "checksum") {
			code = http.StatusBadGateway
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastEvent("update", status)
	writeJSON(w, http.StatusAccepted, status)
}

func decodeUpdateTag(r *http.Request) (string, error) {
	var payload struct {
		Version string `json:"version"`
	}
	if r.Body == nil {
		return "", nil
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", errors.New("invalid JSON body")
	}
	if strings.TrimSpace(payload.Version) == "" {
		return "", nil
	}
	return payload.Version, nil
}
