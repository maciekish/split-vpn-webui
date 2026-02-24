package prewarm

// Logger captures optional diagnostics logging used by pre-warm scheduler.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

func progressErrorCount(progress Progress) int {
	total := 0
	for _, item := range progress.PerVPN {
		total += item.Errors
	}
	return total
}
