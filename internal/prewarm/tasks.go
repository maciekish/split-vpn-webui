package prewarm

import (
	"sort"
	"strings"

	"split-vpn-webui/internal/routing"
)

func buildTasks(groups []routing.DomainGroup) ([]domainTask, error) {
	tasks := make([]domainTask, 0)
	for _, group := range groups {
		for ruleIndex, rule := range group.Rules {
			sets := routing.RuleSetNames(group.Name, ruleIndex)
			for _, rawDomain := range rule.Domains {
				domain := normalizeDomain(rawDomain)
				if domain == "" {
					continue
				}
				tasks = append(tasks, domainTask{
					GroupName: group.Name,
					SetV4:     sets.DestinationV4,
					SetV6:     sets.DestinationV6,
					Domain:    domain,
					Wildcard:  false,
				})
			}
			for _, rawDomain := range rule.WildcardDomains {
				domain := normalizeDomain(rawDomain)
				if domain == "" {
					continue
				}
				tasks = append(tasks, domainTask{
					GroupName: group.Name,
					SetV4:     sets.DestinationV4,
					SetV6:     sets.DestinationV6,
					Domain:    domain,
					Wildcard:  true,
				})
			}
		}
		if len(group.Rules) > 0 {
			continue
		}
		setV4, setV6 := routing.GroupSetNames(group.Name)
		for _, rawDomain := range group.Domains {
			domain := normalizeDomain(rawDomain)
			if domain == "" {
				continue
			}
			tasks = append(tasks, domainTask{
				GroupName: group.Name,
				SetV4:     setV4,
				SetV6:     setV6,
				Domain:    domain,
				Wildcard:  strings.HasPrefix(strings.TrimSpace(strings.ToLower(rawDomain)), "*."),
			})
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].GroupName == tasks[j].GroupName {
			return tasks[i].Domain < tasks[j].Domain
		}
		return tasks[i].GroupName < tasks[j].GroupName
	})
	return tasks, nil
}

func mapKeysSorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
