package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/util"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	interfaces, err := util.InterfacesWithAddrs()
	if err != nil {
		interfaces = nil
	}
	// Scrub auth fields — never expose hash or token via settings API.
	safe := settings.Settings{
		ListenInterface:                current.ListenInterface,
		WANInterface:                   current.WANInterface,
		PrewarmParallelism:             current.PrewarmParallelism,
		PrewarmDoHTimeoutSeconds:       current.PrewarmDoHTimeoutSeconds,
		PrewarmIntervalSeconds:         current.PrewarmIntervalSeconds,
		ResolverParallelism:            current.ResolverParallelism,
		ResolverTimeoutSeconds:         current.ResolverTimeoutSeconds,
		ResolverIntervalSeconds:        current.ResolverIntervalSeconds,
		ResolverDomainTimeoutSeconds:   current.ResolverDomainTimeoutSeconds,
		ResolverASNTimeoutSeconds:      current.ResolverASNTimeoutSeconds,
		ResolverWildcardTimeoutSeconds: current.ResolverWildcardTimeoutSeconds,
		ResolverDomainEnabled:          current.ResolverDomainEnabled,
		ResolverASNEnabled:             current.ResolverASNEnabled,
		ResolverWildcardEnabled:        current.ResolverWildcardEnabled,
		DebugLogEnabled:                current.DebugLogEnabled,
		DebugLogLevel:                  current.DebugLogLevel,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":   safe,
		"interfaces": interfaces,
	})
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	// Decode only the public, user-editable fields.
	var payload struct {
		ListenInterface                string `json:"listenInterface"`
		WANInterface                   string `json:"wanInterface"`
		PrewarmParallelism             int    `json:"prewarmParallelism"`
		PrewarmDoHTimeoutSeconds       int    `json:"prewarmDoHTimeoutSeconds"`
		PrewarmIntervalSeconds         int    `json:"prewarmIntervalSeconds"`
		ResolverParallelism            int    `json:"resolverParallelism"`
		ResolverTimeoutSeconds         int    `json:"resolverTimeoutSeconds"`
		ResolverIntervalSeconds        int    `json:"resolverIntervalSeconds"`
		ResolverDomainTimeoutSeconds   int    `json:"resolverDomainTimeoutSeconds"`
		ResolverASNTimeoutSeconds      int    `json:"resolverAsnTimeoutSeconds"`
		ResolverWildcardTimeoutSeconds int    `json:"resolverWildcardTimeoutSeconds"`
		ResolverDomainEnabled          *bool  `json:"resolverDomainEnabled"`
		ResolverASNEnabled             *bool  `json:"resolverAsnEnabled"`
		ResolverWildcardEnabled        *bool  `json:"resolverWildcardEnabled"`
		DebugLogEnabled                *bool  `json:"debugLogEnabled"`
		DebugLogLevel                  string `json:"debugLogLevel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	current, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Preserve auth fields when saving; only update network fields.
	updated := current
	updated.ListenInterface = payload.ListenInterface
	updated.WANInterface = payload.WANInterface
	updated.PrewarmParallelism = payload.PrewarmParallelism
	updated.PrewarmDoHTimeoutSeconds = payload.PrewarmDoHTimeoutSeconds
	updated.PrewarmIntervalSeconds = payload.PrewarmIntervalSeconds
	updated.ResolverParallelism = payload.ResolverParallelism
	updated.ResolverTimeoutSeconds = payload.ResolverTimeoutSeconds
	updated.ResolverIntervalSeconds = payload.ResolverIntervalSeconds
	updated.ResolverDomainTimeoutSeconds = payload.ResolverDomainTimeoutSeconds
	updated.ResolverASNTimeoutSeconds = payload.ResolverASNTimeoutSeconds
	updated.ResolverWildcardTimeoutSeconds = payload.ResolverWildcardTimeoutSeconds
	updated.ResolverDomainEnabled = payload.ResolverDomainEnabled
	updated.ResolverASNEnabled = payload.ResolverASNEnabled
	updated.ResolverWildcardEnabled = payload.ResolverWildcardEnabled
	if payload.DebugLogEnabled != nil {
		updated.DebugLogEnabled = payload.DebugLogEnabled
	}
	if payload.DebugLogLevel != "" {
		updated.DebugLogLevel = strings.ToLower(strings.TrimSpace(payload.DebugLogLevel))
	}

	if err := s.settings.Save(updated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.diagLog != nil {
		enabled := false
		if updated.DebugLogEnabled != nil {
			enabled = *updated.DebugLogEnabled
		}
		if err := s.diagLog.Configure(enabled, updated.DebugLogLevel); err != nil {
			log.Printf("diagnostics logging configure warning: %v", err)
		}
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	changed := current.ListenInterface != updated.ListenInterface ||
		current.WANInterface != updated.WANInterface
	if s.systemdManaged && changed {
		s.scheduleRestart()
	}
}

func (s *Server) scheduleRestart() {
	go func() {
		time.Sleep(500 * time.Millisecond)
		cmd := exec.Command("systemctl", "restart", "split-vpn-webui.service")
		if err := cmd.Run(); err != nil {
			log.Printf("systemd restart failed: %v", err)
			return
		}
		log.Printf("requested systemd restart after settings update")
	}()
}
