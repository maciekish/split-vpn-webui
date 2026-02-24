package routing

import (
	"context"
	"path/filepath"
	"testing"

	"split-vpn-webui/internal/database"
)

func TestResolverCacheLoadSkipsExpiredRows(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO resolver_cache (selector_type, selector_key, family, cidr, updated_at)
		VALUES
			('domain', 'example.com', 'inet', '1.1.1.1/32', strftime('%s','now') - ?),
			('domain', 'example.com', 'inet', '1.1.1.2/32', strftime('%s','now'))
	`, discoveryCacheRetentionSeconds+60); err != nil {
		t.Fatalf("seed resolver cache: %v", err)
	}

	loaded, err := store.LoadResolverSnapshot(ctx)
	if err != nil {
		t.Fatalf("load resolver snapshot: %v", err)
	}
	values := loaded[ResolverSelector{Type: "domain", Key: "example.com"}]
	if len(values.V4) != 1 || values.V4[0] != "1.1.1.2/32" {
		t.Fatalf("expected only fresh resolver rows, got %#v", values.V4)
	}

	if err := store.PurgeExpiredResolverCache(ctx); err != nil {
		t.Fatalf("purge resolver cache: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resolver_cache`).Scan(&count); err != nil {
		t.Fatalf("count resolver cache rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one resolver cache row after purge, got %d", count)
	}
}

func TestPrewarmCacheUpsertAndClear(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "prewarm-cache.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	ctx := context.Background()
	if err := store.UpsertPrewarmSnapshot(ctx, map[string]ResolverValues{
		"svpn_group_r1d4": {V4: []string{"203.0.113.10/32"}},
		"svpn_group_r1d6": {V6: []string{"2001:db8::10/128"}},
	}); err != nil {
		t.Fatalf("upsert prewarm snapshot: %v", err)
	}
	if err := store.UpsertPrewarmSnapshot(ctx, map[string]ResolverValues{
		"svpn_group_r1d4": {V4: []string{"203.0.113.11/32"}},
	}); err != nil {
		t.Fatalf("upsert prewarm snapshot second pass: %v", err)
	}

	loaded, err := store.LoadPrewarmSnapshot(ctx)
	if err != nil {
		t.Fatalf("load prewarm snapshot: %v", err)
	}
	if len(loaded["svpn_group_r1d4"].V4) != 2 {
		t.Fatalf("expected additive v4 prewarm cache rows, got %#v", loaded["svpn_group_r1d4"].V4)
	}
	if len(loaded["svpn_group_r1d6"].V6) != 1 {
		t.Fatalf("expected one v6 prewarm cache row, got %#v", loaded["svpn_group_r1d6"].V6)
	}

	if err := store.ClearPrewarmCache(ctx); err != nil {
		t.Fatalf("clear prewarm cache: %v", err)
	}
	loaded, err = store.LoadPrewarmSnapshot(ctx)
	if err != nil {
		t.Fatalf("load prewarm snapshot after clear: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty prewarm cache after clear, got %#v", loaded)
	}
}
