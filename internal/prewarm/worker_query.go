package prewarm

import (
	"context"
	"sync"
)

// resilientQuery runs a single DNS query up to w.attempts times. Each attempt is
// bounded by its own w.timeout deadline; on success it returns immediately, and
// after the final failed attempt it returns the last error so the caller can log
// it and move on. A cancelled parent context aborts the retries at once.
func (w *Worker) resilientQuery(ctx context.Context, fn func(context.Context) ([]string, error)) ([]string, error) {
	return w.retryQuery(ctx, w.attempts, fn)
}

func (w *Worker) retryQuery(ctx context.Context, attempts int, fn func(context.Context) ([]string, error)) ([]string, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, w.timeout)
		values, err := fn(attemptCtx)
		cancel()
		if err == nil {
			return values, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (w *Worker) resolveWildcard(ctx context.Context, wildcard string) ([]string, error) {
	// crt.sh is slow and rate-limits aggressively; use a single time-bounded
	// attempt rather than the per-query retry budget so we don't amplify its
	// latency or trigger 429s. A transient miss just falls back to the base
	// domain and is retried on the next scheduled run.
	return w.retryQuery(ctx, 1, func(attemptCtx context.Context) ([]string, error) {
		return w.wildcard.Resolve(attemptCtx, wildcard)
	})
}

// resolverEnabled reports whether a resolver is still worth querying this run.
func (w *Worker) resolverEnabled(idx int) bool {
	return !w.gates[idx].disabled.Load()
}

// resolverAttempts returns the retry budget for a resolver. A resolver that has
// already failed drops to a single attempt so a dead resolver stops burning the
// full retry budget on every query; a healthy one keeps the full budget for
// genuine transient errors.
func (w *Worker) resolverAttempts(idx int) int {
	if w.gates[idx].failures.Load() > 0 {
		return 1
	}
	return w.attempts
}

// noteResolverResult records a query outcome and disables a resolver once it has
// failed disableThreshold times in a row.
func (w *Worker) noteResolverResult(idx int, ok bool) {
	gate := w.gates[idx]
	if ok {
		gate.failures.Store(0)
		return
	}
	failures := gate.failures.Add(1)
	if int(failures) >= w.disableThreshold && gate.disabled.CompareAndSwap(false, true) {
		if w.onResolverOff != nil {
			w.onResolverOff(gate.label, int(failures))
		}
	}
}

// acquireQuerySlot blocks until a query slot is free or the context is done.
func acquireQuerySlot(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseQuerySlot(sem chan struct{}) { <-sem }

// processTask pre-warms one domain across every active interface and resolver.
// Queries fan out concurrently, gated by the run-wide querySem, so no single
// task (notably a wildcard that discovers many subdomains) can monopolize a
// worker and stall the run.
func (w *Worker) processTask(ctx context.Context, task domainTask, ifaces []string, querySem chan struct{}) (taskResult, error) {
	var mu sync.Mutex
	targets := map[string]struct{}{task.Domain: {}}
	perVPNDomains := make(map[string]int, len(ifaces))
	perVPNErrors := make(map[string]int, len(ifaces))
	perVPNIPs := make(map[string]int, len(ifaces))
	perIfaceV4 := make(map[string]map[string]struct{}, len(ifaces))
	perIfaceV6 := make(map[string]map[string]struct{}, len(ifaces))
	for _, iface := range ifaces {
		perVPNDomains[iface] = 1
		perIfaceV4[iface] = make(map[string]struct{})
		perIfaceV6[iface] = make(map[string]struct{})
	}

	// Phase 1: wildcard discovery expands the target set (bounded + retried).
	if task.Wildcard && w.wildcard != nil {
		discovered, err := w.resolveWildcard(ctx, "*."+task.Domain)
		if err != nil {
			w.emitQueryError(QueryError{
				Stage:     "wildcard-discovery",
				Domain:    task.Domain,
				Interface: "*",
				Resolver:  "crt.sh",
				Err:       err,
			})
			for _, iface := range ifaces {
				perVPNErrors[iface]++
			}
		} else {
			for _, subdomain := range discovered {
				if target := normalizeDomain(subdomain); target != "" {
					targets[target] = struct{}{}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return taskResult{}, err
	}

	// Phase 2: CNAME expansion, one concurrent query per interface × resolver.
	var cnameWG sync.WaitGroup
	for _, iface := range ifaces {
		for idx, resolver := range w.resolvers {
			if !w.resolverEnabled(idx) {
				continue
			}
			if err := acquireQuerySlot(ctx, querySem); err != nil {
				cnameWG.Wait()
				return taskResult{}, err
			}
			cnameWG.Add(1)
			go func(iface string, idx int, resolver DoHClient) {
				defer cnameWG.Done()
				defer releaseQuerySlot(querySem)
				cnames, err := w.retryQuery(ctx, w.resolverAttempts(idx), func(attemptCtx context.Context) ([]string, error) {
					return resolver.QueryCNAME(attemptCtx, task.Domain, iface)
				})
				w.noteResolverResult(idx, err == nil)
				if err != nil {
					w.emitQueryError(QueryError{
						Stage:     "cname",
						Domain:    task.Domain,
						Interface: iface,
						Resolver:  w.gates[idx].label,
						Err:       err,
					})
					mu.Lock()
					perVPNErrors[iface]++
					mu.Unlock()
					return
				}
				mu.Lock()
				for _, cname := range cnames {
					if target := normalizeDomain(cname); target != "" {
						targets[target] = struct{}{}
					}
				}
				mu.Unlock()
			}(iface, idx, resolver)
		}
	}
	cnameWG.Wait()
	if err := ctx.Err(); err != nil {
		return taskResult{}, err
	}

	allV4 := make(map[string]struct{})
	allV6 := make(map[string]struct{})
	targetList := mapKeysSorted(targets)

	// Phase 3: A/AAAA resolution, one concurrent unit per target × interface × resolver.
	var addrWG sync.WaitGroup
	for _, target := range targetList {
		for _, iface := range ifaces {
			for idx, resolver := range w.resolvers {
				if !w.resolverEnabled(idx) {
					continue
				}
				if err := acquireQuerySlot(ctx, querySem); err != nil {
					addrWG.Wait()
					return taskResult{}, err
				}
				addrWG.Add(1)
				go func(target, iface string, idx int, resolver DoHClient) {
					defer addrWG.Done()
					defer releaseQuerySlot(querySem)
					resolverName := w.gates[idx].label

					v4, err := w.retryQuery(ctx, w.resolverAttempts(idx), func(attemptCtx context.Context) ([]string, error) {
						return resolver.QueryA(attemptCtx, target, iface)
					})
					w.noteResolverResult(idx, err == nil)
					if err != nil {
						w.emitQueryError(QueryError{Stage: "a", Domain: target, Interface: iface, Resolver: resolverName, Err: err})
						mu.Lock()
						perVPNErrors[iface]++
						mu.Unlock()
					} else {
						mu.Lock()
						for _, ip := range v4 {
							allV4[ip] = struct{}{}
							perIfaceV4[iface][ip] = struct{}{}
						}
						mu.Unlock()
					}

					if !w.resolverEnabled(idx) {
						return
					}
					v6, err := w.retryQuery(ctx, w.resolverAttempts(idx), func(attemptCtx context.Context) ([]string, error) {
						return resolver.QueryAAAA(attemptCtx, target, iface)
					})
					w.noteResolverResult(idx, err == nil)
					if err != nil {
						w.emitQueryError(QueryError{Stage: "aaaa", Domain: target, Interface: iface, Resolver: resolverName, Err: err})
						mu.Lock()
						perVPNErrors[iface]++
						mu.Unlock()
					} else {
						mu.Lock()
						for _, ip := range v6 {
							allV6[ip] = struct{}{}
							perIfaceV6[iface][ip] = struct{}{}
						}
						mu.Unlock()
					}
				}(target, iface, idx, resolver)
			}
		}
	}
	addrWG.Wait()
	if err := ctx.Err(); err != nil {
		return taskResult{}, err
	}

	v4List := mapKeysSorted(allV4)
	v6List := mapKeysSorted(allV6)
	for _, iface := range ifaces {
		perVPNIPs[iface] = len(perIfaceV4[iface]) + len(perIfaceV6[iface])
	}
	return taskResult{
		Inserted:      len(v4List) + len(v6List),
		PerVPNIPs:     perVPNIPs,
		PerVPNErrors:  perVPNErrors,
		PerVPNDomains: perVPNDomains,
		V4:            v4List,
		V6:            v6List,
	}, nil
}
