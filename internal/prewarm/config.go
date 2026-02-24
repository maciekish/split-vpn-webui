package prewarm

import (
	"fmt"
	"net"
	"strings"
)

const (
	maxExtraNameservers = 16
	maxECSProfiles      = 16
)

// ECSProfile describes one EDNS client subnet profile.
type ECSProfile struct {
	Name   string
	Subnet string
}

type parsedSettingLine struct {
	LineNo int
	Value  string
}

// ParseNameserverLines parses one nameserver IP per line.
func ParseNameserverLines(raw string) ([]string, error) {
	lines := parseSettingLines(raw)
	out := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))

	for _, line := range lines {
		ip := net.ParseIP(line.Value)
		if ip == nil {
			return nil, fmt.Errorf("invalid extra nameserver on line %d: %q", line.LineNo, line.Value)
		}
		normalized := ip.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
		if len(out) > maxExtraNameservers {
			return nil, fmt.Errorf("too many extra nameservers (max %d)", maxExtraNameservers)
		}
	}
	return out, nil
}

// ParseECSProfiles parses one ECS profile per line.
//
// Supported formats:
// - "region=203.0.113.0/24"
// - "203.0.113.0/24"
func ParseECSProfiles(raw string) ([]ECSProfile, error) {
	lines := parseSettingLines(raw)
	out := make([]ECSProfile, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))

	for _, line := range lines {
		name := ""
		subnet := strings.TrimSpace(line.Value)
		if idx := strings.Index(subnet, "="); idx > 0 {
			name = strings.TrimSpace(subnet[:idx])
			subnet = strings.TrimSpace(subnet[idx+1:])
		}
		if subnet == "" {
			return nil, fmt.Errorf("missing ECS subnet on line %d", line.LineNo)
		}

		_, parsedSubnet, err := net.ParseCIDR(subnet)
		if err != nil {
			return nil, fmt.Errorf("invalid ECS subnet on line %d: %q", line.LineNo, subnet)
		}
		bits, totalBits := parsedSubnet.Mask.Size()
		if bits < 0 || totalBits <= 0 {
			return nil, fmt.Errorf("invalid ECS subnet on line %d: %q", line.LineNo, subnet)
		}
		if bits == 0 {
			return nil, fmt.Errorf("ECS subnet on line %d is too broad: %q", line.LineNo, subnet)
		}

		networkIP := parsedSubnet.IP
		if v4 := networkIP.To4(); v4 != nil {
			networkIP = v4
		}
		normalizedSubnet := fmt.Sprintf("%s/%d", networkIP.String(), bits)
		if _, exists := seen[normalizedSubnet]; exists {
			continue
		}
		seen[normalizedSubnet] = struct{}{}
		if name == "" {
			name = normalizedSubnet
		}
		out = append(out, ECSProfile{Name: name, Subnet: normalizedSubnet})
		if len(out) > maxECSProfiles {
			return nil, fmt.Errorf("too many ECS profiles (max %d)", maxECSProfiles)
		}
	}
	return out, nil
}

// NormalizeMultilineSetting normalizes newline style for persisted multi-line settings.
func NormalizeMultilineSetting(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.TrimRight(normalized, "\n")
}

func parseSettingLines(raw string) []parsedSettingLine {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(normalized, "\n")
	lines := make([]parsedSettingLine, 0, len(parts))
	for idx, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if commentIdx := strings.Index(trimmed, "#"); commentIdx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:commentIdx])
		}
		if trimmed == "" {
			continue
		}
		lines = append(lines, parsedSettingLine{LineNo: idx + 1, Value: trimmed})
	}
	return lines
}
