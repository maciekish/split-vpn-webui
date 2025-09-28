package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	configFileName      = "vpn.conf"
	autostartMarkerName = ".split-vpn-webui-autostart"
)

var keyValuePattern = regexp.MustCompile(`^([A-Za-z0-9_]+)=(.*)$`)

// VPNConfig represents a parsed vpn.conf file and associated metadata.
type VPNConfig struct {
	Name          string            `json:"name"`
	Path          string            `json:"path"`
	InterfaceName string            `json:"interfaceName"`
	VPNType       string            `json:"vpnType"`
	Gateway       string            `json:"gateway"`
	RawValues     map[string]string `json:"rawValues"`
}

// Manager handles configuration discovery and persistence.
type Manager struct {
	basePath string
	mu       sync.RWMutex
	configs  map[string]*VPNConfig
}

// NewManager creates a new Manager rooted at basePath.
func NewManager(basePath string) *Manager {
	return &Manager{
		basePath: basePath,
		configs:  make(map[string]*VPNConfig),
	}
}

// BasePath returns the configured base path.
func (m *Manager) BasePath() string {
	return m.basePath
}

// Discover loads all vpn.conf files under the base path.
func (m *Manager) Discover() ([]*VPNConfig, error) {
	entries := make(map[string]*VPNConfig)
	err := filepath.WalkDir(m.basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != configFileName {
			return nil
		}
		cfg, err := m.parseConfig(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		entries[cfg.Name] = cfg
		return nil
	})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	ordered := make([]*VPNConfig, 0, len(names))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = make(map[string]*VPNConfig)
	for _, name := range names {
		cfg := entries[name]
		ordered = append(ordered, cfg)
		m.configs[name] = cfg
	}
	return ordered, nil
}

// List returns the cached configs, discovering if necessary.
func (m *Manager) List() ([]*VPNConfig, error) {
	m.mu.RLock()
	if len(m.configs) > 0 {
		configs := make([]*VPNConfig, 0, len(m.configs))
		for _, cfg := range m.configs {
			configs = append(configs, cfg)
		}
		m.mu.RUnlock()
		sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
		return configs, nil
	}
	m.mu.RUnlock()
	return m.Discover()
}

// Get fetches a config by name.
func (m *Manager) Get(name string) (*VPNConfig, error) {
	m.mu.RLock()
	cfg, ok := m.configs[name]
	m.mu.RUnlock()
	if ok {
		return cfg, nil
	}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	cfg, ok = m.configs[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("vpn %s not found", name)
	}
	return cfg, nil
}

func (m *Manager) refresh() error {
	_, err := m.Discover()
	return err
}

func (m *Manager) parseConfig(path string) (*VPNConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		matches := keyValuePattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			continue
		}
		key := strings.TrimSpace(matches[1])
		value := strings.TrimSpace(matches[2])
		value = strings.Trim(value, "\"'")
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	name := filepath.Base(dir)
	cfg := &VPNConfig{
		Name:          name,
		Path:          dir,
		InterfaceName: values["DEV"],
		VPNType:       values["VPN_TYPE"],
		Gateway:       values["VPN_GATEWAY"],
		RawValues:     values,
	}
	return cfg, nil
}

// ConfigPath returns the path to the vpn.conf for the given vpn name.
func (m *Manager) ConfigPath(name string) (string, error) {
	cfg, err := m.Get(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.Path, configFileName), nil
}

// ReadConfigFile returns the raw vpn.conf contents.
func (m *Manager) ReadConfigFile(name string) (string, error) {
	path, err := m.ConfigPath(name)
	if err != nil {
		return "", err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// WriteConfigFile overwrites vpn.conf with the provided contents.
func (m *Manager) WriteConfigFile(name, contents string) error {
	path, err := m.ConfigPath(name)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(contents), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return m.refresh()
}

// AutostartEnabled checks if autostart marker file exists for the config.
func (m *Manager) AutostartEnabled(name string) (bool, error) {
	cfg, err := m.Get(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(cfg.Path, autostartMarkerName))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// SetAutostart toggles autostart marker file.
func (m *Manager) SetAutostart(name string, enabled bool) error {
	cfg, err := m.Get(name)
	if err != nil {
		return err
	}
	marker := filepath.Join(cfg.Path, autostartMarkerName)
	if enabled {
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		file.Close()
	} else {
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// AllAutostart returns a map of autostart settings for every config.
func (m *Manager) AllAutostart() (map[string]bool, error) {
	configs, err := m.List()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(configs))
	for _, cfg := range configs {
		enabled, err := m.AutostartEnabled(cfg.Name)
		if err != nil {
			return nil, err
		}
		result[cfg.Name] = enabled
	}
	return result, nil
}
