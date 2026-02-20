package routing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Store persists domain groups in SQLite.
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

// Create inserts a domain group and its domain entries.
func (s *Store) Create(ctx context.Context, group DomainGroup) (*DomainGroup, error) {
	normalized, err := NormalizeAndValidate(group)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO domain_groups (name, egress_vpn)
		VALUES (?, ?)
	`, normalized.Name, normalized.EgressVPN)
	if err != nil {
		return nil, err
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := insertDomains(ctx, tx, groupID, normalized.Domains); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, groupID)
}

// Update overwrites a group row and its domain entries.
func (s *Store) Update(ctx context.Context, id int64, group DomainGroup) (*DomainGroup, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: invalid group id", ErrGroupValidation)
	}
	normalized, err := NormalizeAndValidate(group)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE domain_groups
		SET name = ?, egress_vpn = ?, updated_at = strftime('%s','now')
		WHERE id = ?
	`, normalized.Name, normalized.EgressVPN, id)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrGroupNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM domain_entries WHERE group_id = ?`, id); err != nil {
		return nil, err
	}
	if err := insertDomains(ctx, tx, id, normalized.Domains); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete removes a group and its domain entries.
func (s *Store) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: invalid group id", ErrGroupValidation)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM domain_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// Get returns a single group by id.
func (s *Store) Get(ctx context.Context, id int64) (*DomainGroup, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: invalid group id", ErrGroupValidation)
	}
	var group DomainGroup
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, egress_vpn, created_at, updated_at
		FROM domain_groups
		WHERE id = ?
	`, id)
	if err := row.Scan(&group.ID, &group.Name, &group.EgressVPN, &group.CreatedAt, &group.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	domains, err := s.listDomainsByGroup(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	group.Domains = domains
	return &group, nil
}

// List returns all groups ordered by name.
func (s *Store) List(ctx context.Context) ([]DomainGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, egress_vpn, created_at, updated_at
		FROM domain_groups
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]DomainGroup, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var group DomainGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.EgressVPN, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, group)
		ids = append(ids, group.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return groups, nil
	}

	domainsByID, err := s.listDomainsForGroups(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].Domains = domainsByID[groups[i].ID]
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

func insertDomains(ctx context.Context, tx *sql.Tx, groupID int64, domains []string) error {
	for _, domain := range domains {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO domain_entries (group_id, domain)
			VALUES (?, ?)
		`, groupID, domain); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listDomainsByGroup(ctx context.Context, groupID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT domain FROM domain_entries WHERE group_id = ? ORDER BY domain ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	domains := make([]string, 0)
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, rows.Err()
}

func (s *Store) listDomainsForGroups(ctx context.Context, groupIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string, len(groupIDs))
	rows, err := s.db.QueryContext(ctx, `
		SELECT group_id, domain
		FROM domain_entries
		ORDER BY group_id ASC, domain ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var groupID int64
		var domain string
		if err := rows.Scan(&groupID, &domain); err != nil {
			return nil, err
		}
		result[groupID] = append(result[groupID], domain)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range groupIDs {
		if _, ok := result[id]; !ok {
			result[id] = []string{}
		}
	}
	return result, nil
}
