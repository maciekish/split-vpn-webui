package remotelist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"split-vpn-webui/internal/routing"
)

// Store persists remote list definitions, their entries and fetch state.
type Store struct {
	db *sql.DB
}

// NewStore creates a store backed by an existing SQLite handle.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("database handle is required")
	}
	return &Store{db: db}, nil
}

const listColumns = `
	id, name, url, kind, refresh_interval_seconds, enabled,
	entry_count, skipped_count, last_fetch_at, last_success_at, last_changed_at,
	last_error, created_at, updated_at
`

// List returns all configured remote lists ordered by name.
func (s *Store) List(ctx context.Context) ([]List, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT`+listColumns+`FROM remote_lists ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lists := make([]List, 0)
	for rows.Next() {
		list, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// Get returns one remote list by id.
func (s *Store) Get(ctx context.Context, id int64) (*List, error) {
	row := s.db.QueryRowContext(ctx, `SELECT`+listColumns+`FROM remote_lists WHERE id = ?`, id)
	list, err := scanList(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &list, nil
}

// Create inserts a new remote list definition.
func (s *Store) Create(ctx context.Context, req UpsertRequest) (*List, error) {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO remote_lists (name, url, kind, refresh_interval_seconds, enabled)
		VALUES (?, ?, ?, ?, ?)
	`, req.Name, req.URL, req.Kind, req.RefreshIntervalSeconds, boolToInt(enabled))
	if err != nil {
		return nil, translateUniqueError(err, req.Name)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Update overwrites a remote list definition. When the URL or kind changes the
// cached entries and fetch state are dropped so the next refresh re-reads the
// source from scratch.
func (s *Store) Update(ctx context.Context, id int64, req UpsertRequest) (*List, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sourceChanged := existing.URL != req.URL || existing.Kind != req.Kind

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE remote_lists
		SET name = ?, url = ?, kind = ?, refresh_interval_seconds = ?, enabled = ?,
		    updated_at = strftime('%s','now')
		WHERE id = ?
	`, req.Name, req.URL, req.Kind, req.RefreshIntervalSeconds, boolToInt(enabled), id); err != nil {
		return nil, translateUniqueError(err, req.Name)
	}
	if sourceChanged {
		if _, err := tx.ExecContext(ctx, `DELETE FROM remote_list_entries WHERE list_id = ?`, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE remote_lists
			SET etag = '', last_modified = '', content_hash = '', entry_count = 0,
			    skipped_count = 0, last_fetch_at = 0, last_success_at = 0, last_error = ''
			WHERE id = ?
		`, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete removes a remote list and its cached entries.
func (s *Store) Delete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM remote_lists WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// FetchState returns the conditional-request headers and content fingerprint
// recorded by the previous successful fetch.
func (s *Store) FetchState(ctx context.Context, id int64) (fetchState, error) {
	var state fetchState
	row := s.db.QueryRowContext(ctx, `
		SELECT etag, last_modified, content_hash FROM remote_lists WHERE id = ?
	`, id)
	if err := row.Scan(&state.ETag, &state.LastModified, &state.ContentHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fetchState{}, ErrNotFound
		}
		return fetchState{}, err
	}
	return state, nil
}

// SaveEntries atomically replaces a list's entries and records the new fetch
// state. It is only called when the content fingerprint actually changed.
func (s *Store) SaveEntries(ctx context.Context, id int64, entries []string, skipped int, state fetchState) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM remote_list_entries WHERE list_id = ?`, id); err != nil {
		return err
	}
	// Prepared once: a large list would otherwise re-plan the insert thousands
	// of times while holding the single SQLite write connection.
	insert, err := tx.PrepareContext(ctx, `INSERT INTO remote_list_entries (list_id, value) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, entry := range entries {
		if _, err := insert.ExecContext(ctx, id, entry); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE remote_lists
		SET etag = ?, last_modified = ?, content_hash = ?, entry_count = ?, skipped_count = ?,
		    last_error = '', last_fetch_at = strftime('%s','now'),
		    last_success_at = strftime('%s','now'), last_changed_at = strftime('%s','now'),
		    updated_at = strftime('%s','now')
		WHERE id = ?
	`, state.ETag, state.LastModified, state.ContentHash, len(entries), skipped, id); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkUnchanged records a successful fetch that produced identical content.
// The skipped count is refreshed too: a source can change only in lines that
// fail normalization, which leaves the content hash untouched.
func (s *Store) MarkUnchanged(ctx context.Context, id int64, state fetchState, skipped int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE remote_lists
		SET etag = ?, last_modified = ?, skipped_count = ?, last_error = '',
		    last_fetch_at = strftime('%s','now'), last_success_at = strftime('%s','now'),
		    updated_at = strftime('%s','now')
		WHERE id = ?
	`, state.ETag, state.LastModified, skipped, id)
	return err
}

// MarkFailed records a failed fetch, leaving the previously stored entries in
// place so a transient outage cannot silently empty a routing set.
func (s *Store) MarkFailed(ctx context.Context, id int64, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE remote_lists
		SET last_error = ?, last_fetch_at = strftime('%s','now'), updated_at = strftime('%s','now')
		WHERE id = ?
	`, truncateError(message), id)
	return err
}

// Contents returns the entries of every enabled list keyed by lower-cased name.
func (s *Store) Contents(ctx context.Context) (map[string]routing.RemoteListContent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.name, l.kind, e.value
		FROM remote_lists l
		LEFT JOIN remote_list_entries e ON e.list_id = l.id
		WHERE l.enabled = 1
		ORDER BY l.name ASC, e.value ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contents := make(map[string]routing.RemoteListContent)
	for rows.Next() {
		var name string
		var kind string
		var value sql.NullString
		if err := rows.Scan(&name, &kind, &value); err != nil {
			return nil, err
		}
		key := strings.ToLower(strings.TrimSpace(name))
		content := contents[key]
		content.Kind = kind
		if value.Valid && strings.TrimSpace(value.String) != "" {
			content.Entries = append(content.Entries, value.String)
		}
		contents[key] = content
	}
	return contents, rows.Err()
}

// Names returns the lower-cased name of every configured list, enabled or not.
func (s *Store) Names(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM remote_lists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return names, rows.Err()
}

// Entries returns the stored entries of one list.
func (s *Store) Entries(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value FROM remote_list_entries WHERE list_id = ? ORDER BY value ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		entries = append(entries, value)
	}
	return entries, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanList(row rowScanner) (List, error) {
	var list List
	var enabled int
	if err := row.Scan(
		&list.ID,
		&list.Name,
		&list.URL,
		&list.Kind,
		&list.RefreshIntervalSeconds,
		&enabled,
		&list.EntryCount,
		&list.SkippedCount,
		&list.LastFetchAt,
		&list.LastSuccessAt,
		&list.LastChangedAt,
		&list.LastError,
		&list.CreatedAt,
		&list.UpdatedAt,
	); err != nil {
		return List{}, err
	}
	list.Enabled = enabled != 0
	return list, nil
}

func translateUniqueError(err error, name string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: a remote list named %q already exists", ErrValidation, name)
	}
	return err
}

func truncateError(message string) string {
	trimmed := strings.TrimSpace(message)
	if len(trimmed) > 500 {
		return trimmed[:500]
	}
	return trimmed
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// ReplaceAll atomically replaces every remote list definition. Cached entries
// are dropped so the next refresh repopulates them from the source.
func (s *Store) ReplaceAll(ctx context.Context, lists []UpsertRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM remote_lists`); err != nil {
		return err
	}
	for _, list := range lists {
		enabled := true
		if list.Enabled != nil {
			enabled = *list.Enabled
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO remote_lists (name, url, kind, refresh_interval_seconds, enabled)
			VALUES (?, ?, ?, ?, ?)
		`, list.Name, list.URL, list.Kind, list.RefreshIntervalSeconds, boolToInt(enabled)); err != nil {
			return translateUniqueError(err, list.Name)
		}
	}
	return tx.Commit()
}
