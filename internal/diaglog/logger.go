package diaglog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level controls diagnostic log verbosity.
type Level int

const (
	// LevelDebug emits all diagnostic entries.
	LevelDebug Level = iota
	// LevelInfo emits info, warn, error.
	LevelInfo
	// LevelWarn emits warn, error.
	LevelWarn
	// LevelError emits only errors.
	LevelError
)

// Manager writes optional diagnostic logs to a persistent file.
type Manager struct {
	path    string
	mu      sync.RWMutex
	enabled bool
	level   Level
	file    *os.File
}

// New creates a diagnostics logger writing to path when enabled.
func New(path string) *Manager {
	return &Manager{
		path:  strings.TrimSpace(path),
		level: LevelInfo,
	}
}

// Configure updates runtime logging controls.
func (m *Manager) Configure(enabled bool, levelRaw string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	level := parseLevel(levelRaw)
	m.level = level
	m.enabled = enabled
	if !enabled {
		if m.file != nil {
			_ = m.file.Close()
			m.file = nil
		}
		return nil
	}
	return m.ensureFileLocked()
}

// Close closes the diagnostics file descriptor.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	return err
}

// Debugf logs a debug-level message.
func (m *Manager) Debugf(format string, args ...any) {
	m.logf(LevelDebug, "DEBUG", format, args...)
}

// Infof logs an info-level message.
func (m *Manager) Infof(format string, args ...any) {
	m.logf(LevelInfo, "INFO", format, args...)
}

// Warnf logs a warning-level message.
func (m *Manager) Warnf(format string, args ...any) {
	m.logf(LevelWarn, "WARN", format, args...)
}

// Errorf logs an error-level message.
func (m *Manager) Errorf(format string, args ...any) {
	m.logf(LevelError, "ERROR", format, args...)
}

// Enabled returns whether diagnostics logging is currently enabled.
func (m *Manager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *Manager) logf(level Level, label string, format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || level < m.level {
		return
	}
	if err := m.ensureFileLocked(); err != nil {
		return
	}
	if m.file == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	line := fmt.Sprintf(
		"%s [%s] %s\n",
		time.Now().UTC().Format(time.RFC3339),
		label,
		message,
	)
	_, _ = m.file.WriteString(line)
}

func (m *Manager) ensureFileLocked() error {
	if m.path == "" {
		return nil
	}
	if m.file != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	m.file = file
	return nil
}

func parseLevel(raw string) Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}
