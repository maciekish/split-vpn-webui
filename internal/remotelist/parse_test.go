package remotelist

import (
	"reflect"
	"testing"
)

func TestParseBodyCIDRStripsCommentsAndCanonicalises(t *testing.T) {
	body := []byte("" +
		"# Telegram CIDRs\n" +
		"91.108.56.0/22\n" +
		"\n" +
		"91.108.4.0/22 // main\n" +
		"2001:b28:f23d::/48\n" +
		"149.154.160.0/20 ; comment\n" +
		"91.108.56.5/22\n" + // non-canonical host bits, collapses onto the first entry
		"not-a-cidr\n")

	entries, skipped, err := parseBody(KindCIDR, body)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	want := []string{"149.154.160.0/20", "2001:b28:f23d::/48", "91.108.4.0/22", "91.108.56.0/22"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestParseBodyPerKindNormalization(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		body    string
		want    []string
		skipped int
	}{
		{
			name: "asn",
			kind: KindASN,
			body: "AS13335\n15169\nas0032934\nbogus\n",
			want: []string{"AS13335", "AS15169", "AS32934"},
			// "bogus" is not numeric.
			skipped: 1,
		},
		{
			name:    "domain strips wildcard prefix",
			kind:    KindDomain,
			body:    "API.Example.COM\n*.cdn.example.net\n",
			want:    []string{"api.example.com", "cdn.example.net"},
			skipped: 0,
		},
		{
			name:    "wildcard adds prefix",
			kind:    KindWildcard,
			body:    "example.com\n*.example.net\n",
			want:    []string{"*.example.com", "*.example.net"},
			skipped: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries, skipped, err := parseBody(test.kind, []byte(test.body))
			if err != nil {
				t.Fatalf("parseBody: %v", err)
			}
			if !reflect.DeepEqual(entries, test.want) {
				t.Fatalf("entries = %v, want %v", entries, test.want)
			}
			if skipped != test.skipped {
				t.Fatalf("skipped = %d, want %d", skipped, test.skipped)
			}
		})
	}
}

func TestParseBodyRejectsUnknownKind(t *testing.T) {
	if _, _, err := parseBody("mac", []byte("1.2.3.4/32\n")); err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestHashEntriesIgnoresSourceFormatting(t *testing.T) {
	first, _, err := parseBody(KindCIDR, []byte("1.1.1.0/24\n2.2.2.0/24\n"))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	second, _, err := parseBody(KindCIDR, []byte("# reformatted\n2.2.2.0/24\n\n1.1.1.0/24   # dup ordering\n"))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if hashEntries(first) != hashEntries(second) {
		t.Fatalf("reordered identical content produced different fingerprints")
	}
	third, _, err := parseBody(KindCIDR, []byte("1.1.1.0/24\n"))
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if hashEntries(first) == hashEntries(third) {
		t.Fatalf("different content produced identical fingerprints")
	}
}

func TestNormalizeUpsertValidation(t *testing.T) {
	valid, err := NormalizeUpsert(UpsertRequest{Name: " telegram ", URL: "https://example.com/cidr.txt", Kind: "CIDR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid.Name != "telegram" || valid.Kind != KindCIDR {
		t.Fatalf("unexpected normalization: %+v", valid)
	}
	if valid.RefreshIntervalSeconds != DefaultRefreshIntervalSeconds {
		t.Fatalf("interval = %d, want default", valid.RefreshIntervalSeconds)
	}

	for _, invalid := range []UpsertRequest{
		{Name: "", URL: "https://example.com/a.txt", Kind: KindCIDR},
		{Name: "bad name", URL: "https://example.com/a.txt", Kind: KindCIDR},
		{Name: "ok", URL: "ftp://example.com/a.txt", Kind: KindCIDR},
		{Name: "ok", URL: "https:///a.txt", Kind: KindCIDR},
		{Name: "ok", URL: "https://example.com/a.txt", Kind: "mac"},
		{Name: "ok", URL: "https://example.com/a.txt", Kind: KindCIDR, RefreshIntervalSeconds: 30},
		{Name: "ok", URL: "https://example.com/a.txt", Kind: KindCIDR, RefreshIntervalSeconds: MaxRefreshIntervalSeconds + 1},
	} {
		if _, err := NormalizeUpsert(invalid); err == nil {
			t.Fatalf("expected validation error for %+v", invalid)
		}
	}
}

func TestIsDue(t *testing.T) {
	const now = 1_700_000_000
	tests := []struct {
		name string
		list List
		want bool
	}{
		{
			name: "disabled never due",
			list: List{Enabled: false, RefreshIntervalSeconds: 3600},
		},
		{
			name: "never fetched is due",
			list: List{Enabled: true, RefreshIntervalSeconds: 3600},
			want: true,
		},
		{
			name: "recent failure waits for retry window",
			list: List{Enabled: true, RefreshIntervalSeconds: 86400, LastFetchAt: now - 60, LastSuccessAt: now - 7200},
		},
		{
			name: "old failure retries before the full interval",
			list: List{Enabled: true, RefreshIntervalSeconds: 86400, LastFetchAt: now - 3600, LastSuccessAt: now - 7200},
			want: true,
		},
		{
			name: "fresh success not due",
			list: List{Enabled: true, RefreshIntervalSeconds: 3600, LastFetchAt: now - 60, LastSuccessAt: now - 60},
		},
		{
			name: "stale success is due",
			list: List{Enabled: true, RefreshIntervalSeconds: 3600, LastFetchAt: now - 7200, LastSuccessAt: now - 7200},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isDue(test.list, now); got != test.want {
				t.Fatalf("isDue = %v, want %v", got, test.want)
			}
		})
	}
}
