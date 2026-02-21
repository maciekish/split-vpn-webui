package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Settings captures user preferences and auth credentials persisted across restarts.
type Settings struct {
	// Network
	ListenInterface string `json:"listenInterface"`
	WANInterface    string `json:"wanInterface"`
	// DNS pre-warm
	PrewarmParallelism       int `json:"prewarmParallelism,omitempty"`
	PrewarmDoHTimeoutSeconds int `json:"prewarmDoHTimeoutSeconds,omitempty"`
	PrewarmIntervalSeconds   int `json:"prewarmIntervalSeconds,omitempty"`
	// Policy resolver refresh
	ResolverParallelism     int `json:"resolverParallelism,omitempty"`
	ResolverTimeoutSeconds  int `json:"resolverTimeoutSeconds,omitempty"`
	ResolverIntervalSeconds int `json:"resolverIntervalSeconds,omitempty"`

	// Auth — stored as bcrypt hash and random token.
	// These fields are omitted from JSON output on API responses;
	// only the settings Manager reads/writes them directly.
	AuthPasswordHash string `json:"authPasswordHash,omitempty"`
	AuthToken        string `json:"authToken,omitempty"`
}

// Manager handles persistence of Settings on disk.
type Manager struct {
	path   string
	mu     sync.RWMutex
	cached Settings
	loaded bool
}

// NewManager creates a settings manager whose file is at settingsPath.
// Pass the full file path (e.g. "/data/split-vpn-webui/settings.json").
func NewManager(settingsPath string) *Manager {
	return &Manager{path: settingsPath}
}

// Get returns the cached settings, loading from disk if necessary.
func (m *Manager) Get() (Settings, error) {
	m.mu.RLock()
	if m.loaded {
		defer m.mu.RUnlock()
		return m.cached, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded {
		return m.cached, nil
	}

	bytes, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.loaded = true
			m.cached = Settings{}
			return m.cached, nil
		}
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(bytes, &settings); err != nil {
		return Settings{}, err
	}
	m.cached = settings
	m.loaded = true
	return settings, nil
}

// Save persists the provided settings to disk.
func (m *Manager) Save(settings Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return err
	}
	m.cached = settings
	m.loaded = true
	return nil
}
