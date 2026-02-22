package vpn

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"regexp"
	"strings"
)

var configFilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func sanitizeSupportingFileName(raw string) (string, error) {
	name := strings.Trim(strings.TrimSpace(raw), `"'`)
	if name == "" {
		return "", fmt.Errorf("%w: supporting file name is required", ErrVPNValidation)
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("%w: supporting file %q must be a base file name", ErrVPNValidation, raw)
	}
	if !configFilePattern.MatchString(name) {
		return "", fmt.Errorf("%w: supporting file name %q is invalid", ErrVPNValidation, raw)
	}
	return name, nil
}

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

func resolveConfigFileName(raw string, existing *VPNProfile, name, vpnType, interfaceName string) (string, error) {
	if vpnType == "wireguard" {
		iface := strings.TrimSpace(interfaceName)
		if err := validateInterfaceName(iface); err != nil {
			return "", fmt.Errorf("%w: %v", ErrVPNValidation, err)
		}
		return iface + ".conf", nil
	}

	requested := strings.TrimSpace(raw)
	if requested == "" && existing != nil && existing.ConfigFile != "" {
		requested = existing.ConfigFile
	}
	if requested == "" {
		requested = name + ".ovpn"
	}
	if filepath.Base(requested) != requested || strings.ContainsAny(requested, `/\\`) {
		return "", fmt.Errorf("%w: invalid config file name", ErrVPNValidation)
	}
	if !configFilePattern.MatchString(requested) {
		return "", fmt.Errorf("%w: config file name %q is invalid", ErrVPNValidation, requested)
	}
	ext := strings.ToLower(filepath.Ext(requested))
	if ext == "" {
		requested += ".ovpn"
	} else if ext != ".ovpn" {
		return "", fmt.Errorf("%w: openvpn config must use .ovpn", ErrVPNValidation)
	}
	return requested, nil
}

func resolveInterfaceName(raw string, existing *VPNProfile, parsed *VPNProfile, name string) (string, error) {
	if parsed != nil && parsed.Type == "wireguard" {
		iface := inferInterfaceFromType(parsed.Type, name)
		requested := strings.TrimSpace(raw)
		if requested != "" && !strings.EqualFold(requested, iface) {
			return "", fmt.Errorf(
				"%w: wireguard interface %q does not match managed interface %q derived from vpn name",
				ErrVPNValidation,
				requested,
				iface,
			)
		}
		if err := validateInterfaceName(iface); err != nil {
			return "", fmt.Errorf("%w: %v", ErrVPNValidation, err)
		}
		return iface, nil
	}

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
	const prefix = "wg-sv-"
	const maxLength = 15
	maxSuffix := maxLength - len(prefix)
	suffix := string(sanitized)
	if len(suffix) > maxSuffix {
		// Keep names human-readable while avoiding collisions when truncating.
		const hashWidth = 3
		baseLen := maxSuffix - hashWidth
		if baseLen < 1 {
			baseLen = maxSuffix
		}
		suffix = suffix[:baseLen] + shortHashHex(suffix, hashWidth)
	}
	return prefix + suffix
}

func shortHashHex(input string, width int) string {
	if width <= 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(input))
	sum := fmt.Sprintf("%08x", h.Sum32())
	if width >= len(sum) {
		return sum
	}
	return sum[:width]
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
