package auth

import (
	"net/http"
	"strings"
)

const sessionCookieName = "svpn_session"

// Middleware is a chi-compatible HTTP middleware that enforces authentication.
//
// Public paths that bypass auth:
//   - GET  /login   (login page)
//   - POST /login   (login form submission)
//   - POST /logout  (cookie clearing — harmless without a session)
//   - /static/*     (CSS, JS, fonts needed by the login page)
//
// API requests (/api/*) that fail auth receive a 401 JSON response.
// All other unauthenticated requests are redirected to /login.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if m.isAuthenticated(r) {
			next.ServeHTTP(w, r)
			return
		}

			if strings.HasPrefix(path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// isAuthenticated checks the request for a valid session cookie or Bearer token.
func (m *Manager) isAuthenticated(r *http.Request) bool {
	// Bearer token header takes precedence (for API/script access).
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return m.ValidateToken(strings.TrimPrefix(auth, "Bearer "))
	}
	// Session cookie — value equals the stored API token.
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		return m.ValidateToken(cookie.Value)
	}
	return false
}

func isPublicPath(path string) bool {
	return path == "/login" ||
		path == "/logout" ||
		strings.HasPrefix(path, "/static/")
}
