package routing

import "context"

func (s *Store) listLegacyDomainsByGroup(ctx context.Context, groupID int64) ([]string, error) {
	legacyDomains, err := s.listLegacyDomainsForGroups(ctx)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), legacyDomains[groupID]...), nil
}

func (s *Store) listLegacyDomainsForGroups(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT group_id, domain
		FROM domain_entries
		ORDER BY group_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var groupID int64
		var domain string
		if err := rows.Scan(&groupID, &domain); err != nil {
			return nil, err
		}
		result[groupID] = append(result[groupID], domain)
	}
	return result, rows.Err()
}
