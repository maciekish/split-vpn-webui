package routing

import (
	"fmt"
	"net/netip"
	"strings"

	"go4.org/netipx"
)

func collapseSetEntries(entries []string, family string) ([]string, error) {
	normalizedFamily := strings.ToLower(strings.TrimSpace(family))
	if normalizedFamily != "inet" && normalizedFamily != "inet6" {
		return nil, fmt.Errorf("unsupported family %q", family)
	}

	var builder netipx.IPSetBuilder
	for _, entry := range entries {
		prefix, err := parseSetEntryPrefix(entry, normalizedFamily)
		if err != nil {
			return nil, err
		}
		if !prefix.IsValid() {
			continue
		}
		builder.AddPrefix(prefix)
	}
	set, err := builder.IPSet()
	if err != nil {
		return nil, err
	}
	prefixes := set.Prefixes()
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, prefix.String())
	}
	return out, nil
}

func parseSetEntryPrefix(entry string, family string) (netip.Prefix, error) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return netip.Prefix{}, nil
	}

	if strings.Contains(trimmed, "/") {
		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: %w", entry, err)
		}
		prefix = prefix.Masked()
		if err := validateEntryFamily(prefix.Addr(), family, entry); err != nil {
			return netip.Prefix{}, err
		}
		return prefix, nil
	}

	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP %q: %w", entry, err)
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if err := validateEntryFamily(addr, family, entry); err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func validateEntryFamily(addr netip.Addr, family string, original string) error {
	if !addr.IsValid() {
		return fmt.Errorf("invalid address in %q", original)
	}
	switch family {
	case "inet":
		if !addr.Is4() {
			return fmt.Errorf("entry %q is not IPv4", original)
		}
	case "inet6":
		if !addr.Is6() || addr.Is4In6() {
			return fmt.Errorf("entry %q is not IPv6", original)
		}
	default:
		return fmt.Errorf("unsupported family %q", family)
	}
	return nil
}
