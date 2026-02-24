package routing

import (
	"context"
	"database/sql"
)

// UpsertPrewarmSnapshot adds or refreshes cached pre-warm rows atomically.
func (s *Store) UpsertPrewarmSnapshot(ctx context.Context, snapshot map[string]ResolverValues) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := upsertPrewarmSnapshotTx(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := purgeExpiredPrewarmCacheTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearPrewarmCache removes all pre-warm cache rows.
func (s *Store) ClearPrewarmCache(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM prewarm_cache`)
	return err
}

// PurgeExpiredPrewarmCache evicts cache rows older than retention.
func (s *Store) PurgeExpiredPrewarmCache(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM prewarm_cache
		WHERE updated_at < (strftime('%s','now') - ?)
	`, discoveryCacheRetentionSeconds)
	return err
}

// LoadPrewarmSnapshot returns active cached pre-warm rows keyed by destination set.
func (s *Store) LoadPrewarmSnapshot(ctx context.Context) (map[string]ResolverValues, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT set_name, family, cidr
		FROM prewarm_cache
		WHERE updated_at >= (strftime('%s','now') - ?)
		ORDER BY set_name ASC, family ASC, cidr ASC
	`, discoveryCacheRetentionSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]ResolverValues)
	for rows.Next() {
		var setName string
		var family string
		var cidr string
		if err := rows.Scan(&setName, &family, &cidr); err != nil {
			return nil, err
		}
		entry := result[setName]
		if family == "inet6" {
			entry.V6 = append(entry.V6, cidr)
		} else {
			entry.V4 = append(entry.V4, cidr)
		}
		result[setName] = entry
	}
	return result, rows.Err()
}

func upsertPrewarmSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	snapshot map[string]ResolverValues,
) error {
	for setName, values := range snapshot {
		for _, cidr := range values.V4 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO prewarm_cache (set_name, family, cidr, updated_at)
				VALUES (?, 'inet', ?, strftime('%s','now'))
				ON CONFLICT(set_name, family, cidr)
				DO UPDATE SET updated_at = excluded.updated_at
			`, setName, cidr); err != nil {
				return err
			}
		}
		for _, cidr := range values.V6 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO prewarm_cache (set_name, family, cidr, updated_at)
				VALUES (?, 'inet6', ?, strftime('%s','now'))
				ON CONFLICT(set_name, family, cidr)
				DO UPDATE SET updated_at = excluded.updated_at
			`, setName, cidr); err != nil {
				return err
			}
		}
	}
	return nil
}

func purgeExpiredPrewarmCacheTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM prewarm_cache
		WHERE updated_at < (strftime('%s','now') - ?)
	`, discoveryCacheRetentionSeconds)
	return err
}
