package vpn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrVPNNotFound indicates a missing profile.
	ErrVPNNotFound = errors.New("vpn not found")
	// ErrVPNAlreadyExists indicates the profile directory already exists.
	ErrVPNAlreadyExists = errors.New("vpn already exists")
	// ErrVPNValidation indicates invalid user input.
	ErrVPNValidation = errors.New("vpn validation failed")
)

// UpsertRequest defines create/update payload fields for VPN profiles.
type UpsertRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Config         string `json:"config"`
	ConfigFile     string `json:"configFile,omitempty"`
	InterfaceName  string `json:"interfaceName,omitempty"`
	BoundInterface string `json:"boundInterface,omitempty"`
}

// Manager manages persisted VPN profiles.
type Manager struct {
	mu sync.Mutex

	vpnsDir   string
	allocator *Allocator
	providers map[string]Provider
}

// NewManager creates a manager rooted at vpnsDir.
func NewManager(vpnsDir string, allocator *Allocator) (*Manager, error) {
	trimmed := strings.TrimSpace(vpnsDir)
	if trimmed == "" {
		return nil, fmt.Errorf("vpns directory is required")
	}
	if err := os.MkdirAll(trimmed, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(trimmed, 0o700); err != nil {
		return nil, err
	}
	var err error
	if allocator == nil {
		allocator, err = NewAllocator(trimmed)
		if err != nil {
			return nil, err
		}
	}
	return &Manager{
		vpnsDir:   trimmed,
		allocator: allocator,
		providers: map[string]Provider{
			"wireguard": NewWireGuardProvider(),
			"openvpn":   NewOpenVPNProvider(),
		},
	}, nil
}

// List returns all VPN profiles from disk.
func (m *Manager) List() ([]*VPNProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.vpnsDir)
	if err != nil {
		return nil, err
	}

	profiles := make([]*VPNProfile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profile, err := m.readProfileLocked(entry.Name())
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// Get returns a specific VPN profile.
func (m *Manager) Get(name string) (*VPNProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	validated, err := validateExistingName(name)
	if err != nil {
		return nil, err
	}
	return m.readProfileLocked(validated)
}

// Create creates a new VPN profile.
func (m *Manager) Create(req UpsertRequest) (*VPNProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name, err := validateCreateName(req.Name)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(m.vpnsDir, name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrVPNAlreadyExists, name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	prepared, err := m.prepareProfileLocked(name, req, nil)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		_ = os.RemoveAll(dir)
		return nil, err
	}

	if err := writeFileAtomic(filepath.Join(dir, prepared.configFileName), []byte(prepared.rawConfig), 0o600); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "vpn.conf"), []byte(renderVPNConf(prepared.meta)), 0o644); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		_ = os.RemoveAll(dir)
		return nil, err
	}

	profile, err := m.readProfileLocked(name)
	if err != nil {
		return nil, err
	}
	profile.Warnings = append(profile.Warnings, prepared.warnings...)
	return profile, nil
}

// Update updates an existing VPN profile.
func (m *Manager) Update(name string, req UpsertRequest) (*VPNProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	validatedName, err := validateExistingName(name)
	if err != nil {
		return nil, err
	}
	if req.Name != "" && req.Name != validatedName {
		return nil, fmt.Errorf("%w: renaming vpn profiles is not supported", ErrVPNValidation)
	}

	existing, err := m.readProfileLocked(validatedName)
	if err != nil {
		return nil, err
	}

	prepared, err := m.prepareProfileLocked(validatedName, req, existing)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(m.vpnsDir, validatedName)
	if err := writeFileAtomic(filepath.Join(dir, prepared.configFileName), []byte(prepared.rawConfig), 0o600); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "vpn.conf"), []byte(renderVPNConf(prepared.meta)), 0o644); err != nil {
		m.allocator.Release(prepared.routeTableReserved, prepared.markReserved)
		return nil, err
	}
	if existing.ConfigFile != "" && existing.ConfigFile != prepared.configFileName {
		_ = os.Remove(filepath.Join(dir, existing.ConfigFile))
	}
	if prepared.releaseTable > 0 || prepared.releaseMark > 0 {
		m.allocator.Release(prepared.releaseTable, prepared.releaseMark)
	}

	profile, err := m.readProfileLocked(validatedName)
	if err != nil {
		return nil, err
	}
	profile.Warnings = append(profile.Warnings, prepared.warnings...)
	return profile, nil
}

// Delete removes a VPN profile.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	validated, err := validateExistingName(name)
	if err != nil {
		return err
	}
	profile, err := m.readProfileLocked(validated)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(m.vpnsDir, validated)); err != nil {
		return err
	}
	m.allocator.Release(profile.RouteTable, profile.FWMark)
	return nil
}

type preparedProfile struct {
	meta           VPNMeta
	rawConfig      string
	configFileName string
	warnings       []string

	routeTableReserved int
	markReserved       uint32
	releaseTable       int
	releaseMark        uint32
}
