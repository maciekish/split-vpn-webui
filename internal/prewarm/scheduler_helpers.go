package prewarm

import (
	"strings"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/settings"
)

func validateQuerySettings(current settings.Settings) error {
	if _, err := nameserversFromSettings(current); err != nil {
		return err
	}
	if _, err := ecsProfilesFromSettings(current); err != nil {
		return err
	}
	return nil
}

func nameserversFromSettings(current settings.Settings) ([]string, error) {
	return ParseNameserverLines(current.PrewarmExtraNameservers)
}

func ecsProfilesFromSettings(current settings.Settings) ([]string, error) {
	profiles, err := ParseECSProfiles(current.PrewarmECSProfiles)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profile.Subnet)
	}
	return out, nil
}

func (s *Scheduler) logInfof(format string, args ...any) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger != nil {
		logger.Infof(format, args...)
	}
}

func (s *Scheduler) logDebugf(format string, args ...any) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger != nil {
		logger.Debugf(format, args...)
	}
}

func (s *Scheduler) logWarnf(format string, args ...any) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger != nil {
		logger.Warnf(format, args...)
	}
}

func (s *Scheduler) logErrorf(format string, args ...any) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger != nil {
		logger.Errorf(format, args...)
	}
}

func lenOrZero(raw string) int {
	count := 0
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		count++
	}
	return count
}

func cacheSnapshotToResolverValues(snapshot map[string]CachedSetValues) map[string]routing.ResolverValues {
	out := make(map[string]routing.ResolverValues, len(snapshot))
	for setName, values := range snapshot {
		out[setName] = routing.ResolverValues{
			V4: append([]string(nil), values.V4...),
			V6: append([]string(nil), values.V6...),
		}
	}
	return out
}

func cloneStoredRunRecord(run *RunRecord) *RunRecord {
	if run == nil {
		return nil
	}
	cloned := *run
	return &cloned
}

func configuredParallelism(current settings.Settings) int {
	if current.PrewarmParallelism <= 0 {
		return defaultParallelism
	}
	if current.PrewarmParallelism > maxParallelism {
		return maxParallelism
	}
	return current.PrewarmParallelism
}

func configuredTimeout(current settings.Settings) time.Duration {
	seconds := current.PrewarmDoHTimeoutSeconds
	if seconds <= 0 {
		seconds = defaultTimeoutSeconds
	}
	if seconds > maxTimeoutSeconds {
		seconds = maxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func configuredInterval(current settings.Settings) time.Duration {
	seconds := current.PrewarmIntervalSeconds
	if seconds <= 0 {
		seconds = defaultIntervalSeconds
	}
	if seconds > maxIntervalSeconds {
		seconds = maxIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (s *Scheduler) mergeStatsWithCurrentProgress(started time.Time, stats RunStats) RunStats {
	if stats.Progress.TotalDomains > 0 {
		return stats
	}
	s.mu.RLock()
	current := s.progress
	s.mu.RUnlock()
	if current == nil || current.TotalDomains == 0 {
		if stats.Progress.PerVPN == nil {
			stats.Progress = Progress{
				StartedAt: started.Unix(),
				PerVPN:    map[string]VPNProgress{},
			}
		}
		return stats
	}
	cloned := current.Clone()
	if cloned.StartedAt == 0 {
		cloned.StartedAt = started.Unix()
	}
	stats.Progress = cloned
	stats.DomainsTotal = cloned.TotalDomains
	stats.DomainsDone = cloned.ProcessedDomains
	stats.IPsInserted = cloned.TotalIPs
	return stats
}
