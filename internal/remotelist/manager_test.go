package remotelist

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"split-vpn-webui/internal/database"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "remotelist.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	manager, err := NewManager(db)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func TestRefreshOnlyReportsChangeWhenContentDiffers(t *testing.T) {
	body := atomic.Pointer[string]{}
	initial := "1.1.1.0/24\n2.2.2.0/24\n"
	body.Store(&initial)
	requests := atomic.Int64{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(*body.Load()))
	}))
	defer server.Close()

	manager := newTestManager(t)
	changes := atomic.Int64{}
	manager.SetChangeHandler(func(ctx context.Context, changed []RefreshResult) {
		changes.Add(int64(len(changed)))
	})

	ctx := context.Background()
	created, result, err := manager.Create(ctx, UpsertRequest{
		Name:                   "telegram",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("initial fetch failed: %s", result.Error)
	}
	if !result.Changed || result.EntryCount != 2 {
		t.Fatalf("initial refresh = %+v, want changed with 2 entries", result)
	}
	if created.EntryCount != 2 || created.LastError != "" {
		t.Fatalf("created list = %+v", created)
	}
	if changes.Load() != 1 {
		t.Fatalf("change handler fired %d times, want 1", changes.Load())
	}

	// Same content, reformatted: no change, so no routing side effects.
	reformatted := "# header\n2.2.2.0/24\n\n1.1.1.0/24\n"
	body.Store(&reformatted)
	again, err := manager.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if again.Changed {
		t.Fatalf("identical content reported a change: %+v", again)
	}
	if changes.Load() != 1 {
		t.Fatalf("change handler fired %d times after no-op refresh, want 1", changes.Load())
	}

	// Real content change fires the handler again.
	updated := "1.1.1.0/24\n3.3.3.0/24\n"
	body.Store(&updated)
	third, err := manager.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !third.Changed || third.EntryCount != 2 {
		t.Fatalf("changed refresh = %+v", third)
	}
	if changes.Load() != 2 {
		t.Fatalf("change handler fired %d times, want 2", changes.Load())
	}

	entries, err := manager.Entries(ctx, created.ID)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if !reflect.DeepEqual(entries, []string{"1.1.1.0/24", "3.3.3.0/24"}) {
		t.Fatalf("entries = %v", entries)
	}
	if requests.Load() != 3 {
		t.Fatalf("server saw %d requests, want 3", requests.Load())
	}
}

func TestRefreshHonoursNotModified(t *testing.T) {
	const etag = `"v1"`
	conditional := atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte("10.0.0.0/8\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "conditional",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := manager.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Changed {
		t.Fatalf("304 response reported a change")
	}
	if conditional.Load() != 1 {
		t.Fatalf("conditional request count = %d, want 1", conditional.Load())
	}
	after, err := manager.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.EntryCount != 1 || after.LastError != "" {
		t.Fatalf("list after 304 = %+v", after)
	}
}

