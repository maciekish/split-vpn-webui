package prewarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/util"
	"split-vpn-webui/internal/vpn"
)

const (
	defaultParallelism              = 4
	maxSafeWorkerParallelism        = 64
	defaultWorkerAttempts           = 3
	maxSafeWorkerAttempts           = 10
	defaultWorkerQueryTimeout       = 10 * time.Second
	defaultResolverFailureThreshold = 10
)

// resolverGate tracks the health of one resolver during a run. After enough
// consecutive failures it is disabled so the pre-warmer stops wasting time on a
// resolver that is unreachable over the active VPN interfaces (e.g. an ISP
// nameserver that only answers its own customers).
type resolverGate struct {
	label    string
	failures atomic.Int32
	disabled atomic.Bool
}

// GroupSource lists configured domain routing groups.
type GroupSource interface {
	ListGroups(ctx context.Context) ([]routing.DomainGroup, error)
}

// VPNSource lists configured VPN profiles.
type VPNSource interface {
	List() ([]*vpn.VPNProfile, error)
}

// WildcardResolver discovers known subdomains for wildcard patterns.
type WildcardResolver interface {
	Resolve(ctx context.Context, wildcard string) ([]string, error)
}

// WorkerOptions controls worker runtime behavior.
type WorkerOptions struct {
	Parallelism              int
	Timeout                  time.Duration
	Attempts                 int
	ResolverFailureThreshold int
	ExtraNameservers         []string
	ECSProfiles              []string
	AdditionalResolvers      []DoHClient
	ProgressCallback         func(Progress)
	ErrorCallback            func(QueryError)
	ResolverDisabledCallback func(label string, failures int)
	InterfaceActive          func(name string) (bool, error)
	InterfaceList            func() ([]string, error)
	WildcardResolver         WildcardResolver
}

// Worker executes one DNS pre-warm pass.
type Worker struct {
	groups           GroupSource
	vpns             VPNSource
	doh              DoHClient
	ipset            routing.IPSetOperator
	resolvers        []DoHClient
	gates            []*resolverGate
	disableThreshold int
	parallel         int
	attempts         int
	timeout          time.Duration
	progress         func(Progress)
	onError          func(QueryError)
	onResolverOff    func(label string, failures int)
	ifaceUp          func(name string) (bool, error)
	ifaceList        func() ([]string, error)
	wildcard         WildcardResolver
}

type domainTask struct {
	GroupName string
	SetV4     string
	SetV6     string
	Domain    string
	Wildcard  bool
}

type taskResult struct {
	Inserted      int
	PerVPNIPs     map[string]int
	PerVPNErrors  map[string]int
	PerVPNDomains map[string]int
	V4            []string
	V6            []string
}

// NewWorker builds a pre-warm worker from dependencies.
func NewWorker(groups GroupSource, vpns VPNSource, doh DoHClient, ipset routing.IPSetOperator, opts WorkerOptions) (*Worker, error) {
	if groups == nil {
		return nil, fmt.Errorf("group source is required")
	}
	if vpns == nil {
		return nil, fmt.Errorf("vpn source is required")
	}
	if doh == nil {
		return nil, fmt.Errorf("doh client is required")
	}
	if ipset == nil {
		return nil, fmt.Errorf("ipset operator is required")
	}
	resolvers, err := buildQueryResolvers(doh, opts)
	if err != nil {
		return nil, err
	}
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	if parallelism > maxSafeWorkerParallelism {
		parallelism = maxSafeWorkerParallelism
	}
	attempts := opts.Attempts
	if attempts <= 0 {
		attempts = defaultWorkerAttempts
	}
	if attempts > maxSafeWorkerAttempts {
		attempts = maxSafeWorkerAttempts
	}
	queryTimeout := opts.Timeout
	if queryTimeout <= 0 {
		queryTimeout = defaultWorkerQueryTimeout
	}
	threshold := opts.ResolverFailureThreshold
	if threshold <= 0 {
		threshold = defaultResolverFailureThreshold
	}
	gates := make([]*resolverGate, len(resolvers))
	for i, resolver := range resolvers {
		gates[i] = &resolverGate{label: resolverLabel(resolver)}
	}
	ifaceActive := opts.InterfaceActive
	if ifaceActive == nil {
		ifaceActive = func(name string) (bool, error) {
			up, _, err := util.InterfaceOperState(name)
			return up, err
		}
	}
	ifaceList := opts.InterfaceList
	if ifaceList == nil {
		ifaceList = listInterfaceNames
	}
	wildcard := opts.WildcardResolver
	if wildcard == nil {
		wildcard = newCRTSHWildcardResolver(defaultDoHTimeout)
	}
	return &Worker{
		groups:           groups,
		vpns:             vpns,
		doh:              doh,
		ipset:            ipset,
		resolvers:        resolvers,
		gates:            gates,
		disableThreshold: threshold,
		parallel:         parallelism,
		attempts:         attempts,
		timeout:          queryTimeout,
		progress:         opts.ProgressCallback,
		onError:          opts.ErrorCallback,
		onResolverOff:    opts.ResolverDisabledCallback,
		ifaceUp:          ifaceActive,
		ifaceList:        ifaceList,
		wildcard:         wildcard,
	}, nil
}

