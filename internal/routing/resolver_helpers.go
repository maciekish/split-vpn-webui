package routing

import (
	"sort"
	"strings"
	"time"

	"split-vpn-webui/internal/settings"
)

func collectResolverJobs(groups []DomainGroup) []resolverJob {
	seen := make(map[ResolverSelector]struct{})
	jobs := make([]resolverJob, 0)
	for _, group := range groups {
		for _, rule := range group.Rules {
			for _, domain := range rule.Domains {
				selector := ResolverSelector{Type: "domain", Key: domain}
				if _, exists := seen[selector]; exists {
					continue
				}
				seen[selector] = struct{}{}
				jobs = append(jobs, resolverJob{Selector: selector, Label: "domain:" + domain})
			}
			for _, wildcard := range rule.WildcardDomains {
				selector := ResolverSelector{Type: "wildcard", Key: wildcard}
				if _, exists := seen[selector]; exists {
					continue
				}
				seen[selector] = struct{}{}
				jobs = append(jobs, resolverJob{Selector: selector, Label: "wildcard:" + wildcard})
			}
			for _, asn := range rule.DestinationASNs {
				selector := ResolverSelector{Type: "asn", Key: normalizeASNKey(asn)}
				if selector.Key == "" {
					continue
				}
				if _, exists := seen[selector]; exists {
					continue
				}
				seen[selector] = struct{}{}
				jobs = append(jobs, resolverJob{Selector: selector, Label: "asn:" + selector.Key})
			}
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Selector.Type == jobs[j].Selector.Type {
			return jobs[i].Selector.Key < jobs[j].Selector.Key
		}
		return jobs[i].Selector.Type < jobs[j].Selector.Type
	})
	return jobs
}

func cloneResolverRun(run *ResolverRunRecord) *ResolverRunRecord {
	if run == nil {
		return nil
	}
	copied := *run
	return &copied
}

func (s *ResolverScheduler) currentInterval() time.Duration {
	current, err := s.settings.Get()
	if err != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.defaultInterval
	}
	interval := resolverIntervalFromSettings(current)
	s.mu.Lock()
	s.defaultInterval = interval
	s.mu.Unlock()
	return interval
}

func (s *ResolverScheduler) emitProgress(progress ResolverProgress) {
	s.mu.RLock()
	handler := s.progressHandler
	s.mu.RUnlock()
	if handler != nil {
		handler(progress)
	}
}

func resolverIntervalFromSettings(current settings.Settings) time.Duration {
	seconds := current.ResolverIntervalSeconds
	if seconds <= 0 {
		seconds = defaultResolverIntervalSeconds
	}
	if seconds > maxResolverIntervalSeconds {
		seconds = maxResolverIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

func resolverTimeoutFromSettings(current settings.Settings) time.Duration {
	seconds := current.ResolverTimeoutSeconds
	if seconds <= 0 {
		seconds = defaultResolverTimeoutSeconds
	}
	if seconds > maxResolverTimeoutSeconds {
		seconds = maxResolverTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func resolverParallelismFromSettings(current settings.Settings) int {
	value := current.ResolverParallelism
	if value <= 0 {
		value = defaultResolverParallelism
	}
	if value > maxResolverParallelism {
		value = maxResolverParallelism
	}
	return value
}

func normalizeASNKey(value string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	trimmed = strings.TrimPrefix(trimmed, "AS")
	if trimmed == "" {
		return ""
	}
	for _, char := range trimmed {
		if char < '0' || char > '9' {
			return ""
		}
	}
	trimmed = strings.TrimLeft(trimmed, "0")
	if trimmed == "" {
		return ""
	}
	return "AS" + trimmed
}

func mapKeysSorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
