package vpn

import (
	"fmt"
	"path/filepath"
)

// AmneziaWGProvider implements Provider for AmneziaWG profiles. The config
// format is the WireGuard wg-quick format plus AmneziaWG obfuscation keys in
// [Interface]. Tunnels run through the embedded userspace engine (or the
// amneziawg kernel module when present) via the `tunnel run` subcommand.
type AmneziaWGProvider struct{}

func NewAmneziaWGProvider() *AmneziaWGProvider {
	return &AmneziaWGProvider{}
}

func (p *AmneziaWGProvider) Type() string {
	return "amneziawg"
}

func (p *AmneziaWGProvider) ValidateConfig(raw string) error {
	_, err := p.ParseConfig(raw)
	return err
}

func (p *AmneziaWGProvider) ParseConfig(raw string) (*VPNProfile, error) {
	parsed, routeTable, gateway, err := parseWireGuardConfig(raw)
	if err != nil {
		return nil, err
	}
	params, err := parseAmneziaWGParams(&parsed.Interface)
	if err != nil {
		return nil, err
	}
	return &VPNProfile{
		Type:       p.Type(),
		RawConfig:  raw,
		RouteTable: routeTable,
		Gateway:    gateway,
		WireGuard:  parsed,
		AmneziaWG:  params,
	}, nil
}

func (p *AmneziaWGProvider) GenerateUnit(profile *VPNProfile, dataDir string) string {
	if profile == nil {
		return ""
	}
	name := profile.Name
	if name == "" {
		name = "vpn"
	}
	binaryPath := filepath.Join(dataDir, "split-vpn-webui")
	return fmt.Sprintf(`[Unit]
Description=split-vpn-webui AmneziaWG tunnel (%s)
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
NotifyAccess=main
ExecStart=%s tunnel run --name %s --data-dir %s
Restart=on-failure
RestartSec=5
TimeoutStartSec=2min
TimeoutStopSec=1min
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
`, name, binaryPath, name, dataDir)
}
