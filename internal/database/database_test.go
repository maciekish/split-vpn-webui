package database

import (
	"testing"
)

func TestOpen_InMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error: %v", err)
	}
	defer db.Close()

	// Verify all expected tables exist.
	tables := []string{"stats_history", "domain_groups", "domain_entries", "prewarm_runs"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Running migrate a second time must not error.
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
}

func TestOpen_ForeignKeys(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Insert a domain_group first.
	res, err := db.Exec("INSERT INTO domain_groups (name, egress_vpn) VALUES ('test', 'vpn0')")
	if err != nil {
		t.Fatalf("insert group: %v", err)
	}
	groupID, _ := res.LastInsertId()

	// Insert a domain entry referencing the group.
	if _, err := db.Exec("INSERT INTO domain_entries (group_id, domain) VALUES (?, 'example.com')", groupID); err != nil {
		t.Fatalf("insert entry: %v", err)
	}

	// Deleting the group should cascade-delete the entry.
	if _, err := db.Exec("DELETE FROM domain_groups WHERE id=?", groupID); err != nil {
		t.Fatalf("delete group: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM domain_entries WHERE group_id=?", groupID).Scan(&count)
	if count != 0 {
		t.Errorf("expected cascade delete, got %d orphan entries", count)
	}
}
