package backup

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/systemd"
	"split-vpn-webui/internal/vpn"
)

type configStore interface {
	BasePath() string
	AllAutostart() (map[string]bool, error)
	SetAutostart(name string, enabled bool) error
}

type settingsStore interface {
	Get() (settings.Settings, error)
	Save(settings.Settings) error
}

type vpnStore interface {
	List() ([]*vpn.VPNProfile, error)
	Create(req vpn.UpsertRequest) (*vpn.VPNProfile, error)
	Delete(name string) error
}

type routingStore interface {
	ListGroups(ctx context.Context) ([]routing.DomainGroup, error)
	LoadResolverSnapshot(ctx context.Context) (map[routing.ResolverSelector]routing.ResolverValues, error)
	ReplaceState(ctx context.Context, groups []routing.DomainGroup, snapshot map[routing.ResolverSelector]routing.ResolverValues) error
}

type systemdStore interface {
	Stop(unitName string) error
}

// Manager exports/imports full runtime configuration (excluding statistics).
type Manager struct {
	config   configStore
	settings settingsStore
	vpns     vpnStore
	routing  routingStore
	systemd  systemdStore

	now func() time.Time
	mu  sync.Mutex
}

// NewManager creates a backup manager wired to runtime managers.
func NewManager(
	configManager *config.Manager,
	settingsManager *settings.Manager,
	vpnManager *vpn.Manager,
	routingManager *routing.Manager,
	systemdManager systemd.ServiceManager,
) (*Manager, error) {
	if configManager == nil {
		return nil, fmt.Errorf("config manager is required")
	}
	if settingsManager == nil {
		return nil, fmt.Errorf("settings manager is required")
	}
	if vpnManager == nil {
		return nil, fmt.Errorf("vpn manager is required")
	}
	if routingManager == nil {
		return nil, fmt.Errorf("routing manager is required")
	}
	return &Manager{
		config:   configManager,
		settings: settingsManager,
		vpns:     vpnManager,
		routing:  routingManager,
		systemd:  systemdManager,
		now:      time.Now,
	}, nil
}

// Export returns a monolithic snapshot payload.
func (m *Manager) Export(ctx context.Context) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exportLocked(ctx)
}

