package vpn

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (m *Manager) readProfileLocked(name string) (*VPNProfile, error) {
	values, err := parseVPNConf(filepath.Join(m.vpnsDir, name, "vpn.conf"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrVPNNotFound, name)
		}
		return nil, err
	}

	vpnType := normalizeVPNType(values["VPN_PROVIDER"])
	if vpnType == "" {
		vpnType = normalizeVPNType(values["VPN_TYPE"])
	}
	provider, ok := m.providers[vpnType]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported vpn type %q for %s", ErrVPNValidation, vpnType, name)
	}

	configFileName := strings.TrimSpace(values["CONFIG_FILE"])
	if configFileName == "" {
		configFileName, err = detectConfigFile(filepath.Join(m.vpnsDir, name), vpnType)
		if err != nil {
			return nil, err
		}
	}

	rawConfigBytes, err := os.ReadFile(filepath.Join(m.vpnsDir, name, configFileName))
	if err != nil {
		return nil, err
	}
	rawConfig := string(rawConfigBytes)
	parsed, err := provider.ParseConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}

	parsed.Name = name
	parsed.Type = vpnType
	parsed.ConfigFile = configFileName
	parsed.RawConfig = rawConfig
	supportingFiles, err := listSupportingFiles(filepath.Join(m.vpnsDir, name), configFileName)
	if err != nil {
		return nil, err
	}
	parsed.SupportingFiles = supportingFiles
	parsed.InterfaceName = strings.TrimSpace(values["DEV"])
	if parsed.InterfaceName == "" {
		parsed.InterfaceName = inferInterfaceFromType(vpnType, name)
	}
	if table, err := strconv.Atoi(strings.TrimSpace(values["ROUTE_TABLE"])); err == nil {
		parsed.RouteTable = table
	}
	if mark, ok := parseMarkToken(values["MARK"]); ok {
		parsed.FWMark = mark
	}
	parsed.BoundInterface = strings.TrimSpace(values["VPN_BOUND_IFACE"])
	if endpointV4 := strings.TrimSpace(values["VPN_ENDPOINT_IPV4"]); endpointV4 != "" {
		parsed.Gateway = endpointV4
	} else if endpointV6 := strings.TrimSpace(values["VPN_ENDPOINT_IPV6"]); endpointV6 != "" {
		parsed.Gateway = endpointV6
	}
	parsed.Meta = values
	return parsed, nil
}

func parseVPNConf(path string) (VPNMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(VPNMeta)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		values[key] = strings.Trim(value, "\"'")
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmpPath := path + ".tmp"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func renderVPNConf(meta VPNMeta) string {
	order := []string{
		"VPN_PROVIDER",
		"DEV",
		"ROUTE_TABLE",
		"MARK",
		"FORCED_IPSETS",
		"VPN_ENDPOINT_IPV4",
		"VPN_ENDPOINT_IPV6",
		"VPN_BOUND_IFACE",
		"CONFIG_FILE",
	}
	lines := make([]string, 0, len(order)+2)
	seen := make(map[string]struct{}, len(meta))
	for _, key := range order {
		value, ok := meta[key]
		if !ok {
			continue
		}
		seen[key] = struct{}{}
		switch key {
		case "ROUTE_TABLE", "MARK":
			lines = append(lines, fmt.Sprintf("%s=%s", key, strings.TrimSpace(value)))
		default:
			lines = append(lines, fmt.Sprintf("%s=%q", key, value))
		}
	}

	extra := make([]string, 0, len(meta))
	for key := range meta {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	for _, key := range extra {
		lines = append(lines, fmt.Sprintf("%s=%q", key, meta[key]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func detectConfigFile(dir, vpnType string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	allowed := map[string]bool{}
	if vpnType == "openvpn" {
		allowed[".ovpn"] = true
	} else {
		allowed[".wg"] = true
		allowed[".conf"] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "vpn.conf" {
			continue
		}
		if allowed[strings.ToLower(filepath.Ext(entry.Name()))] {
			return entry.Name(), nil
		}
	}
	return "", fmt.Errorf("%w: config file missing for %s", ErrVPNValidation, filepath.Base(dir))
}
