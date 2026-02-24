package routing

import (
	"context"
	"database/sql"
	"strings"
)

func replaceRulesTx(ctx context.Context, tx *sql.Tx, groupID int64, rules []RoutingRule) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM routing_rules WHERE group_id = ?`, groupID); err != nil {
		return err
	}

	for idx, rule := range rules {
		excludeMulticast := true
		if rule.ExcludeMulticast != nil {
			excludeMulticast = *rule.ExcludeMulticast
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO routing_rules (group_id, name, position, exclude_multicast)
			VALUES (?, ?, ?, ?)
		`, groupID, rule.Name, idx, boolToInt(excludeMulticast))
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
		for _, cidr := range rule.ExcludedSourceCIDRs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_excluded_source_cidrs (rule_id, cidr) VALUES (?, ?)
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
		for _, cidr := range rule.ExcludedDestinationCIDRs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_excluded_destination_cidrs (rule_id, cidr) VALUES (?, ?)
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
		for _, port := range rule.ExcludedDestinationPorts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_excluded_ports (rule_id, protocol, start_port, end_port)
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
		for _, asn := range rule.ExcludedDestinationASNs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routing_rule_excluded_asns (rule_id, asn) VALUES (?, ?)
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
		if err := insertRuleRawSelectorsTx(ctx, tx, ruleID, rule.RawSelectors); err != nil {
			return err
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

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
