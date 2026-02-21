package routing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"split-vpn-webui/internal/database"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func TestStoreCRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, DomainGroup{
		Name:      "Streaming-SG",
		EgressVPN: "wg-sgp",
		Domains:   []string{"*.Example.com", "api.example.com", "example.com"},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("expected created id > 0, got %d", created.ID)
	}
	if len(created.Domains) != 2 {
		t.Fatalf("expected normalized deduped domains length 2, got %d", len(created.Domains))
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if fetched.Name != "Streaming-SG" || fetched.EgressVPN != "wg-sgp" {
		t.Fatalf("unexpected fetched group: %+v", fetched)
	}

	updated, err := store.Update(ctx, created.ID, DomainGroup{
		Name:      "Streaming-EU",
		EgressVPN: "ovpn-web",
		Domains:   []string{"hbo.com", "max.com"},
	})
	if err != nil {
		t.Fatalf("update group: %v", err)
	}
	if updated.Name != "Streaming-EU" {
		t.Fatalf("expected updated name Streaming-EU, got %s", updated.Name)
	}
	if len(updated.Domains) != 2 {
		t.Fatalf("expected updated domains length 2, got %d", len(updated.Domains))
	}

	groups, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound after delete, got %v", err)
	}
}

func TestStoreValidationAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.Create(ctx, DomainGroup{
		Name:      "bad name with spaces",
		EgressVPN: "wg-sgp",
		Domains:   []string{"example.com"},
	}); !errors.Is(err, ErrGroupValidation) {
		t.Fatalf("expected validation error for bad name, got %v", err)
	}

	if _, err := store.Update(ctx, 9999, DomainGroup{Name: "Gaming", EgressVPN: "wg-sgp", Domains: []string{"example.com"}}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound on update, got %v", err)
	}
	if err := store.Delete(ctx, 9999); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound on delete, got %v", err)
	}
}

func TestStoreReadsLegacyDomainEntriesAsRule(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	result, err := store.db.ExecContext(ctx, `
		INSERT INTO domain_groups (name, egress_vpn) VALUES ('Legacy', 'wg-sgp')
	`)
	if err != nil {
		t.Fatalf("insert group: %v", err)
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO domain_entries (group_id, domain) VALUES (?, 'example.com')
	`, groupID); err != nil {
		t.Fatalf("insert domain entry: %v", err)
	}

	group, err := store.Get(ctx, groupID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one generated legacy rule, got %d", len(group.Rules))
	}
	if len(group.Rules[0].Domains) != 1 || group.Rules[0].Domains[0] != "example.com" {
		t.Fatalf("unexpected legacy rule domains: %#v", group.Rules[0].Domains)
	}
}
