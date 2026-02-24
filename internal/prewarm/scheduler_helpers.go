package prewarm

import (
	"strings"

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
