package prewarm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RunRecord is a persisted pre-warm run.
type RunRecord struct {
	ID           int64  `json:"id"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   int64  `json:"finishedAt,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
	DomainsTotal int    `json:"domainsTotal"`
	DomainsDone  int    `json:"domainsDone"`
	IPsInserted  int    `json:"ipsInserted"`
	Error        string `json:"error,omitempty"`
}

// Store persists pre-warm run metadata to SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a run store from an existing database handle.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("database handle is required")
	}
	return &Store{db: db}, nil
}

// SaveRun inserts a run row and returns a copy with the generated id set.
func (s *Store) SaveRun(ctx context.Context, run RunRecord) (*RunRecord, error) {
	var finishedAt any
	if run.FinishedAt > 0 {
		finishedAt = run.FinishedAt
	}
	var durationMS any
	if run.DurationMS > 0 {
		durationMS = run.DurationMS
	}
	var runErr any
	if run.Error != "" {
		runErr = run.Error
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO prewarm_runs (
			started_at, finished_at, duration_ms, domains_total, domains_done, ips_inserted, error
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, run.StartedAt, finishedAt, durationMS, run.DomainsTotal, run.DomainsDone, run.IPsInserted, runErr)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	run.ID = id
	return &run, nil
}

// LastRun returns the newest run row, or nil when no runs exist.
func (s *Store) LastRun(ctx context.Context) (*RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, duration_ms, domains_total, domains_done, ips_inserted, error
		FROM prewarm_runs
		ORDER BY id DESC
		LIMIT 1
	`)

	var run RunRecord
	var finishedAt sql.NullInt64
	var durationMS sql.NullInt64
	var runErr sql.NullString
	if err := row.Scan(
		&run.ID,
		&run.StartedAt,
		&finishedAt,
		&durationMS,
		&run.DomainsTotal,
		&run.DomainsDone,
		&run.IPsInserted,
		&runErr,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.Int64
	}
	if durationMS.Valid {
		run.DurationMS = durationMS.Int64
	}
	if runErr.Valid {
		run.Error = runErr.String
	}
	return &run, nil
}
