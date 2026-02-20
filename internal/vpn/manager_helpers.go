package vpn

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var configFilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateCreateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if err := ValidateName(trimmed); err != nil {
		return "", fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	return trimmed, nil
}

func validateExistingName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if err := ValidateName(trimmed); err != nil {
		return "", fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	return trimmed, nil
}

func normalizeVPNType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "wireguard", "wg", "external":
		return "wireguard"
	case "openvpn", "ovpn":
		return "openvpn"
	default:
		return ""
	}
}

func providerConfigValue(vpnType string) string {
	if vpnType == "wireguard" {
		return "external"
	}
	return "openvpn"
}

func resolveConfigFileName(raw string, existing *VPNProfile, name, vpnType string) (string, error) {
	requested := strings.TrimSpace(raw)
	if requested == "" && existing != nil && existing.ConfigFile != "" {
		requested = existing.ConfigFile
	}
	if requested == "" {
		if vpnType == "wireguard" {
			requested = name + ".wg"
		} else {
			requested = name + ".ovpn"
		}
	}
	if filepath.Base(requested) != requested || strings.ContainsAny(requested, `/\\`) {
		return "", fmt.Errorf("%w: invalid config file name", ErrVPNValidation)
	}
	if !configFilePattern.MatchString(requested) {
		return "", fmt.Errorf("%w: config file name %q is invalid", ErrVPNValidation, requested)
	}
	if vpnType == "wireguard" {
		ext := strings.ToLower(filepath.Ext(requested))
		if ext == "" {
			requested += ".wg"
		} else if ext != ".wg" && ext != ".conf" {
			return "", fmt.Errorf("%w: wireguard config must use .wg or .conf", ErrVPNValidation)
		}
	} else {
		ext := strings.ToLower(filepath.Ext(requested))
		if ext == "" {
			requested += ".ovpn"
		} else if ext != ".ovpn" {
			return "", fmt.Errorf("%w: openvpn config must use .ovpn", ErrVPNValidation)
		}
	}
	return requested, nil
}

func resolveInterfaceName(raw string, existing *VPNProfile, parsed *VPNProfile, name string) (string, error) {
	iface := strings.TrimSpace(raw)
	if iface == "" && existing != nil {
		iface = strings.TrimSpace(existing.InterfaceName)
	}
	if iface == "" && parsed != nil {
		iface = strings.TrimSpace(parsed.InterfaceName)
	}
	if iface == "" && parsed != nil {
		iface = inferInterfaceFromType(parsed.Type, name)
	}
	if err := validateInterfaceName(iface); err != nil {
		return "", fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	return iface, nil
}

func inferInterfaceFromType(vpnType, name string) string {
	sanitized := make([]rune, 0, len(name))
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sanitized = append(sanitized, r)
		}
	}
	if len(sanitized) == 0 {
		sanitized = []rune("0")
	}
	if vpnType == "openvpn" {
		if len(sanitized) > 3 {
			sanitized = sanitized[:3]
		}
		return "tun" + string(sanitized)
	}
	maxSuffix := 12
	if len(sanitized) > maxSuffix {
		sanitized = sanitized[:maxSuffix]
	}
	return "wg-" + string(sanitized)
}

func validateInterfaceName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("interface name is required")
	}
	if len(trimmed) > 15 {
		return fmt.Errorf("interface name must be 15 characters or fewer")
	}
	for _, r := range trimmed {
		isAllowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.'
		if !isAllowed {
			return fmt.Errorf("interface name contains invalid character %q", r)
		}
	}
	return nil
}

func vpnServiceUnitName(name string) string {
	return "svpn-" + name + ".service"
}
