package routing

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"

	"split-vpn-webui/internal/vpn"
)

const (
	setPrefix       = "svpn_"
	setSuffixV4     = "_v4"
	setSuffixV6     = "_v6"
	maxIPSetNameLen = 31
)

var (
	groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

	// ErrGroupNotFound indicates the requested group id does not exist.
	ErrGroupNotFound = fmt.Errorf("domain group not found")
	// ErrGroupValidation indicates invalid input payload.
	ErrGroupValidation = fmt.Errorf("domain group validation failed")
)

// DomainGroup is a persisted domain routing group.
type DomainGroup struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	EgressVPN string   `json:"egressVpn"`
	Domains   []string `json:"domains"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

// RouteBinding describes ipset/routing state derived from a domain group and VPN.
type RouteBinding struct {
	GroupName   string
	SetV4       string
	SetV6       string
	Mark        uint32
	RouteTable  int
	Interface   string
	EgressVPN   string
	DomainCount int
}

// NormalizeAndValidate validates a group and returns a canonical version.
func NormalizeAndValidate(group DomainGroup) (DomainGroup, error) {
	trimmedName := strings.TrimSpace(group.Name)
	if trimmedName == "" {
		return DomainGroup{}, fmt.Errorf("%w: group name is required", ErrGroupValidation)
	}
	if !groupNamePattern.MatchString(trimmedName) {
		return DomainGroup{}, fmt.Errorf("%w: group name %q is invalid", ErrGroupValidation, group.Name)
	}
	egress := strings.TrimSpace(group.EgressVPN)
	if err := vpn.ValidateName(egress); err != nil {
		return DomainGroup{}, fmt.Errorf("%w: invalid egress vpn: %v", ErrGroupValidation, err)
	}
	if len(group.Domains) == 0 {
		return DomainGroup{}, fmt.Errorf("%w: at least one domain is required", ErrGroupValidation)
	}
	domains, err := normalizeDomains(group.Domains)
	if err != nil {
		return DomainGroup{}, err
	}

	group.Name = trimmedName
	group.EgressVPN = egress
	group.Domains = domains
	return group, nil
}

func normalizeDomains(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	domains := make([]string, 0, len(raw))
	for _, domain := range raw {
		trimmed := strings.ToLower(strings.TrimSpace(domain))
		if trimmed == "" {
			continue
		}
		if err := vpn.ValidateDomain(trimmed); err != nil {
			return nil, fmt.Errorf("%w: invalid domain %q: %v", ErrGroupValidation, domain, err)
		}
		// dnsmasq ipset= matches subdomains with base domain automatically.
		if strings.HasPrefix(trimmed, "*.") {
			trimmed = strings.TrimPrefix(trimmed, "*.")
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		domains = append(domains, trimmed)
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: at least one valid domain is required", ErrGroupValidation)
	}
	sort.Strings(domains)
	return domains, nil
}

// GroupSetNames derives deterministic ipset names for a group.
func GroupSetNames(groupName string) (string, string) {
	normalized := normalizeSetBase(groupName)
	v4 := setPrefix + normalized + setSuffixV4
	v6 := setPrefix + normalized + setSuffixV6
	if len(v4) > maxIPSetNameLen || len(v6) > maxIPSetNameLen {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.ToLower(groupName)))
		suffix := fmt.Sprintf("%08x", h.Sum32())
		baseMax := maxIPSetNameLen - len(setPrefix) - len(setSuffixV4) - 1 - len(suffix)
		if baseMax < 3 {
			baseMax = 3
		}
		if len(normalized) > baseMax {
			normalized = normalized[:baseMax]
		}
		normalized = normalized + "_" + suffix
		v4 = setPrefix + normalized + setSuffixV4
		v6 = setPrefix + normalized + setSuffixV6
	}
	return v4, v6
}

func normalizeSetBase(groupName string) string {
	input := strings.ToLower(strings.TrimSpace(groupName))
	if input == "" {
		return "group"
	}
	builder := strings.Builder{}
	builder.Grow(len(input))
	lastUnderscore := false
	for _, r := range input {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteRune('_')
			lastUnderscore = true
		}
	}
	base := strings.Trim(builder.String(), "_")
	if base == "" {
		base = "group"
	}
	return base
}
