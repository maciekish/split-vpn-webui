package server

import "net/http"

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	directory := loadDeviceDirectory(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": directory.listDevices(),
	})
}
