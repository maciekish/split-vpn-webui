package server

import (
	"encoding/json"
	"net/http"
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
