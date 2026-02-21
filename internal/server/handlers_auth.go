package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

const sessionCookieName = "svpn_session"

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	// Already authenticated — redirect to dashboard.
	if cookie, err := r.Cookie("svpn_session"); err == nil && s.auth.ValidateToken(cookie.Value) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	if !s.auth.CheckPassword(password) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.templates.ExecuteTemplate(w, "login.html", map[string]any{
			"Error": "Invalid password. Please try again.",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	token, err := s.auth.GetToken()
	if err != nil || token == "" {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleGetAuthToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.auth.GetToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleRegenerateAuthToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.auth.RegenerateToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Keep browser session alive after token rotation.
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(payload.CurrentPassword) == "" || strings.TrimSpace(payload.NewPassword) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "currentPassword and newPassword are required"})
		return
	}
	if !s.auth.CheckPassword(payload.CurrentPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}
	if err := s.auth.SetPassword(payload.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}