// Import validates and restores a snapshot using source-style API payloads.
// On restore failure it attempts best-effort rollback to the pre-import state.
func (m *Manager) Import(ctx context.Context, snapshot Snapshot) (ImportResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized, err := normalizeSnapshot(snapshot)
	if err != nil {
		return ImportResult{}, err
	}

	current, err := m.exportLocked(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	result, importErr := m.applyLocked(ctx, normalized)
	if importErr == nil {
		return result, nil
	}
	if _, rollbackErr := m.applyLocked(ctx, current); rollbackErr != nil {
		return result, fmt.Errorf("restore failed: %v; rollback failed: %w", importErr, rollbackErr)
	}
	return result, fmt.Errorf("restore failed and was rolled back: %w", importErr)
}

func (m *Manager) exportLocked(ctx context.Context) (Snapshot, error) {
	settingsValue, err := m.settings.Get()
	if err != nil {
		return Snapshot{}, err
	}
	autostart, err := m.config.AllAutostart()
	if err != nil {
		return Snapshot{}, err
	}
	profiles, err := m.vpns.List()
	if err != nil {
		return Snapshot{}, err
	}
	groups, err := m.routing.ListGroups(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	resolverSnapshot, err := m.routing.LoadResolverSnapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	vpnRecords := make([]VPNRecord, 0, len(profiles))
	basePath := m.config.BasePath()
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		record, err := m.profileToRecord(basePath, profile, autostart[profile.Name])
		if err != nil {
			return Snapshot{}, err
		}
		vpnRecords = append(vpnRecords, record)
	}
	sort.Slice(vpnRecords, func(i, j int) bool { return vpnRecords[i].Name < vpnRecords[j].Name })

	groupRecords := make([]GroupRecord, 0, len(groups))
	for _, group := range groups {
		groupRecords = append(groupRecords, groupToRecord(group))
	}
	sort.Slice(groupRecords, func(i, j int) bool { return groupRecords[i].Name < groupRecords[j].Name })

	resolverRecords := resolverSnapshotToRecords(resolverSnapshot)
	sort.Slice(resolverRecords, func(i, j int) bool {
		if resolverRecords[i].Type != resolverRecords[j].Type {
			return resolverRecords[i].Type < resolverRecords[j].Type
		}
		return resolverRecords[i].Key < resolverRecords[j].Key
	})

	return Snapshot{
		Format:           FormatName,
		Version:          CurrentVersion,
		ExportedAt:       m.now().Unix(),
		Settings:         settingsValue,
		VPNs:             vpnRecords,
		Groups:           groupRecords,
		ResolverSnapshot: resolverRecords,
	}, nil
}

func (m *Manager) profileToRecord(basePath string, profile *vpn.VPNProfile, autostart bool) (VPNRecord, error) {
	record := VPNRecord{
		Name:           profile.Name,
		Type:           profile.Type,
		Config:         profile.RawConfig,
		ConfigFile:     profile.ConfigFile,
		InterfaceName:  profile.InterfaceName,
		BoundInterface: profile.BoundInterface,
		Autostart:      autostart,
	}
	if len(profile.SupportingFiles) == 0 {
		return record, nil
	}

	supporting := make([]vpn.SupportingFileUpload, 0, len(profile.SupportingFiles))
	for _, name := range profile.SupportingFiles {
		path := filepath.Join(basePath, profile.Name, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return VPNRecord{}, fmt.Errorf("read supporting file %s: %w", path, err)
		}
		supporting = append(supporting, vpn.SupportingFileUpload{
			Name:          name,
			ContentBase64: base64.StdEncoding.EncodeToString(content),
		})
	}
	sort.Slice(supporting, func(i, j int) bool { return supporting[i].Name < supporting[j].Name })
	record.SupportingFiles = supporting
	return record, nil
}

func (m *Manager) applyLocked(ctx context.Context, snapshot Snapshot) (ImportResult, error) {
	normalized, err := normalizeSnapshot(snapshot)
	if err != nil {
		return ImportResult{}, err
	}

	// Clear routing first so old egress references do not block VPN replacement.
	if err := m.routing.ReplaceState(ctx, nil, nil); err != nil {
		return ImportResult{}, err
	}

	existing, err := m.vpns.List()
	if err != nil {
		return ImportResult{}, err
	}
	warnings := make([]string, 0)
	for _, profile := range existing {
		if profile == nil {
			continue
		}
		if m.systemd == nil {
			continue
		}
		unitName := vpnServiceUnitName(profile.Name)
		if err := m.systemd.Stop(unitName); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to stop %s: %v", unitName, err))
		}
	}
	sort.Slice(existing, func(i, j int) bool {
		left := ""
		if existing[i] != nil {
			left = existing[i].Name
		}
		right := ""
		if existing[j] != nil {
			right = existing[j].Name
		}
		return left < right
	})
	for _, profile := range existing {
		if profile == nil {
			continue
		}
		if err := m.vpns.Delete(profile.Name); err != nil {
			return ImportResult{Warnings: warnings}, err
		}
	}

	for _, item := range normalized.VPNs {
		request := vpn.UpsertRequest{
			Name:            item.Name,
			Type:            item.Type,
			Config:          item.Config,
			ConfigFile:      item.ConfigFile,
			SupportingFiles: append([]vpn.SupportingFileUpload(nil), item.SupportingFiles...),
			InterfaceName:   item.InterfaceName,
			BoundInterface:  item.BoundInterface,
		}
		if _, err := m.vpns.Create(request); err != nil {
			return ImportResult{Warnings: warnings}, err
		}
	}
	for _, item := range normalized.VPNs {
		if err := m.config.SetAutostart(item.Name, item.Autostart); err != nil {
			return ImportResult{Warnings: warnings}, err
		}
	}

	groupState := make([]routing.DomainGroup, 0, len(normalized.Groups))
	for _, group := range normalized.Groups {
		groupState = append(groupState, groupToRouting(group))
	}
	if err := m.routing.ReplaceState(ctx, groupState, resolverRecordsToSnapshot(normalized.ResolverSnapshot)); err != nil {
		return ImportResult{Warnings: warnings}, err
	}

	if err := m.settings.Save(normalized.Settings); err != nil {
		return ImportResult{Warnings: warnings}, err
	}
	return ImportResult{Warnings: warnings}, nil
}

