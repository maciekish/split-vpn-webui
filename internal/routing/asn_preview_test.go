package routing

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type previewMockResolver struct {
	values map[string]ResolverValues
	errs   map[string]error
}

func (m *previewMockResolver) Resolve(ctx context.Context, asn string) (ResolverValues, error) {
	if err := ctx.Err(); err != nil {
		return ResolverValues{}, err
	}
	if err := m.errs[asn]; err != nil {
		return ResolverValues{}, err
	}
	return m.values[asn], nil
}

func TestPreviewASNEntriesWithResolverCollapsesPerASNAndTotals(t *testing.T) {
	resolver := &previewMockResolver{
		values: map[string]ResolverValues{
			"AS13335": {
				V4: []string{"203.0.113.0/25", "203.0.113.128/25"},
				V6: []string{"2001:db8::/64", "2001:db8:0:1::/64"},
			},
			"AS15169": {
				V4: []string{"198.51.100.0/24"},
				V6: []string{"2001:db8:100::/48"},
			},
		},
	}

	result, err := PreviewASNEntriesWithResolver(context.Background(), []string{"13335", "AS15169", "AS13335"}, resolver)
	if err != nil {
		t.Fatalf("PreviewASNEntriesWithResolver failed: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 ASN preview items, got %d", len(result.Items))
	}
	first := result.Items[0]
	if first.ASN != "AS13335" || first.EntriesV4 != 1 || first.EntriesV6 != 1 {
		t.Fatalf("unexpected first item: %#v", first)
	}
	second := result.Items[1]
	if second.ASN != "AS15169" || second.EntriesV4 != 1 || second.EntriesV6 != 1 {
		t.Fatalf("unexpected second item: %#v", second)
	}
	if result.TotalEntriesV4 != 2 || result.TotalEntriesV6 != 2 {
		t.Fatalf("unexpected total entry counts: v4=%d v6=%d", result.TotalEntriesV4, result.TotalEntriesV6)
	}
	if result.TotalPrefixesV4 != 3 || result.TotalPrefixesV6 != 3 {
		t.Fatalf("unexpected total prefix counts: v4=%d v6=%d", result.TotalPrefixesV4, result.TotalPrefixesV6)
	}
	if result.ResolvedSelector != 2 {
		t.Fatalf("unexpected resolved selectors: %d", result.ResolvedSelector)
	}
}

func TestPreviewASNEntriesWithResolverHandlesPerASNErrors(t *testing.T) {
	resolver := &previewMockResolver{
		values: map[string]ResolverValues{
			"AS15169": {V4: []string{"198.51.100.0/24"}},
		},
		errs: map[string]error{
			"AS13335": errors.New("resolver unavailable"),
		},
	}

	result, err := PreviewASNEntriesWithResolver(context.Background(), []string{"AS13335", "AS15169"}, resolver)
	if err != nil {
		t.Fatalf("PreviewASNEntriesWithResolver failed: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.Items[0].Error == "" || !strings.Contains(result.Items[0].Error, "resolver unavailable") {
		t.Fatalf("expected first item error, got %#v", result.Items[0])
	}
	if result.Items[1].Error != "" {
		t.Fatalf("unexpected second item error: %#v", result.Items[1])
	}
	if result.ResolvedSelector != 1 {
		t.Fatalf("expected one resolved selector, got %d", result.ResolvedSelector)
	}
}

func TestPreviewASNEntriesWithResolverRejectsInvalidASN(t *testing.T) {
	resolver := &previewMockResolver{}
	_, err := PreviewASNEntriesWithResolver(context.Background(), []string{"AS13335", "ASbad"}, resolver)
	if err == nil {
		t.Fatalf("expected invalid ASN error")
	}
	if !strings.Contains(err.Error(), "invalid ASN") {
		t.Fatalf("unexpected error: %v", err)
	}
}
