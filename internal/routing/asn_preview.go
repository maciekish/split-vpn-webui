package routing

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ASNPreviewItem captures per-ASN prefix and collapsed ipset entry counts.
type ASNPreviewItem struct {
	ASN        string `json:"asn"`
	PrefixesV4 int    `json:"prefixesV4"`
	PrefixesV6 int    `json:"prefixesV6"`
	EntriesV4  int    `json:"entriesV4"`
	EntriesV6  int    `json:"entriesV6"`
	Error      string `json:"error,omitempty"`
}

// ASNPreviewResult summarizes ASN preview results.
type ASNPreviewResult struct {
	Items            []ASNPreviewItem `json:"items"`
	TotalEntriesV4   int              `json:"totalEntriesV4"`
	TotalEntriesV6   int              `json:"totalEntriesV6"`
	TotalPrefixesV4  int              `json:"totalPrefixesV4"`
	TotalPrefixesV6  int              `json:"totalPrefixesV6"`
	ResolvedSelector int              `json:"resolvedSelectors"`
}

// PreviewASNEntries resolves ASN prefixes and computes collapsed ipset entry counts.
func PreviewASNEntries(ctx context.Context, asns []string, timeout time.Duration) (ASNPreviewResult, error) {
	return PreviewASNEntriesWithResolver(ctx, asns, newRIPEASNResolver(timeout))
}

// PreviewASNEntriesWithResolver is the testable resolver-injected variant.
func PreviewASNEntriesWithResolver(ctx context.Context, asns []string, resolver ASNResolver) (ASNPreviewResult, error) {
	if resolver == nil {
		return ASNPreviewResult{}, fmt.Errorf("asn resolver is required")
	}

	normalized, err := normalizePreviewASNs(asns)
	if err != nil {
		return ASNPreviewResult{}, err
	}
	if len(normalized) == 0 {
		return ASNPreviewResult{}, fmt.Errorf("at least one ASN is required")
	}

	items := make([]ASNPreviewItem, 0, len(normalized))
	mergedV4 := make([]string, 0, len(normalized))
	mergedV6 := make([]string, 0, len(normalized))
	totalPrefixesV4 := 0
	totalPrefixesV6 := 0
	resolvedSelectors := 0

	for _, asn := range normalized {
		if err := ctx.Err(); err != nil {
			return ASNPreviewResult{}, err
		}
		item := ASNPreviewItem{ASN: asn}
		values, err := resolver.Resolve(ctx, asn)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}

		item.PrefixesV4 = len(values.V4)
		item.PrefixesV6 = len(values.V6)
		totalPrefixesV4 += item.PrefixesV4
		totalPrefixesV6 += item.PrefixesV6

		v4Entries, err := collapseSetEntries(values.V4, "inet")
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		v6Entries, err := collapseSetEntries(values.V6, "inet6")
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}

		item.EntriesV4 = len(v4Entries)
		item.EntriesV6 = len(v6Entries)
		mergedV4 = append(mergedV4, values.V4...)
		mergedV6 = append(mergedV6, values.V6...)
		resolvedSelectors++
		items = append(items, item)
	}

	totalEntriesV4 := 0
	totalEntriesV6 := 0
	if len(mergedV4) > 0 {
		collapsed, err := collapseSetEntries(mergedV4, "inet")
		if err != nil {
			return ASNPreviewResult{}, err
		}
		totalEntriesV4 = len(collapsed)
	}
	if len(mergedV6) > 0 {
		collapsed, err := collapseSetEntries(mergedV6, "inet6")
		if err != nil {
			return ASNPreviewResult{}, err
		}
		totalEntriesV6 = len(collapsed)
	}

	return ASNPreviewResult{
		Items:            items,
		TotalEntriesV4:   totalEntriesV4,
		TotalEntriesV6:   totalEntriesV6,
		TotalPrefixesV4:  totalPrefixesV4,
		TotalPrefixesV6:  totalPrefixesV6,
		ResolvedSelector: resolvedSelectors,
	}, nil
}

func normalizePreviewASNs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		normalized := normalizeASNKey(trimmed)
		if normalized == "" {
			return nil, fmt.Errorf("invalid ASN %q", raw)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}