func normalizeSnapshot(raw Snapshot) (Snapshot, error) {
	snapshot := raw
	if strings.TrimSpace(snapshot.Format) == "" {
		snapshot.Format = FormatName
	}
	if snapshot.Format != FormatName {
		return Snapshot{}, fmt.Errorf("%w: unsupported backup format %q", ErrInvalidSnapshot, snapshot.Format)
	}
	if snapshot.Version <= 0 {
		snapshot.Version = CurrentVersion
	}
	if snapshot.Version != CurrentVersion {
		return Snapshot{}, fmt.Errorf("%w: unsupported backup version %d", ErrInvalidSnapshot, snapshot.Version)
	}

	seenNames := make(map[string]struct{}, len(snapshot.VPNs))
	for i := range snapshot.VPNs {
		item := &snapshot.VPNs[i]
		item.Name = strings.TrimSpace(item.Name)
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		item.ConfigFile = strings.TrimSpace(item.ConfigFile)
		item.InterfaceName = strings.TrimSpace(item.InterfaceName)
		item.BoundInterface = strings.TrimSpace(item.BoundInterface)
		if err := vpn.ValidateName(item.Name); err != nil {
			return Snapshot{}, fmt.Errorf("%w: invalid vpn name %q: %v", ErrInvalidSnapshot, item.Name, err)
		}
		if _, exists := seenNames[item.Name]; exists {
			return Snapshot{}, fmt.Errorf("%w: duplicate vpn name %q", ErrInvalidSnapshot, item.Name)
		}
		seenNames[item.Name] = struct{}{}
		if item.Type != "wireguard" && item.Type != "openvpn" {
			return Snapshot{}, fmt.Errorf("%w: vpn %q has unsupported type %q", ErrInvalidSnapshot, item.Name, item.Type)
		}
		if strings.TrimSpace(item.Config) == "" {
			return Snapshot{}, fmt.Errorf("%w: vpn %q config is empty", ErrInvalidSnapshot, item.Name)
		}
		sort.Slice(item.SupportingFiles, func(left, right int) bool {
			return item.SupportingFiles[left].Name < item.SupportingFiles[right].Name
		})
	}
	sort.Slice(snapshot.VPNs, func(i, j int) bool { return snapshot.VPNs[i].Name < snapshot.VPNs[j].Name })

	for i := range snapshot.Groups {
		group := &snapshot.Groups[i]
		group.Name = strings.TrimSpace(group.Name)
		group.EgressVPN = strings.TrimSpace(group.EgressVPN)
		routingGroup, err := routing.NormalizeAndValidate(groupToRouting(*group))
		if err != nil {
			return Snapshot{}, fmt.Errorf("%w: invalid group %q: %v", ErrInvalidSnapshot, group.Name, err)
		}
		if _, exists := seenNames[routingGroup.EgressVPN]; !exists {
			return Snapshot{}, fmt.Errorf(
				"%w: group %q references missing egress vpn %q",
				ErrInvalidSnapshot,
				routingGroup.Name,
				routingGroup.EgressVPN,
			)
		}
		*group = groupToRecord(routingGroup)
	}
	sort.Slice(snapshot.Groups, func(i, j int) bool { return snapshot.Groups[i].Name < snapshot.Groups[j].Name })

	for i := range snapshot.ResolverSnapshot {
		entry := &snapshot.ResolverSnapshot[i]
		entry.Type = strings.ToLower(strings.TrimSpace(entry.Type))
		entry.Key = strings.TrimSpace(entry.Key)
		if entry.Key == "" {
			return Snapshot{}, fmt.Errorf("%w: resolver selector key is required", ErrInvalidSnapshot)
		}
		switch entry.Type {
		case "domain", "asn", "wildcard":
		default:
			return Snapshot{}, fmt.Errorf("%w: resolver selector type %q is invalid", ErrInvalidSnapshot, entry.Type)
		}
		entry.V4 = dedupeSorted(entry.V4)
		entry.V6 = dedupeSorted(entry.V6)
	}
	sort.Slice(snapshot.ResolverSnapshot, func(i, j int) bool {
		if snapshot.ResolverSnapshot[i].Type != snapshot.ResolverSnapshot[j].Type {
			return snapshot.ResolverSnapshot[i].Type < snapshot.ResolverSnapshot[j].Type
		}
		return snapshot.ResolverSnapshot[i].Key < snapshot.ResolverSnapshot[j].Key
	})

	return snapshot, nil
}

