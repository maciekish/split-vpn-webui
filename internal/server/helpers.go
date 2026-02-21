package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/vpn"
)

func dominantKey(counts map[string]int) string {
	highest := 0
	winner := ""
	for key, count := range counts {
		if count > highest {
			highest = count
			winner = key
		}
	}
	return winner
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func vpnServiceUnitName(name string) string {
	return "svpn-" + name + ".service"
}

func (s *Server) requireVPNNameParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := chi.URLParam(r, "name")
	if err := vpn.ValidateName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid vpn name: %v", err)})
		return "", false
	}
	return name, true
}
