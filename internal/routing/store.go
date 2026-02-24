package routing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Store persists routing groups and resolver cache rows in SQLite.
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

// Create inserts a group and all nested rule selectors.
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

	if err := replaceRulesTx(ctx, tx, groupID, normalized.Rules); err != nil {
		return nil, err
	}
	if err := replaceLegacyDomainsTx(ctx, tx, groupID, normalized.Domains); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, groupID)
}

// Update overwrites a group row and nested rule selectors.
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

	if err := replaceRulesTx(ctx, tx, id, normalized.Rules); err != nil {
		return nil, err
	}
	if err := replaceLegacyDomainsTx(ctx, tx, id, normalized.Domains); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete removes a group and all dependent rows.
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

	rules, err := s.listRulesByGroup(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		legacyDomains, legacyErr := s.listLegacyDomainsByGroup(ctx, group.ID)
		if legacyErr != nil {
			return nil, legacyErr
		}
		if len(legacyDomains) > 0 {
			rules = []RoutingRule{{Name: "Rule 1", Domains: legacyDomains}}
		}
	}
	group.Rules = rules
	group.Domains = legacyDomainsFromRules(rules)
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
	groupIDs := make([]int64, 0)
	for rows.Next() {
		var group DomainGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.EgressVPN, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, group)
		groupIDs = append(groupIDs, group.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return groups, nil
	}

	rulesByGroup, err := s.listRulesForGroups(ctx)
	if err != nil {
		return nil, err
	}
	legacyDomainsByGroup, err := s.listLegacyDomainsForGroups(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		rules := append([]RoutingRule(nil), rulesByGroup[groups[i].ID]...)
		if len(rules) == 0 && len(legacyDomainsByGroup[groups[i].ID]) > 0 {
			rules = []RoutingRule{{Name: "Rule 1", Domains: append([]string(nil), legacyDomainsByGroup[groups[i].ID]...)}}
		}
		groups[i].Rules = rules
		groups[i].Domains = legacyDomainsFromRules(rules)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

// ReplaceAll atomically replaces all persisted groups and resolver cache rows.
func (s *Store) ReplaceAll(
	ctx context.Context,
	groups []DomainGroup,
	snapshot map[ResolverSelector]ResolverValues,
) error {
	normalizedGroups := make([]DomainGroup, 0, len(groups))
	for _, group := range groups {
		normalized, err := NormalizeAndValidate(group)
		if err != nil {
			return err
		}
		normalizedGroups = append(normalizedGroups, normalized)
	}
	sort.Slice(normalizedGroups, func(i, j int) bool { return normalizedGroups[i].Name < normalizedGroups[j].Name })

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM domain_groups`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resolver_cache`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM prewarm_cache`); err != nil {
		return err
	}

	for _, group := range normalizedGroups {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO domain_groups (name, egress_vpn)
			VALUES (?, ?)
		`, group.Name, group.EgressVPN)
		if err != nil {
			return err
		}
		groupID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err := replaceRulesTx(ctx, tx, groupID, group.Rules); err != nil {
			return err
		}
		if err := replaceLegacyDomainsTx(ctx, tx, groupID, group.Domains); err != nil {
			return err
		}
	}
	if err := upsertResolverSnapshotTx(ctx, tx, snapshot); err != nil {
		return err
	}

	return tx.Commit()
}

