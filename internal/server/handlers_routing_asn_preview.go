package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/settings"
)

const (
	defaultASNPreviewTimeoutSeconds = 10
	maxASNPreviewTimeoutSeconds     = 60
	maxASNPreviewSelectors          = 64
)

var previewASNEntries = routing.PreviewASNEntries

type asnPreviewPayload struct {
	ASNs []string `json:"asns"`
}

func (s *Server) handleASNPreview(w http.ResponseWriter, r *http.Request) {
	var payload asnPreviewPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	asns := sanitizeASNPreviewInputs(payload.ASNs)
	if len(asns) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one ASN is required"})
		return
	}
	if len(asns) > maxASNPreviewSelectors {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many ASNs in preview request"})
		return
	}

	timeout := asnPreviewTimeout(s.settings)
	result, err := previewASNEntries(r.Context(), asns, timeout)
	if err != nil {
		if isASNPreviewValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func sanitizeASNPreviewInputs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func asnPreviewTimeout(manager *settings.Manager) time.Duration {
	if manager == nil {
		return defaultASNPreviewTimeout()
	}
	current, err := manager.Get()
	if err != nil {
		return defaultASNPreviewTimeout()
	}
	seconds := current.ResolverASNTimeoutSeconds
	if seconds <= 0 {
		seconds = current.ResolverTimeoutSeconds
	}
	if seconds <= 0 {
		seconds = defaultASNPreviewTimeoutSeconds
	}
	if seconds > maxASNPreviewTimeoutSeconds {
		seconds = maxASNPreviewTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func defaultASNPreviewTimeout() time.Duration {
	return time.Duration(defaultASNPreviewTimeoutSeconds) * time.Second
}

func isASNPreviewValidationError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "invalid asn") ||
		strings.Contains(message, "at least one asn is required")
}
