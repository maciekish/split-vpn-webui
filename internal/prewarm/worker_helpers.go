package prewarm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func buildRunStats(progress Progress, cacheV4BySet, cacheV6BySet map[string]map[string]struct{}) RunStats {
	final := progress.Clone()
	return RunStats{
		DomainsTotal:  final.TotalDomains,
		DomainsDone:   final.ProcessedDomains,
		IPsInserted:   final.TotalIPs,
		Progress:      final,
		CacheSnapshot: materializeCacheSnapshot(cacheV4BySet, cacheV6BySet),
	}
}

func appendSetIPs(cache map[string]map[string]struct{}, setName string, ips []string) {
	if len(ips) == 0 || strings.TrimSpace(setName) == "" {
		return
	}
	existing := cache[setName]
	if existing == nil {
		existing = make(map[string]struct{}, len(ips))
		cache[setName] = existing
	}
	for _, ip := range ips {
		trimmed := strings.TrimSpace(ip)
		if trimmed == "" {
			continue
		}
		existing[trimmed] = struct{}{}
	}
}

func materializeCacheSnapshot(v4BySet, v6BySet map[string]map[string]struct{}) map[string]CachedSetValues {
	snapshot := make(map[string]CachedSetValues, len(v4BySet)+len(v6BySet))
	for setName, values := range v4BySet {
		entry := snapshot[setName]
		entry.V4 = mapKeysSorted(values)
		snapshot[setName] = entry
	}
	for setName, values := range v6BySet {
		entry := snapshot[setName]
		entry.V6 = mapKeysSorted(values)
		snapshot[setName] = entry
	}
	return snapshot
}

func (w *Worker) emitProgress(progress Progress) {
	if w.progress == nil {
		return
	}
	w.progress(progress.Clone())
}

func (w *Worker) emitQueryError(event QueryError) {
	if w.onError == nil || event.Err == nil {
		return
	}
	if errors.Is(event.Err, context.Canceled) {
		return
	}
	w.onError(event)
}

func resolverLabel(resolver DoHClient) string {
	switch typed := resolver.(type) {
	case *NameserverClient:
		return "dns://" + typed.serverAddr
	case *CloudflareDoHClient:
		name := hostOrURL(typed.baseURL)
		if subnet := strings.TrimSpace(typed.extraQuery["edns_client_subnet"]); subnet != "" {
			return fmt.Sprintf("%s ecs=%s", name, subnet)
		}
		return name
	default:
		return fmt.Sprintf("%T", resolver)
	}
}

func hostOrURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "resolver"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return trimmed
	}
	return parsed.Host
}
