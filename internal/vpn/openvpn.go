package vpn

import (
	"bufio"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// OpenVPNProvider implements Provider for OpenVPN profiles.
type OpenVPNProvider struct{}

func NewOpenVPNProvider() *OpenVPNProvider {
	return &OpenVPNProvider{}
}

func (p *OpenVPNProvider) Type() string {
	return "openvpn"
}

func (p *OpenVPNProvider) ValidateConfig(raw string) error {
	_, err := p.ParseConfig(raw)
	return err
}

func (p *OpenVPNProvider) ParseConfig(raw string) (*VPNProfile, error) {
	parsed, iface, gateway, err := parseOpenVPNConfig(raw)
	if err != nil {
		return nil, err
	}
	return &VPNProfile{
		Type:          p.Type(),
		RawConfig:     raw,
		InterfaceName: iface,
		Gateway:       gateway,
		OpenVPN:       parsed,
	}, nil
}

func (p *OpenVPNProvider) GenerateUnit(profile *VPNProfile, dataDir string) string {
	if profile == nil {
		return ""
	}
	name := profile.Name
	if name == "" {
		name = "vpn"
	}
	fileName := profile.ConfigFile
	if fileName == "" {
		fileName = name + ".ovpn"
	}
	iface := profile.InterfaceName
	if iface == "" {
		iface = "tun0"
	}
	configPath := filepath.Join(dataDir, "vpns", name, fileName)
	return fmt.Sprintf(`[Unit]
Description=split-vpn-webui OpenVPN tunnel (%s)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/sbin/openvpn --config %s --dev %s --route-noexec --script-security 1
Restart=on-failure
RestartSec=5s
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
`, name, configPath, iface)
}

// ValidateOVPNConfig validates raw OpenVPN config text.
func ValidateOVPNConfig(raw string) error {
	return NewOpenVPNProvider().ValidateConfig(raw)
}

func parseOpenVPNConfig(raw string) (*OpenVPNConfig, string, string, error) {
	directives := make(map[string][]string)
	inlineBlocks := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	lineNum := 0
	activeBlock := ""
	blockLines := make([]string, 0)

	for scanner.Scan() {
		lineNum++
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)

		if activeBlock != "" {
			if strings.EqualFold(line, "</"+activeBlock+">") {
				inlineBlocks[activeBlock] = strings.Join(blockLines, "\n")
				activeBlock = ""
				blockLines = blockLines[:0]
				continue
			}
			blockLines = append(blockLines, rawLine)
			continue
		}

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "</") {
			return nil, "", "", fmt.Errorf("line %d: unexpected closing block", lineNum)
		}
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			blockName := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if blockName == "" || strings.Contains(blockName, " ") {
				return nil, "", "", fmt.Errorf("line %d: invalid inline block name", lineNum)
			}
			activeBlock = blockName
			blockLines = blockLines[:0]
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := ""
		if len(fields) > 1 {
			value = strings.TrimSpace(line[len(fields[0]):])
		}
		directives[key] = append(directives[key], value)
	}

	if err := scanner.Err(); err != nil {
		return nil, "", "", err
	}
	if activeBlock != "" {
		return nil, "", "", fmt.Errorf("unclosed inline block <%s>", activeBlock)
	}

	if _, ok := directives["client"]; !ok {
		return nil, "", "", fmt.Errorf("'client' directive is required")
	}
	remoteEntries, ok := directives["remote"]
	if !ok || len(remoteEntries) == 0 {
		return nil, "", "", fmt.Errorf("'remote' directive is required")
	}
	devEntries, ok := directives["dev"]
	if !ok || len(devEntries) == 0 {
		return nil, "", "", fmt.Errorf("'dev' directive is required")
	}

	devName := normalizeOpenVPNDevice(firstToken(devEntries[0]))
	if devName == "" {
		return nil, "", "", fmt.Errorf("invalid 'dev' directive")
	}

	gateway := parseRemoteHost(remoteEntries[0])

	return &OpenVPNConfig{
		Directives:   directives,
		InlineBlocks: inlineBlocks,
	}, devName, gateway, nil
}

func normalizeOpenVPNDevice(dev string) string {
	trimmed := strings.TrimSpace(dev)
	switch trimmed {
	case "":
		return ""
	case "tun":
		return "tun0"
	case "tap":
		return "tap0"
	default:
		return trimmed
	}
}

func parseRemoteHost(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func firstToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func requiredOpenVPNFiles(config *OpenVPNConfig) ([]string, error) {
	if config == nil {
		return nil, nil
	}
	needsFile := map[string]bool{
		"ca":            true,
		"cert":          true,
		"key":           true,
		"pkcs12":        true,
		"tls-auth":      true,
		"tls-crypt":     true,
		"tls-crypt-v2":  true,
		"auth-user-pass": true,
		"secret":        true,
		"crl-verify":    true,
	}
	inlineBlock := map[string]bool{
		"ca":           true,
		"cert":         true,
		"key":          true,
		"tls-auth":     true,
		"tls-crypt":    true,
		"tls-crypt-v2": true,
		"secret":       true,
	}

	seen := make(map[string]struct{})
	for directive, values := range config.Directives {
		key := strings.ToLower(strings.TrimSpace(directive))
		if !needsFile[key] {
			continue
		}
		if inlineBlock[key] && config.InlineBlocks[key] != "" {
			continue
		}
		for _, raw := range values {
			token := strings.Trim(strings.TrimSpace(firstToken(raw)), `"'`)
			if key == "auth-user-pass" && token == "" {
				// auth-user-pass without an argument uses interactive prompts.
				continue
			}
			if token == "" {
				return nil, fmt.Errorf("openvpn directive %q requires a supporting file", key)
			}
			name, err := sanitizeSupportingFileName(token)
			if err != nil {
				return nil, err
			}
			seen[name] = struct{}{}
		}
	}
	required := make([]string, 0, len(seen))
	for name := range seen {
		required = append(required, name)
	}
	sort.Strings(required)
	return required, nil
}