func replaceRulesTx(ctx context.Context, tx *sql.Tx, groupID int64, rules []RoutingRule) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM routing_rules WHERE group_id = ?`, groupID); err != nil {
		return err
	}

	for idx, rule := range rules {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO routing_rules (group_id, name, position)
			VALUES (?, ?, ?)
		`, groupID, rule.Name, idx)
		if err != nil {
			return err
		}
		ruleID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		for _, cidr := range rule.SourceCIDRs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_source_cidrs (rule_id, cidr) VALUES (?, ?)
			`, ruleID, cidr); err != nil {
				return err
			}
		}
		for _, iface := range rule.SourceInterfaces {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_source_interfaces (rule_id, iface) VALUES (?, ?)
			`, ruleID, iface); err != nil {
				return err
			}
		}
		for _, mac := range rule.SourceMACs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_source_macs (rule_id, mac) VALUES (?, ?)
			`, ruleID, mac); err != nil {
				return err
			}
		}
		for _, cidr := range rule.DestinationCIDRs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_destination_cidrs (rule_id, cidr) VALUES (?, ?)
			`, ruleID, cidr); err != nil {
				return err
			}
		}
		for _, port := range rule.DestinationPorts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_ports (rule_id, protocol, start_port, end_port)
				VALUES (?, ?, ?, ?)
			`, ruleID, port.Protocol, port.Start, port.End); err != nil {
				return err
			}
		}
		for _, asn := range rule.DestinationASNs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_asns (rule_id, asn) VALUES (?, ?)
			`, ruleID, asn); err != nil {
				return err
			}
		}
		for _, domain := range rule.Domains {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_domains (rule_id, domain, is_wildcard)
				VALUES (?, ?, 0)
			`, ruleID, domain); err != nil {
				return err
			}
		}
		for _, wildcard := range rule.WildcardDomains {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_domains (rule_id, domain, is_wildcard)
				VALUES (?, ?, 1)
			`, ruleID, wildcard); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceLegacyDomainsTx(ctx context.Context, tx *sql.Tx, groupID int64, domains []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM domain_entries WHERE group_id = ?`, groupID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "*.")
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO domain_entries (group_id, domain)
			VALUES (?, ?)
		`, groupID, trimmed); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listRulesByGroup(ctx context.Context, groupID int64) ([]RoutingRule, error) {
	rulesByGroup, err := s.listRulesForGroups(ctx)
	if err != nil {
		return nil, err
	}
	return append([]RoutingRule(nil), rulesByGroup[groupID]...), nil
}

func (s *Store) listRulesForGroups(ctx context.Context) (map[int64][]RoutingRule, error) {
	rulesByGroup := make(map[int64][]RoutingRule)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, name, position
		FROM routing_rules
		ORDER BY group_id ASC, position ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type storedRule struct {
		groupID int64
		ruleID  int64
		rule    RoutingRule
	}
	stored := make([]storedRule, 0)
	ruleIDs := make([]int64, 0)
	for rows.Next() {
		var entry storedRule
		var position int
		if err := rows.Scan(&entry.ruleID, &entry.groupID, &entry.rule.Name, &position); err != nil {
			return nil, err
		}
		entry.rule.ID = entry.ruleID
		stored = append(stored, entry)
		ruleIDs = append(ruleIDs, entry.ruleID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return rulesByGroup, nil
	}

	sourceByRule, err := listRuleCIDRs(ctx, s.db, "routing_rule_source_cidrs", ruleIDs)
	if err != nil {
		return nil, err
	}
	sourceInterfacesByRule, err := listRuleSourceInterfaces(ctx, s.db, ruleIDs)
	if err != nil {
		return nil, err
	}
	sourceMACsByRule, err := listRuleSourceMACs(ctx, s.db, ruleIDs)
	if err != nil {
		return nil, err
	}
	destByRule, err := listRuleCIDRs(ctx, s.db, "routing_rule_destination_cidrs", ruleIDs)
	if err != nil {
		return nil, err
	}
	portsByRule, err := listRulePorts(ctx, s.db, ruleIDs)
	if err != nil {
		return nil, err
	}
	asnByRule, err := listRuleASNs(ctx, s.db, ruleIDs)
	if err != nil {
		return nil, err
	}
	domainsByRule, wildcardsByRule, err := listRuleDomains(ctx, s.db, ruleIDs)
	if err != nil {
		return nil, err
	}

	for _, entry := range stored {
		rule := entry.rule
		rule.SourceInterfaces = append([]string(nil), sourceInterfacesByRule[entry.ruleID]...)
		rule.SourceCIDRs = append([]string(nil), sourceByRule[entry.ruleID]...)
		rule.SourceMACs = append([]string(nil), sourceMACsByRule[entry.ruleID]...)
		rule.DestinationCIDRs = append([]string(nil), destByRule[entry.ruleID]...)
		rule.DestinationPorts = append([]PortRange(nil), portsByRule[entry.ruleID]...)
		rule.DestinationASNs = append([]string(nil), asnByRule[entry.ruleID]...)
		rule.Domains = append([]string(nil), domainsByRule[entry.ruleID]...)
		rule.WildcardDomains = append([]string(nil), wildcardsByRule[entry.ruleID]...)
		rulesByGroup[entry.groupID] = append(rulesByGroup[entry.groupID], rule)
	}
	return rulesByGroup, nil
}
