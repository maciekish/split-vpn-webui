package vpn

import (
	"bufio"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// WireGuardProvider implements Provider for WireGuard profiles.
type WireGuardProvider struct{}

func NewWireGuardProvider() *WireGuardProvider {
	return &WireGuardProvider{}
}

func (p *WireGuardProvider) Type() string {
	return "wireguard"
}

func (p *WireGuardProvider) ValidateConfig(raw string) error {
	_, err := p.ParseConfig(raw)
	return err
}

func (p *WireGuardProvider) ParseConfig(raw string) (*VPNProfile, error) {
	parsed, routeTable, gateway, err := parseWireGuardConfig(raw)
	if err != nil {
		return nil, err
	}
	return &VPNProfile{
		Type:       p.Type(),
		RawConfig:  raw,
		RouteTable: routeTable,
		Gateway:    gateway,
		WireGuard:  parsed,
	}, nil
}

func (p *WireGuardProvider) GenerateUnit(profile *VPNProfile, dataDir string) string {
	if profile == nil {
		return ""
	}
	name := profile.Name
	if name == "" {
		name = "vpn"
	}
	fileName := profile.ConfigFile
	if fileName == "" {
		fileName = name + ".wg"
	}
	configPath := filepath.Join(dataDir, "vpns", name, fileName)
	return fmt.Sprintf(`[Unit]
Description=split-vpn-webui WireGuard tunnel (%s)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=WG_ENDPOINT_RESOLUTION_RETRIES=infinity
ExecStart=/usr/bin/wg-quick up %s
ExecStop=/usr/bin/wg-quick down %s
TimeoutStartSec=2min
TimeoutStopSec=1min
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
`, name, configPath, configPath)
}

// ParseWGConfig parses raw WireGuard config text.
func ParseWGConfig(raw string) (*VPNProfile, error) {
	return NewWireGuardProvider().ParseConfig(raw)
}

// ValidateWGConfig validates raw WireGuard config text.
func ValidateWGConfig(raw string) error {
	return NewWireGuardProvider().ValidateConfig(raw)
}

func parseWireGuardConfig(raw string) (*WireGuardConfig, int, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	cfg := &WireGuardConfig{
		Interface: WireGuardInterface{
			Extras: make(map[string][]string),
		},
	}

	section := ""
	var currentPeer *WireGuardPeer
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "interface":
				currentPeer = nil
			case "peer":
				cfg.Peers = append(cfg.Peers, WireGuardPeer{Extras: make(map[string][]string)})
				currentPeer = &cfg.Peers[len(cfg.Peers)-1]
			default:
				return nil, 0, "", fmt.Errorf("line %d: unsupported section [%s]", lineNum, section)
			}
			continue
		}

		key, value, ok := splitINIKeyValue(line)
		if !ok {
			return nil, 0, "", fmt.Errorf("line %d: invalid key-value pair", lineNum)
		}
		value = stripInlineComment(value)
		lowerKey := strings.ToLower(key)

		switch section {
		case "interface":
			applyWireGuardInterfaceField(&cfg.Interface, lowerKey, value)
		case "peer":
			if currentPeer == nil {
				return nil, 0, "", fmt.Errorf("line %d: key outside of [Peer] section", lineNum)
			}
			applyWireGuardPeerField(currentPeer, lowerKey, value)
		default:
			return nil, 0, "", fmt.Errorf("line %d: key outside known section", lineNum)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, "", err
	}

	if cfg.Interface.PrivateKey == "" {
		return nil, 0, "", fmt.Errorf("[Interface] PrivateKey is required")
	}
	if len(cfg.Interface.Addresses) == 0 {
		return nil, 0, "", fmt.Errorf("[Interface] Address is required")
	}
	if len(cfg.Peers) == 0 {
		return nil, 0, "", fmt.Errorf("at least one [Peer] section is required")
	}

	for i, peer := range cfg.Peers {
		index := i + 1
		if peer.PublicKey == "" {
			return nil, 0, "", fmt.Errorf("[Peer %d] PublicKey is required", index)
		}
		if len(peer.AllowedIPs) == 0 {
			return nil, 0, "", fmt.Errorf("[Peer %d] AllowedIPs is required", index)
		}
		if peer.Endpoint == "" {
			return nil, 0, "", fmt.Errorf("[Peer %d] Endpoint is required", index)
		}
	}

	routeTable := 0
	if table := strings.TrimSpace(cfg.Interface.Table); table != "" {
		if value, err := strconv.Atoi(table); err == nil && value > 0 {
			routeTable = value
		}
	}

	gateway := ""
	if len(cfg.Peers) > 0 {
		gateway = parseEndpointHost(cfg.Peers[0].Endpoint)
	}

	return cfg, routeTable, gateway, nil
}

func applyWireGuardInterfaceField(target *WireGuardInterface, key, value string) {
	switch key {
	case "privatekey":
		target.PrivateKey = value
	case "address":
		target.Addresses = append(target.Addresses, parseCSVList(value)...)
	case "dns":
		target.DNS = append(target.DNS, parseCSVList(value)...)
	case "table":
		target.Table = value
	case "postup":
		target.PostUp = append(target.PostUp, value)
	case "predown":
		target.PreDown = append(target.PreDown, value)
	case "postdown":
		target.PostDown = append(target.PostDown, value)
	default:
		target.Extras[key] = append(target.Extras[key], value)
	}
}

func applyWireGuardPeerField(target *WireGuardPeer, key, value string) {
	switch key {
	case "publickey":
		target.PublicKey = value
	case "presharedkey":
		target.PresharedKey = value
	case "allowedips":
		target.AllowedIPs = append(target.AllowedIPs, parseCSVList(value)...)
	case "endpoint":
		target.Endpoint = value
	case "persistentkeepalive":
		target.PersistentKeepalive = value
	default:
		target.Extras[key] = append(target.Extras[key], value)
	}
}

func splitINIKeyValue(line string) (string, string, bool) {
	if idx := strings.Index(line, "="); idx >= 0 {
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			return "", "", false
		}
		return key, value, true
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	key := fields[0]
	value := strings.TrimSpace(line[len(key):])
	return key, value, true
}

func parseCSVList(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	return items
}

func stripInlineComment(value string) string {
	for _, marker := range []string{" #", " ;"} {
		if idx := strings.Index(value, marker); idx >= 0 {
			value = value[:idx]
		}
	}
	return strings.TrimSpace(value)
}

func parseEndpointHost(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(host, "[]")
	}
	if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
		closeIdx := strings.Index(trimmed, "]")
		if closeIdx > 1 {
			return strings.TrimSpace(trimmed[1:closeIdx])
		}
	}
	if idx := strings.LastIndex(trimmed, ":"); idx > 0 {
		port := strings.TrimSpace(trimmed[idx+1:])
		if port != "" && allDigits(port) {
			return strings.TrimSpace(trimmed[:idx])
		}
	}
	return trimmed
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
