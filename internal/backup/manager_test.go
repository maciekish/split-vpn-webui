package backup

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/vpn"
)

func TestExportIncludesSourcePayloadAndSupportingFiles(t *testing.T) {
	base := t.TempDir()
	profileDir := filepath.Join(base, "alpha")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	supportPath := filepath.Join(profileDir, "auth.txt")
	supportContent := []byte{0x00, 0x7f, 0x10, 0xff, '\n', 'a', 'b', 'c'}
	if err := os.WriteFile(supportPath, supportContent, 0o600); err != nil {
		t.Fatalf("write support file: %v", err)
	}

	manager := &Manager{
		config: &mockConfigStore{
			basePath: base,
			autostart: map[string]bool{
				"alpha": true,
			},
		},
		settings: &mockSettingsStore{
			value: settings.Settings{
				ListenInterface:  "br0",
				AuthPasswordHash: "hash",
				AuthToken:        "token",
			},
		},
		vpns: &mockVPNStore{
			profiles: map[string]*vpn.VPNProfile{
				"alpha": {
					Name:            "alpha",
					Type:            "openvpn",
					RawConfig:       "client\nremote example.com 1194\n",
					ConfigFile:      "alpha.ovpn",
					SupportingFiles: []string{"auth.txt"},
					InterfaceName:   "tun0",
					BoundInterface:  "br0",
				},
			},
		},
		routing: &mockRoutingStore{
			groups: []routing.DomainGroup{
				{
					Name:      "Streaming",
					EgressVPN: "alpha",
					Rules: []routing.RoutingRule{
						{Name: "Rule 1", Domains: []string{"example.com"}},
					},
				},
			},
			snapshot: map[routing.ResolverSelector]routing.ResolverValues{
				{Type: "domain", Key: "example.com"}: {V4: []string{"1.1.1.1/32"}},
			},
		},
		now: func() time.Time { return time.Unix(1700000000, 0) },
	}

	exported, err := manager.Export(context.Background())
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if exported.Format != FormatName {
		t.Fatalf("unexpected format: %q", exported.Format)
	}
	if exported.Version != CurrentVersion {
		t.Fatalf("unexpected version: %d", exported.Version)
	}
	if len(exported.VPNs) != 1 {
		t.Fatalf("expected one vpn record, got %d", len(exported.VPNs))
	}
	record := exported.VPNs[0]
	if !record.Autostart {
		t.Fatalf("expected autostart=true")
	}
	if len(record.SupportingFiles) != 1 {
		t.Fatalf("expected one supporting file, got %d", len(record.SupportingFiles))
	}
	decoded, err := base64.StdEncoding.DecodeString(record.SupportingFiles[0].ContentBase64)
	if err != nil {
		t.Fatalf("decode supporting file: %v", err)
	}
	if string(decoded) != string(supportContent) {
		t.Fatalf("unexpected supporting file content bytes: %#v", decoded)
	}
	if len(exported.Groups) != 1 || exported.Groups[0].Name != "Streaming" {
		t.Fatalf("unexpected groups payload: %#v", exported.Groups)
	}
	if len(exported.ResolverSnapshot) != 1 || exported.ResolverSnapshot[0].Key != "example.com" {
		t.Fatalf("unexpected resolver snapshot: %#v", exported.ResolverSnapshot)
	}
}

func TestImportRejectsInvalidSnapshotFormat(t *testing.T) {
	manager := &Manager{
		config:   &mockConfigStore{},
		settings: &mockSettingsStore{},
		vpns:     &mockVPNStore{profiles: map[string]*vpn.VPNProfile{}},
		routing:  &mockRoutingStore{},
		now:      time.Now,
	}
	_, err := manager.Import(context.Background(), Snapshot{
		Format:  "unknown-format",
		Version: CurrentVersion,
	})
	if err == nil {
		t.Fatalf("expected invalid snapshot error")
	}
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("expected ErrInvalidSnapshot, got %v", err)
	}
}

