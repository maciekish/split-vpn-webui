package auth

import (
	"path/filepath"
	"testing"

	"split-vpn-webui/internal/settings"
)

func init() {
	// bcrypt.MinCost == 4; use minimum cost in tests for speed.
	bcryptCost = 4
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	sm := settings.NewManager(filepath.Join(dir, "settings.json"))
	return NewManager(sm)
}

func TestEnsureDefaults_CreatesHashAndToken(t *testing.T) {
	m := newTestManager(t)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	s, _ := m.settings.Get()
	if s.AuthPasswordHash == "" {
		t.Error("expected password hash to be set")
	}
	if s.AuthToken == "" {
		t.Error("expected auth token to be set")
	}
}

func TestEnsureDefaults_Idempotent(t *testing.T) {
	m := newTestManager(t)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("first EnsureDefaults: %v", err)
	}
	s1, _ := m.settings.Get()

	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("second EnsureDefaults: %v", err)
	}
	s2, _ := m.settings.Get()

	if s1.AuthPasswordHash != s2.AuthPasswordHash {
		t.Error("password hash changed on second call")
	}
	if s1.AuthToken != s2.AuthToken {
		t.Error("token changed on second call")
	}
}

func TestCheckPassword_DefaultPassword(t *testing.T) {
	m := newTestManager(t)
	// Before EnsureDefaults, no hash stored — falls back to plain comparison.
	if !m.CheckPassword(defaultPassword) {
		t.Error("default password should be accepted before hash is stored")
	}
	if m.CheckPassword("wrong") {
		t.Error("wrong password should be rejected")
	}
}

func TestCheckPassword_AfterSetPassword(t *testing.T) {
	m := newTestManager(t)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if !m.CheckPassword(defaultPassword) {
		t.Error("default password should work after EnsureDefaults")
	}

	if err := m.SetPassword("newpass"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if !m.CheckPassword("newpass") {
		t.Error("new password should be accepted")
	}
	if m.CheckPassword(defaultPassword) {
		t.Error("old password should be rejected after change")
	}
}

func TestSetPassword_EmptyRejected(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetPassword(""); err == nil {
		t.Error("expected error for empty password")
	}
}

func TestValidateToken(t *testing.T) {
	m := newTestManager(t)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	token, err := m.GetToken()
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}

	if !m.ValidateToken(token) {
		t.Error("stored token should be valid")
	}
	if m.ValidateToken("badtoken") {
		t.Error("wrong token should be invalid")
	}
	if m.ValidateToken("") {
		t.Error("empty token should be invalid")
	}
}

func TestRegenerateToken(t *testing.T) {
	m := newTestManager(t)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	old, _ := m.GetToken()

	newToken, err := m.RegenerateToken()
	if err != nil {
		t.Fatalf("RegenerateToken: %v", err)
	}
	if newToken == old {
		t.Error("regenerated token should differ from old token")
	}
	if !m.ValidateToken(newToken) {
		t.Error("new token should be valid")
	}
	if m.ValidateToken(old) {
		t.Error("old token should be invalidated")
	}
}
