package routing

import (
	"context"
	"database/sql"
	"fmt"
)

func listRuleCIDRs(ctx context.Context, db *sql.DB, table string, ruleIDs []int64) (map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT rule_id, cidr FROM %s ORDER BY rule_id ASC, id ASC`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var ruleID int64
		var cidr string
		if err := rows.Scan(&ruleID, &cidr); err != nil {
			return nil, err
		}
		result[ruleID] = append(result[ruleID], cidr)
	}
	return result, rows.Err()
}

func listRuleSourceInterfaces(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, iface
		FROM routing_rule_source_interfaces
		ORDER BY rule_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var ruleID int64
		var iface string
		if err := rows.Scan(&ruleID, &iface); err != nil {
			return nil, err
		}
		result[ruleID] = append(result[ruleID], iface)
	}
	return result, rows.Err()
}

func listRuleSourceMACs(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, mac
		FROM routing_rule_source_macs
		ORDER BY rule_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var ruleID int64
		var mac string
		if err := rows.Scan(&ruleID, &mac); err != nil {
			return nil, err
		}
		result[ruleID] = append(result[ruleID], mac)
	}
	return result, rows.Err()
}

func listRulePorts(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64][]PortRange, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, protocol, start_port, end_port
		FROM routing_rule_ports
		ORDER BY rule_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]PortRange)
	for rows.Next() {
		var ruleID int64
		var protocol string
		var start int
		var end int
		if err := rows.Scan(&ruleID, &protocol, &start, &end); err != nil {
			return nil, err
		}
		result[ruleID] = append(result[ruleID], PortRange{Protocol: protocol, Start: start, End: end})
	}
	return result, rows.Err()
}

func listRuleASNs(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, asn
		FROM routing_rule_asns
		ORDER BY rule_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var ruleID int64
		var asn string
		if err := rows.Scan(&ruleID, &asn); err != nil {
			return nil, err
		}
		result[ruleID] = append(result[ruleID], asn)
	}
	return result, rows.Err()
}

func listRuleDomains(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64][]string, map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, domain, is_wildcard
		FROM routing_rule_domains
		ORDER BY rule_id ASC, id ASC
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	domains := make(map[int64][]string)
	wildcards := make(map[int64][]string)
	for rows.Next() {
		var ruleID int64
		var domain string
		var isWildcard int
		if err := rows.Scan(&ruleID, &domain, &isWildcard); err != nil {
			return nil, nil, err
		}
		if isWildcard == 1 {
			wildcards[ruleID] = append(wildcards[ruleID], domain)
		} else {
			domains[ruleID] = append(domains[ruleID], domain)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return domains, wildcards, nil
}
