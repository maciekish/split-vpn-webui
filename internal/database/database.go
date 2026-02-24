// Package database manages the SQLite database used for persistent storage.
// It opens the database, enables WAL mode, and runs all schema migrations.
package database

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Open opens (or creates) the SQLite database at path and runs all migrations.
// Use ":memory:" for an in-memory database (useful in tests).
func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Keep a single writer connection to avoid SQLITE_BUSY under concurrent load.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrate executes the schema DDL. All statements are idempotent.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return ensureColumn(db, "routing_rules", "exclude_multicast", "INTEGER NOT NULL DEFAULT 1")
}

func ensureColumn(db *sql.DB, tableName, columnName, definition string) error {
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(columnName)) {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + tableName + " ADD COLUMN " + columnName + " " + definition)
	return err
}

// Cleanup prunes stale rows from tables that are expected to be bounded.
func Cleanup(db *sql.DB) error {
	return cleanupBefore(db, time.Now().UTC())
}

func cleanupBefore(db *sql.DB, now time.Time) error {
	if db == nil {
		return errors.New("database handle is required")
	}
	cutoff := now.Add(-7 * 24 * time.Hour).Unix()
	_, err := db.Exec(`DELETE FROM stats_history WHERE timestamp < ?`, cutoff)
	return err
}