func groupToRecord(group routing.DomainGroup) GroupRecord {
	rules := make([]RuleRecord, 0, len(group.Rules))
	for _, rule := range group.Rules {
		ports := make([]PortRecord, 0, len(rule.DestinationPorts))
		for _, port := range rule.DestinationPorts {
			ports = append(ports, PortRecord{
				Protocol: port.Protocol,
				Start:    port.Start,
				End:      port.End,
			})
		}
		rules = append(rules, RuleRecord{
			Name:             rule.Name,
			SourceInterfaces: append([]string(nil), rule.SourceInterfaces...),
			SourceCIDRs:      append([]string(nil), rule.SourceCIDRs...),
			SourceMACs:       append([]string(nil), rule.SourceMACs...),
			DestinationCIDRs: append([]string(nil), rule.DestinationCIDRs...),
			DestinationPorts: ports,
			DestinationASNs:  append([]string(nil), rule.DestinationASNs...),
			Domains:          append([]string(nil), rule.Domains...),
			WildcardDomains:  append([]string(nil), rule.WildcardDomains...),
		})
	}
	return GroupRecord{
		Name:      group.Name,
		EgressVPN: group.EgressVPN,
		Rules:     rules,
	}
}

func groupToRouting(group GroupRecord) routing.DomainGroup {
	rules := make([]routing.RoutingRule, 0, len(group.Rules))
	for _, rule := range group.Rules {
		ports := make([]routing.PortRange, 0, len(rule.DestinationPorts))
		for _, port := range rule.DestinationPorts {
			ports = append(ports, routing.PortRange{
				Protocol: port.Protocol,
				Start:    port.Start,
				End:      port.End,
			})
		}
		rules = append(rules, routing.RoutingRule{
			Name:             rule.Name,
			SourceInterfaces: append([]string(nil), rule.SourceInterfaces...),
			SourceCIDRs:      append([]string(nil), rule.SourceCIDRs...),
			SourceMACs:       append([]string(nil), rule.SourceMACs...),
			DestinationCIDRs: append([]string(nil), rule.DestinationCIDRs...),
			DestinationPorts: ports,
			DestinationASNs:  append([]string(nil), rule.DestinationASNs...),
			Domains:          append([]string(nil), rule.Domains...),
			WildcardDomains:  append([]string(nil), rule.WildcardDomains...),
		})
	}
	return routing.DomainGroup{
		Name:      group.Name,
		EgressVPN: group.EgressVPN,
		Rules:     rules,
	}
}

func resolverSnapshotToRecords(
	snapshot map[routing.ResolverSelector]routing.ResolverValues,
) []ResolverCacheRecord {
	if len(snapshot) == 0 {
		return nil
	}
	records := make([]ResolverCacheRecord, 0, len(snapshot))
	for selector, values := range snapshot {
		records = append(records, ResolverCacheRecord{
			Type: selector.Type,
			Key:  selector.Key,
			V4:   dedupeSorted(values.V4),
			V6:   dedupeSorted(values.V6),
		})
	}
	return records
}

func resolverRecordsToSnapshot(
	records []ResolverCacheRecord,
) map[routing.ResolverSelector]routing.ResolverValues {
	snapshot := make(map[routing.ResolverSelector]routing.ResolverValues, len(records))
	for _, item := range records {
		snapshot[routing.ResolverSelector{Type: item.Type, Key: item.Key}] = routing.ResolverValues{
			V4: append([]string(nil), item.V4...),
			V6: append([]string(nil), item.V6...),
		}
	}
	return snapshot
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func vpnServiceUnitName(name string) string {
	return "svpn-" + name + ".service"
}
