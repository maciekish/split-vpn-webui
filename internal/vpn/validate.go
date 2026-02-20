package vpn

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	namePattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	domainLabelRegexp = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

// ValidateName checks if a VPN profile name is safe for filesystem and systemd usage.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("vpn name is required")
	}
	if trimmed != name {
		return fmt.Errorf("vpn name must not start or end with whitespace")
	}
	if len(trimmed) > 64 {
		return fmt.Errorf("vpn name must be 64 characters or fewer")
	}
	if strings.Contains(trimmed, "..") {
		return fmt.Errorf("vpn name must not contain '..'")
	}
	if strings.ContainsAny(trimmed, `/\\`) {
		return fmt.Errorf("vpn name must not contain path separators")
	}
	if strings.ContainsRune(trimmed, '@') {
		return fmt.Errorf("vpn name must not contain '@'")
	}
	for _, r := range trimmed {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("vpn name must not contain whitespace or control characters")
		}
	}
	if !namePattern.MatchString(trimmed) {
		return fmt.Errorf("vpn name must match ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$")
	}
	return nil
}

// ValidateDomain checks user-supplied domain entries, including wildcard form (*.example.com).
func ValidateDomain(domain string) error {
	trimmed := strings.TrimSpace(strings.ToLower(domain))
	if trimmed == "" {
		return fmt.Errorf("domain is required")
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return fmt.Errorf("domain must not contain whitespace")
	}
	if strings.Count(trimmed, "*") > 0 {
		if !strings.HasPrefix(trimmed, "*.") || strings.Count(trimmed, "*") != 1 {
			return fmt.Errorf("wildcard domains must use the '*.example.com' form")
		}
	}

	base := strings.TrimPrefix(trimmed, "*.")
	if base == "" {
		return fmt.Errorf("domain is required")
	}
	if len(base) > 253 {
		return fmt.Errorf("domain is too long")
	}
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, ".") {
		return fmt.Errorf("domain must not start or end with '.'")
	}

	labels := strings.Split(base, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must include at least one dot")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("domain label length must be 1-63 characters")
		}
		if !domainLabelRegexp.MatchString(label) {
			return fmt.Errorf("domain labels may only contain letters, numbers, and '-' characters")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("domain labels must not start or end with '-' characters")
		}
	}
	return nil
}
