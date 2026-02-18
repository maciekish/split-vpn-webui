// Package database manages the SQLite database used for persistent storage.
// It opens the database, enables WAL mode, and runs all schema migrations.
package database

import (
	"database/sql"
	"os"
	"path/filepath"

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
	_, err := db.Exec(schema)
	return err
}