func TestImportRecreatesViaAPIAndRestoresState(t *testing.T) {
	configStore := &mockConfigStore{
		basePath:   t.TempDir(),
		autostart:  map[string]bool{"old": true},
		setHistory: make([]autostartChange, 0),
	}
	settingsStore := &mockSettingsStore{
		value: settings.Settings{
			AuthToken: "old-token",
		},
	}
	vpnStore := &mockVPNStore{
		profiles: map[string]*vpn.VPNProfile{
			"old": {
				Name:       "old",
				Type:       "openvpn",
				RawConfig:  "client\nremote old.example 1194\n",
				ConfigFile: "old.ovpn",
			},
		},
	}
	routingStore := &mockRoutingStore{
		groups: []routing.DomainGroup{
			{Name: "OldGroup", EgressVPN: "old", Rules: []routing.RoutingRule{{Name: "Rule 1", Domains: []string{"old.example"}}}},
		},
		snapshot: map[routing.ResolverSelector]routing.ResolverValues{
			{Type: "domain", Key: "old.example"}: {V4: []string{"10.0.0.1/32"}},
		},
	}
	systemdStore := &mockSystemdStore{}
	manager := &Manager{
		config:   configStore,
		settings: settingsStore,
		vpns:     vpnStore,
		routing:  routingStore,
		systemd:  systemdStore,
		now:      time.Now,
	}

	importPayload := Snapshot{
		Format:  FormatName,
		Version: CurrentVersion,
		Settings: settings.Settings{
			ListenInterface: "br0",
			AuthToken:       "new-token",
		},
		VPNs: []VPNRecord{
			{
				Name:          "new",
				Type:          "openvpn",
				Config:        "client\nremote new.example 1194\n",
				ConfigFile:    "new.ovpn",
				Autostart:     true,
				InterfaceName: "tun0",
				SupportingFiles: []vpn.SupportingFileUpload{
					{Name: "auth.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("abc"))},
				},
			},
		},
		Groups: []GroupRecord{
			{
				Name:      "NewGroup",
				EgressVPN: "new",
				Rules: []RuleRecord{
					{Name: "Rule 1", Domains: []string{"new.example"}},
				},
			},
		},
		ResolverSnapshot: []ResolverCacheRecord{
			{Type: "domain", Key: "new.example", V4: []string{"1.1.1.1/32"}},
		},
	}

	result, err := manager.Import(context.Background(), importPayload)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", result.Warnings)
	}

	if len(systemdStore.stopped) != 1 || systemdStore.stopped[0] != "svpn-old.service" {
		t.Fatalf("unexpected stopped units: %#v", systemdStore.stopped)
	}
	if len(vpnStore.deleted) != 1 || vpnStore.deleted[0] != "old" {
		t.Fatalf("unexpected deleted profiles: %#v", vpnStore.deleted)
	}
	if len(vpnStore.created) != 1 || vpnStore.created[0].Name != "new" {
		t.Fatalf("unexpected created requests: %#v", vpnStore.created)
	}
	if len(configStore.setHistory) == 0 || configStore.setHistory[len(configStore.setHistory)-1].name != "new" {
		t.Fatalf("expected autostart write for new profile, got %#v", configStore.setHistory)
	}
	if settingsStore.value.AuthToken != "new-token" {
		t.Fatalf("expected settings restore, got %#v", settingsStore.value)
	}
	if len(routingStore.replaceHistory) < 2 {
		t.Fatalf("expected at least two routing replace calls, got %d", len(routingStore.replaceHistory))
	}
	lastReplace := routingStore.replaceHistory[len(routingStore.replaceHistory)-1]
	if len(lastReplace.groups) != 1 || lastReplace.groups[0].Name != "NewGroup" {
		t.Fatalf("unexpected restored groups: %#v", lastReplace.groups)
	}
}

type mockConfigStore struct {
	basePath   string
	autostart  map[string]bool
	setHistory []autostartChange
}

type autostartChange struct {
	name    string
	enabled bool
}

func (m *mockConfigStore) BasePath() string {
	return m.basePath
}

func (m *mockConfigStore) AllAutostart() (map[string]bool, error) {
	out := make(map[string]bool, len(m.autostart))
	for key, value := range m.autostart {
		out[key] = value
	}
	return out, nil
}