func TestRefreshFailureKeepsPreviousEntries(t *testing.T) {
	fail := atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("203.0.113.0/24\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "flaky",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	fail.Store(true)
	result, err := manager.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Error == "" {
		t.Fatalf("expected a fetch error")
	}
	if result.Changed {
		t.Fatalf("failed fetch must not report a change")
	}

	entries, err := manager.Entries(ctx, created.ID)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if !reflect.DeepEqual(entries, []string{"203.0.113.0/24"}) {
		t.Fatalf("failed fetch dropped cached entries: %v", entries)
	}
	after, err := manager.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.LastError == "" {
		t.Fatalf("expected the failure to be recorded on the list")
	}
}

func TestContentsExcludesDisabledListsButKeepsEmptyOnes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\n# only comments\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "Empty-List",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	contents, err := manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	content, ok := contents["empty-list"]
	if !ok {
		t.Fatalf("an empty list must still be advertised so rules stay fail-closed")
	}
	if content.Kind != KindCIDR || len(content.Entries) != 0 {
		t.Fatalf("content = %+v", content)
	}

	disabled := false
	if _, _, err := manager.Update(ctx, created.ID, UpsertRequest{
		Name:                   created.Name,
		URL:                    created.URL,
		Kind:                   created.Kind,
		RefreshIntervalSeconds: created.RefreshIntervalSeconds,
		Enabled:                &disabled,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	contents, err = manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	if _, ok := contents["empty-list"]; ok {
		t.Fatalf("disabled list must not contribute entries")
	}
}

func TestUpdateChangingSourceRefetches(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("198.51.100.0/24\n"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("AS64500\nAS64501\n"))
	}))
	defer second.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "moving",
		URL:                    first.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, result, err := manager.Update(ctx, created.ID, UpsertRequest{
		Name:                   "moving",
		URL:                    second.URL,
		Kind:                   KindASN,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !result.Changed || updated.EntryCount != 2 || updated.Kind != KindASN {
		t.Fatalf("updated = %+v, refresh = %+v", updated, result)
	}
	entries, err := manager.Entries(ctx, created.ID)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if !reflect.DeepEqual(entries, []string{"AS64500", "AS64501"}) {
		t.Fatalf("entries = %v", entries)
	}
}

func TestDuplicateNameIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3.0/24\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	req := UpsertRequest{Name: "dup", URL: server.URL, Kind: KindCIDR, RefreshIntervalSeconds: MinRefreshIntervalSeconds}
	if _, _, err := manager.Create(ctx, req); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := manager.Create(ctx, req); err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestContentsCacheIsInvalidatedOnMutation(t *testing.T) {
	body := "1.1.1.0/24\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "cached",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	if len(first["cached"].Entries) != 1 {
		t.Fatalf("contents = %+v", first)
	}

	body = "1.1.1.0/24\n9.9.9.0/24\n"
	if _, err := manager.Refresh(ctx, created.ID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second, err := manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	if len(second["cached"].Entries) != 2 {
		t.Fatalf("cache was not invalidated after a content change: %+v", second)
	}

	if err := manager.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	third, err := manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	if _, ok := third["cached"]; ok {
		t.Fatalf("cache was not invalidated after delete: %+v", third)
	}
}

// A 200 response that parses to nothing usable (captive portal, CDN error page,
// truncated transfer) must be treated as a failure rather than silently
// emptying the list's routing set.
func TestUnusableResponseDoesNotEmptyList(t *testing.T) {
	body := "1.1.1.0/24\n2.2.2.0/24\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	manager := newTestManager(t)
	changes := atomic.Int64{}
	manager.SetChangeHandler(func(ctx context.Context, changed []RefreshResult) {
		changes.Add(1)
	})

	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name:                   "hijacked",
		URL:                    server.URL,
		Kind:                   KindCIDR,
		RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if changes.Load() != 1 {
		t.Fatalf("expected the initial fetch to report a change")
	}

	body = "<html><body>502 Bad Gateway</body></html>\n"
	result, err := manager.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Changed {
		t.Fatalf("an unusable response reported a content change: %+v", result)
	}
	if result.Error == "" {
		t.Fatalf("an unusable response must be reported as an error")
	}
	if changes.Load() != 1 {
		t.Fatalf("an unusable response triggered routing side effects")
	}

	entries, err := manager.Entries(ctx, created.ID)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if !reflect.DeepEqual(entries, []string{"1.1.1.0/24", "2.2.2.0/24"}) {
		t.Fatalf("cached entries were wiped by an unusable response: %v", entries)
	}
	after, err := manager.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.EntryCount != 2 || after.LastError == "" {
		t.Fatalf("list state after an unusable response = %+v", after)
	}
}

func TestNamesCollideCaseInsensitively(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3.0/24\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	if _, _, err := manager.Create(ctx, UpsertRequest{
		Name: "Telegram", URL: server.URL, Kind: KindCIDR, RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Contents and Names key on the lower-cased name, so a case-only variant
	// would collapse two lists into one map entry.
	if _, _, err := manager.Create(ctx, UpsertRequest{
		Name: "telegram", URL: server.URL, Kind: KindDomain, RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	}); err == nil {
		t.Fatalf("expected a case-insensitive duplicate name to be rejected")
	}

	contents, err := manager.RemoteListContents(ctx)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	if len(contents) != 1 || contents["telegram"].Kind != KindCIDR {
		t.Fatalf("contents = %+v", contents)
	}
}

func TestDisabledListIsNotFetchedByRefreshAll(t *testing.T) {
	requests := atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("1.2.3.0/24\n"))
	}))
	defer server.Close()

	manager := newTestManager(t)
	ctx := context.Background()
	created, _, err := manager.Create(ctx, UpsertRequest{
		Name: "off", URL: server.URL, Kind: KindCIDR, RefreshIntervalSeconds: MinRefreshIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	disabled := false
	if _, _, err := manager.Update(ctx, created.ID, UpsertRequest{
		Name: created.Name, URL: created.URL, Kind: created.Kind,
		RefreshIntervalSeconds: created.RefreshIntervalSeconds, Enabled: &disabled,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	before := requests.Load()
	results, err := manager.RefreshAll(ctx)
	if err != nil {
		t.Fatalf("refresh all: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("a disabled list was refreshed: %+v", results)
	}
	if requests.Load() != before {
		t.Fatalf("a disabled list's URL was still fetched")
	}
}

func TestEntryCapsAreKindSpecific(t *testing.T) {
	if MaxEntriesForKind(KindCIDR) != MaxEntries {
		t.Fatalf("cidr cap = %d", MaxEntriesForKind(KindCIDR))
	}
	if MaxEntriesForKind(KindASN) != MaxEntries {
		t.Fatalf("asn cap = %d", MaxEntriesForKind(KindASN))
	}
	if MaxEntriesForKind(KindDomain) != MaxDomainEntries {
		t.Fatalf("domain cap = %d", MaxEntriesForKind(KindDomain))
	}
	if MaxEntriesForKind(KindWildcard) != MaxWildcardEntries {
		t.Fatalf("wildcard cap = %d", MaxEntriesForKind(KindWildcard))
	}

	body := make([]byte, 0, MaxWildcardEntries*20)
	for i := 0; i <= MaxWildcardEntries; i++ {
		body = append(body, []byte(fmt.Sprintf("host%d.example.com\n", i))...)
	}
	if _, _, err := parseBody(KindWildcard, body); err == nil {
		t.Fatalf("expected an oversized wildcard list to be rejected")
	}
	if _, _, err := parseBody(KindDomain, body); err != nil {
		t.Fatalf("the same list is within the domain cap: %v", err)
	}
}
