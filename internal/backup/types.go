package backup

import (
	"errors"

	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/vpn"
)

const (
	// FormatName identifies split-vpn-webui backup files.
	FormatName = "split-vpn-webui-backup"
	// CurrentVersion is incremented on incompatible backup schema changes.
	CurrentVersion = 1
)

var (
	// ErrInvalidSnapshot indicates backup payload validation failure.
	ErrInvalidSnapshot = errors.New("invalid backup snapshot")
)

// Snapshot is the monolithic export/import payload.
type Snapshot struct {
	Format           string                `json:"format"`
	Version          int                   `json:"version"`
	ExportedAt       int64                 `json:"exportedAt"`
	Settings         settings.Settings     `json:"settings"`
	VPNs             []VPNRecord           `json:"vpns"`
	Groups           []GroupRecord         `json:"groups"`
	ResolverSnapshot []ResolverCacheRecord `json:"resolverSnapshot,omitempty"`
}

// VPNRecord stores one VPN profile in source payload form.
type VPNRecord struct {
	Name            string                     `json:"name"`
	Type            string                     `json:"type"`
	Config          string                     `json:"config"`
	ConfigFile      string                     `json:"configFile,omitempty"`
	InterfaceName   string                     `json:"interfaceName,omitempty"`
	BoundInterface  string                     `json:"boundInterface,omitempty"`
	SupportingFiles []vpn.SupportingFileUpload `json:"supportingFiles,omitempty"`
	Autostart       bool                       `json:"autostart"`
}

// GroupRecord stores one policy group and all of its selectors.
type GroupRecord struct {
	Name      string       `json:"name"`
	EgressVPN string       `json:"egressVpn"`
	Rules     []RuleRecord `json:"rules"`
}

// RuleRecord stores one AND-combined routing selector set.
type RuleRecord struct {
	Name             string       `json:"name,omitempty"`
	SourceInterfaces []string     `json:"sourceInterfaces,omitempty"`
	SourceCIDRs      []string     `json:"sourceCidrs,omitempty"`
	SourceMACs       []string     `json:"sourceMacs,omitempty"`
	DestinationCIDRs []string     `json:"destinationCidrs,omitempty"`
	DestinationPorts []PortRecord `json:"destinationPorts,omitempty"`
	DestinationASNs  []string     `json:"destinationAsns,omitempty"`
	Domains          []string     `json:"domains,omitempty"`
	WildcardDomains  []string     `json:"wildcardDomains,omitempty"`
}

// PortRecord stores one destination port/range selector.
type PortRecord struct {
	Protocol string `json:"protocol"`
	Start    int    `json:"start"`
	End      int    `json:"end,omitempty"`
}

// ResolverCacheRecord stores one selector's resolved IPv4/IPv6 prefixes.
type ResolverCacheRecord struct {
	Type string   `json:"type"`
	Key  string   `json:"key"`
	V4   []string `json:"v4,omitempty"`
	V6   []string `json:"v6,omitempty"`
}

// ImportResult includes non-fatal warnings encountered during restore.
type ImportResult struct {
	Warnings []string `json:"warnings,omitempty"`
}
