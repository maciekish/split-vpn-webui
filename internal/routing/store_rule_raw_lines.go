package routing

import (
	"context"
	"database/sql"
)

const (
	selectorSourceInterfaces = "source_interfaces"
	selectorSourceCIDRs      = "source_cidrs"
	selectorSourceMACs       = "source_macs"
	selectorDestinationCIDRs = "destination_cidrs"
	selectorDestinationPorts = "destination_ports"
	selectorDestinationASNs  = "destination_asns"
	selectorDomains          = "domains"
	selectorWildcardDomains  = "wildcard_domains"
)

func insertRuleRawSelectorsTx(ctx context.Context, tx *sql.Tx, ruleID int64, raw *RuleRawSelectors) error {
	normalized := normalizeRuleRawSelectors(raw)
	linesBySelector := map[string][]string{
		selectorSourceInterfaces: normalized.SourceInterfaces,
		selectorSourceCIDRs:      normalized.SourceCIDRs,
		selectorSourceMACs:       normalized.SourceMACs,
		selectorDestinationCIDRs: normalized.DestinationCIDRs,
		selectorDestinationPorts: normalized.DestinationPorts,
		selectorDestinationASNs:  normalized.DestinationASNs,
		selectorDomains:          normalized.Domains,
		selectorWildcardDomains:  normalized.WildcardDomains,
	}
	for selector, lines := range linesBySelector {
		for position, line := range lines {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_selector_lines (rule_id, selector, line, position)
				VALUES (?, ?, ?, ?)
			`, ruleID, selector, line, position); err != nil {
				return err
			}
		}
	}
	return nil
}

func listRuleRawSelectors(ctx context.Context, db *sql.DB, ruleIDs []int64) (map[int64]RuleRawSelectors, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, selector, line
		FROM routing_rule_selector_lines
		ORDER BY rule_id ASC, selector ASC, position ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]RuleRawSelectors, len(ruleIDs))
	for rows.Next() {
		var ruleID int64
		var selector string
		var line string
		if err := rows.Scan(&ruleID, &selector, &line); err != nil {
			return nil, err
		}
		raw := result[ruleID]
		switch selector {
		case selectorSourceInterfaces:
			raw.SourceInterfaces = append(raw.SourceInterfaces, line)
		case selectorSourceCIDRs:
			raw.SourceCIDRs = append(raw.SourceCIDRs, line)
		case selectorSourceMACs:
			raw.SourceMACs = append(raw.SourceMACs, line)
		case selectorDestinationCIDRs:
			raw.DestinationCIDRs = append(raw.DestinationCIDRs, line)
		case selectorDestinationPorts:
			raw.DestinationPorts = append(raw.DestinationPorts, line)
		case selectorDestinationASNs:
			raw.DestinationASNs = append(raw.DestinationASNs, line)
		case selectorDomains:
			raw.Domains = append(raw.Domains, line)
		case selectorWildcardDomains:
			raw.WildcardDomains = append(raw.WildcardDomains, line)
		}
		result[ruleID] = raw
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