func (m *mockConfigStore) SetAutostart(name string, enabled bool) error {
	if m.autostart == nil {
		m.autostart = make(map[string]bool)
	}
	m.autostart[name] = enabled
	m.setHistory = append(m.setHistory, autostartChange{name: name, enabled: enabled})
	return nil
}

type mockSettingsStore struct {
	value settings.Settings
}

func (m *mockSettingsStore) Get() (settings.Settings, error) {
	return m.value, nil
}

func (m *mockSettingsStore) Save(value settings.Settings) error {
	m.value = value
	return nil
}

type mockVPNStore struct {
	profiles map[string]*vpn.VPNProfile
	created  []vpn.UpsertRequest
	deleted  []string
}

func (m *mockVPNStore) List() ([]*vpn.VPNProfile, error) {
	keys := make([]string, 0, len(m.profiles))
	for key := range m.profiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*vpn.VPNProfile, 0, len(keys))
	for _, key := range keys {
		profile := m.profiles[key]
		copied := *profile
		out = append(out, &copied)
	}
	return out, nil
}

func (m *mockVPNStore) Create(req vpn.UpsertRequest) (*vpn.VPNProfile, error) {
	m.created = append(m.created, req)
	if m.profiles == nil {
		m.profiles = make(map[string]*vpn.VPNProfile)
	}
	m.profiles[req.Name] = &vpn.VPNProfile{
		Name:           req.Name,
		Type:           req.Type,
		RawConfig:      req.Config,
		ConfigFile:     req.ConfigFile,
		InterfaceName:  req.InterfaceName,
		BoundInterface: req.BoundInterface,
	}
	profile := m.profiles[req.Name]
	copied := *profile
	return &copied, nil
}

func (m *mockVPNStore) Delete(name string) error {
	m.deleted = append(m.deleted, name)
	delete(m.profiles, name)
	return nil
}

type mockRoutingStore struct {
	groups         []routing.DomainGroup
	snapshot       map[routing.ResolverSelector]routing.ResolverValues
	replaceHistory []replaceCall
}

type replaceCall struct {
	groups   []routing.DomainGroup
	snapshot map[routing.ResolverSelector]routing.ResolverValues
}

func (m *mockRoutingStore) ListGroups(ctx context.Context) ([]routing.DomainGroup, error) {
	out := make([]routing.DomainGroup, 0, len(m.groups))
	for _, group := range m.groups {
		out = append(out, group)
	}
	return out, nil
}

func (m *mockRoutingStore) LoadResolverSnapshot(ctx context.Context) (map[routing.ResolverSelector]routing.ResolverValues, error) {
	out := make(map[routing.ResolverSelector]routing.ResolverValues, len(m.snapshot))
	for key, value := range m.snapshot {
		out[key] = routing.ResolverValues{
			V4: append([]string(nil), value.V4...),
			V6: append([]string(nil), value.V6...),
		}
	}
	return out, nil
}

func (m *mockRoutingStore) ReplaceState(
	ctx context.Context,
	groups []routing.DomainGroup,
	snapshot map[routing.ResolverSelector]routing.ResolverValues,
) error {
	groupCopy := make([]routing.DomainGroup, 0, len(groups))
	for _, group := range groups {
		groupCopy = append(groupCopy, group)
	}
	snapshotCopy := make(map[routing.ResolverSelector]routing.ResolverValues, len(snapshot))
	for key, value := range snapshot {
		snapshotCopy[key] = routing.ResolverValues{
			V4: append([]string(nil), value.V4...),
			V6: append([]string(nil), value.V6...),
		}
	}
	m.groups = groupCopy
	m.snapshot = snapshotCopy
	m.replaceHistory = append(m.replaceHistory, replaceCall{groups: groupCopy, snapshot: snapshotCopy})
	return nil
}

type mockSystemdStore struct {
	stopped []string
}

func (m *mockSystemdStore) Stop(unitName string) error {
	m.stopped = append(m.stopped, unitName)
	return nil
}