// Run executes a single pre-warm pass.
func (w *Worker) Run(ctx context.Context) (RunStats, error) {
	if err := ctx.Err(); err != nil {
		return RunStats{}, err
	}
	groups, err := w.groups.ListGroups(ctx)
	if err != nil {
		return RunStats{}, err
	}
	tasks, err := buildTasks(groups)
	if err != nil {
		return RunStats{}, err
	}
	ifaces, err := w.activeInterfaces()
	if err != nil {
		return RunStats{}, err
	}

	progress := Progress{
		StartedAt:        time.Now().Unix(),
		TotalDomains:     len(tasks),
		ProcessedDomains: 0,
		TotalIPs:         0,
		PerVPN:           make(map[string]VPNProgress, len(ifaces)),
	}
	for _, iface := range ifaces {
		progress.PerVPN[iface] = VPNProgress{
			Interface:    iface,
			TotalDomains: len(tasks),
		}
	}
	w.emitProgress(progress)

	if len(tasks) == 0 {
		return RunStats{
			DomainsTotal:  0,
			DomainsDone:   0,
			IPsInserted:   0,
			Progress:      progress,
			CacheSnapshot: map[string]CachedSetValues{},
		}, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// querySem bounds the number of concurrent in-flight DNS queries across the
	// whole run, so a single heavy task (e.g. a wildcard that discovers many
	// subdomains) cannot monopolize a worker and stall overall progress.
	querySem := make(chan struct{}, w.parallel)

	jobs := make(chan domainTask)
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		runErr       error
		errOnce      sync.Once
		cacheV4BySet = make(map[string]map[string]struct{})
		cacheV6BySet = make(map[string]map[string]struct{})
	)

	workerCount := w.parallel
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				if err := runCtx.Err(); err != nil {
					return
				}
				result, err := w.processTask(runCtx, task, ifaces, querySem)
				if err != nil {
					errOnce.Do(func() {
						runErr = err
						cancel()
					})
					return
				}
				mu.Lock()
				progress.ProcessedDomains++
				progress.TotalIPs += result.Inserted
				for iface, count := range result.PerVPNDomains {
					entry := progress.PerVPN[iface]
					entry.DomainsProcessed += count
					progress.PerVPN[iface] = entry
				}
				for iface, count := range result.PerVPNIPs {
					entry := progress.PerVPN[iface]
					entry.IPsInserted += count
					progress.PerVPN[iface] = entry
				}
				for iface, count := range result.PerVPNErrors {
					entry := progress.PerVPN[iface]
					entry.Errors += count
					progress.PerVPN[iface] = entry
				}
				appendSetIPs(cacheV4BySet, task.SetV4, result.V4)
				appendSetIPs(cacheV6BySet, task.SetV6, result.V6)
				snapshot := progress.Clone()
				mu.Unlock()
				w.emitProgress(snapshot)
			}
		}()
	}

	snapshotStats := func() RunStats {
		mu.Lock()
		defer mu.Unlock()
		return buildRunStats(progress, cacheV4BySet, cacheV6BySet)
	}

	for _, task := range tasks {
		select {
		case <-runCtx.Done():
			close(jobs)
			wg.Wait()
			stats := snapshotStats()
			if runErr != nil {
				return stats, runErr
			}
			return stats, runCtx.Err()
		case jobs <- task:
		}
	}
	close(jobs)
	wg.Wait()
	final := snapshotStats()

	if runErr != nil {
		return final, runErr
	}
	if err := runCtx.Err(); err != nil {
		return final, err
	}
	return final, nil
}

func (w *Worker) activeInterfaces() ([]string, error) {
	profiles, err := w.vpns.List()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(profiles))
	active := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		iface := strings.TrimSpace(profile.InterfaceName)
		if iface == "" {
			continue
		}
		if _, exists := seen[iface]; exists {
			continue
		}
		up, err := w.ifaceUp(iface)
		if err != nil || !up {
			continue
		}
		seen[iface] = struct{}{}
		active = append(active, iface)
	}
	if len(active) == 0 {
		fallback, err := w.activeManagedVPNInterfaces()
		if err == nil && len(fallback) > 0 {
			active = append(active, fallback...)
		}
	}
	sort.Strings(active)
	if len(active) == 0 {
		return nil, fmt.Errorf("no active vpn interfaces found")
	}
	return active, nil
}

func (w *Worker) activeManagedVPNInterfaces() ([]string, error) {
	if w.ifaceList == nil {
		return nil, nil
	}
	names, err := w.ifaceList()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(names))
	active := make([]string, 0, len(names))
	for _, rawName := range names {
		iface := strings.TrimSpace(rawName)
		if iface == "" {
			continue
		}
		if !isManagedVPNInterface(iface) {
			continue
		}
		if _, exists := seen[iface]; exists {
			continue
		}
		up, err := w.ifaceUp(iface)
		if err != nil || !up {
			continue
		}
		seen[iface] = struct{}{}
		active = append(active, iface)
	}
	sort.Strings(active)
	return active, nil
}

func isManagedVPNInterface(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(lower, "wg-sv-") || strings.HasPrefix(lower, "awg-sv-")
}

func listInterfaceNames() ([]string, error) {
	infos, err := util.InterfacesWithAddrs()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}
