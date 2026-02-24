package routing

import (
	"context"
	"database/sql"
	"errors"
)

// ResolverSelector identifies one resolved selector source.
type ResolverSelector struct {
	Type string
	Key  string
}

// ResolverValues stores resolved IPv4/IPv6 CIDRs for one selector.
type ResolverValues struct {
	V4 []string
	V6 []string
}

// ResolverRunRecord is persisted for resolver status/history.
type ResolverRunRecord struct {
	ID               int64  `json:"id"`
	StartedAt        int64  `json:"startedAt"`
	FinishedAt       int64  `json:"finishedAt,omitempty"`
	DurationMS       int64  `json:"durationMs,omitempty"`
	SelectorsTotal   int    `json:"selectorsTotal"`
	SelectorsDone    int    `json:"selectorsDone"`
	PrefixesResolved int    `json:"prefixesResolved"`
	Error            string `json:"error,omitempty"`
}

// ReplaceResolverSnapshot replaces all cached resolver rows atomically.
// This is primarily used for explicit restore flows.
func (s *Store) ReplaceResolverSnapshot(ctx context.Context, snapshot map[ResolverSelector]ResolverValues) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM resolver_cache`); err != nil {
		return err
	}
	if err := upsertResolverSnapshotTx(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := purgeExpiredResolverCacheTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertResolverSnapshot adds or refreshes cached resolver rows atomically.
func (s *Store) UpsertResolverSnapshot(ctx context.Context, snapshot map[ResolverSelector]ResolverValues) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := upsertResolverSnapshotTx(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := purgeExpiredResolverCacheTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearResolverCache removes all resolver cache rows.
func (s *Store) ClearResolverCache(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM resolver_cache`)
	return err
}

// PurgeExpiredResolverCache evicts cache rows older than retention.
func (s *Store) PurgeExpiredResolverCache(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM resolver_cache
		WHERE updated_at < (strftime('%s','now') - ?)
	`, discoveryCacheRetentionSeconds)
	return err
}

// LoadResolverSnapshot returns all cached resolver rows keyed by selector.
func (s *Store) LoadResolverSnapshot(ctx context.Context) (map[ResolverSelector]ResolverValues, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT selector_type, selector_key, family, cidr
		FROM resolver_cache
		WHERE updated_at >= (strftime('%s','now') - ?)
		ORDER BY selector_type ASC, selector_key ASC, family ASC, cidr ASC
	`, discoveryCacheRetentionSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[ResolverSelector]ResolverValues)
	for rows.Next() {
		var selectorType string
		var selectorKey string
		var family string
		var cidr string
		if err := rows.Scan(&selectorType, &selectorKey, &family, &cidr); err != nil {
			return nil, err
		}
		selector := ResolverSelector{Type: selectorType, Key: selectorKey}
		entry := result[selector]
		if family == "inet6" {
			entry.V6 = append(entry.V6, cidr)
		} else {
			entry.V4 = append(entry.V4, cidr)
		}
		result[selector] = entry
	}
	return result, rows.Err()
}

func upsertResolverSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	snapshot map[ResolverSelector]ResolverValues,
) error {
	for selector, values := range snapshot {
		for _, cidr := range values.V4 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO resolver_cache (selector_type, selector_key, family, cidr, updated_at)
				VALUES (?, ?, 'inet', ?, strftime('%s','now'))
				ON CONFLICT(selector_type, selector_key, family, cidr)
				DO UPDATE SET updated_at = excluded.updated_at
			`, selector.Type, selector.Key, cidr); err != nil {
				return err
			}
		}
		for _, cidr := range values.V6 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO resolver_cache (selector_type, selector_key, family, cidr, updated_at)
				VALUES (?, ?, 'inet6', ?, strftime('%s','now'))
				ON CONFLICT(selector_type, selector_key, family, cidr)
				DO UPDATE SET updated_at = excluded.updated_at
			`, selector.Type, selector.Key, cidr); err != nil {
				return err
			}
		}
	}
	return nil
}

func purgeExpiredResolverCacheTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM resolver_cache
		WHERE updated_at < (strftime('%s','now') - ?)
	`, discoveryCacheRetentionSeconds)
	return err
}

// SaveResolverRun inserts one resolver run row.
func (s *Store) SaveResolverRun(ctx context.Context, run ResolverRunRecord) (*ResolverRunRecord, error) {
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
		INSERT INTO resolver_runs (
			started_at, finished_at, duration_ms, selectors_total, selectors_done, prefixes_resolved, error
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, run.StartedAt, finishedAt, durationMS, run.SelectorsTotal, run.SelectorsDone, run.PrefixesResolved, runErr)
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

// LastResolverRun returns the newest resolver run row or nil.
func (s *Store) LastResolverRun(ctx context.Context) (*ResolverRunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, duration_ms, selectors_total, selectors_done, prefixes_resolved, error
		FROM resolver_runs
		ORDER BY id DESC
		LIMIT 1
	`)

	var run ResolverRunRecord
	var finishedAt sql.NullInt64
	var durationMS sql.NullInt64
	var runErr sql.NullString
	if err := row.Scan(
		&run.ID,
		&run.StartedAt,
		&finishedAt,
		&durationMS,
		&run.SelectorsTotal,
		&run.SelectorsDone,
		&run.PrefixesResolved,
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
