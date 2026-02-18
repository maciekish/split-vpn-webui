// Package auth manages password authentication and API token validation
// for the split-vpn-webui single-admin web interface.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"split-vpn-webui/internal/settings"
)

const defaultPassword = "split-vpn"

// bcryptCost is the work factor used when hashing passwords.
// It can be lowered in tests via the exported variable below.
var bcryptCost = bcrypt.DefaultCost

// Manager handles password authentication and API token management.
// Auth state is persisted inside the Settings struct.
type Manager struct {
	settings *settings.Manager
}

// NewManager creates an auth manager backed by the provided settings manager.
func NewManager(sm *settings.Manager) *Manager {
	return &Manager{settings: sm}
}

// EnsureDefaults initialises auth credentials on first run.
// If no password hash is stored, the default password is hashed and saved.
// If no API token is stored, a random token is generated and saved.
func (m *Manager) EnsureDefaults() error {
	s, err := m.settings.Get()
	if err != nil {
		return err
	}
	changed := false

	if s.AuthPasswordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcryptCost)
		if err != nil {
			return err
		}
		s.AuthPasswordHash = string(hash)
		changed = true
	}

	if s.AuthToken == "" {
		token, err := generateToken()
		if err != nil {
			return err
		}
		s.AuthToken = token
		changed = true
	}

	if changed {
		return m.settings.Save(s)
	}
	return nil
}

// CheckPassword returns true if plain matches the stored password hash.
// Falls back to comparing against the default password if no hash is stored yet.
func (m *Manager) CheckPassword(plain string) bool {
	s, err := m.settings.Get()
	if err != nil {
		return false
	}
	if s.AuthPasswordHash == "" {
		return plain == defaultPassword
	}
	return bcrypt.CompareHashAndPassword([]byte(s.AuthPasswordHash), []byte(plain)) == nil
}

// SetPassword hashes plain and persists the new hash.
func (m *Manager) SetPassword(plain string) error {
	if plain == "" {
		return errors.New("password cannot be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return err
	}
	s, err := m.settings.Get()
	if err != nil {
		return err
	}
	s.AuthPasswordHash = string(hash)
	return m.settings.Save(s)
}

// ValidateToken returns true if token matches the stored API token.
// Uses constant-time comparison to prevent timing attacks.
func (m *Manager) ValidateToken(token string) bool {
	if token == "" {
		return false
	}
	s, err := m.settings.Get()
	if err != nil || s.AuthToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.AuthToken)) == 1
}

// GetToken returns the current API / session token.
func (m *Manager) GetToken() (string, error) {
	s, err := m.settings.Get()
	if err != nil {
		return "", err
	}
	return s.AuthToken, nil
}

// RegenerateToken creates a new random API token, persists it, and returns it.
// All existing sessions are invalidated when the token changes.
func (m *Manager) RegenerateToken() (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	s, err := m.settings.Get()
	if err != nil {
		return "", err
	}
	s.AuthToken = token
	if err := m.settings.Save(s); err != nil {
		return "", err
	}
	return token, nil
}

// generateToken returns a cryptographically random 32-byte hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
