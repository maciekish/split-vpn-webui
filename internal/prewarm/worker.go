package prewarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/util"
	"split-vpn-webui/internal/vpn"
)

const (
	defaultParallelism       = 4
	maxSafeWorkerParallelism = 64
)

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
	Parallelism         int
	Timeout             time.Duration
	ExtraNameservers    []string
	ECSProfiles         []string
	AdditionalResolvers []DoHClient
	ProgressCallback    func(Progress)
	ErrorCallback       func(QueryError)
	InterfaceActive     func(name string) (bool, error)
	InterfaceList       func() ([]string, error)
	WildcardResolver    WildcardResolver
}

// Worker executes one DNS pre-warm pass.
type Worker struct {
	groups    GroupSource
	vpns      VPNSource
	doh       DoHClient
	ipset     routing.IPSetOperator
	resolvers []DoHClient
	parallel  int
	progress  func(Progress)
	onError   func(QueryError)
	ifaceUp   func(name string) (bool, error)
	ifaceList func() ([]string, error)
	wildcard  WildcardResolver
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
		groups:    groups,
		vpns:      vpns,
		doh:       doh,
		ipset:     ipset,
		resolvers: resolvers,
		parallel:  parallelism,
		progress:  opts.ProgressCallback,
		onError:   opts.ErrorCallback,
		ifaceUp:   ifaceActive,
		ifaceList: ifaceList,
		wildcard:  wildcard,
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
				result, err := w.processTask(runCtx, task, ifaces)
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
		fallback, err := w.activeManagedWireGuardInterfaces()
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

func (w *Worker) activeManagedWireGuardInterfaces() ([]string, error) {
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
		if !strings.HasPrefix(strings.ToLower(iface), "wg-sv-") {
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

func (w *Worker) processTask(ctx context.Context, task domainTask, ifaces []string) (taskResult, error) {
	targets := map[string]struct{}{task.Domain: {}}
	perVPNDomains := make(map[string]int, len(ifaces))
	perVPNErrors := make(map[string]int, len(ifaces))
	perVPNIPs := make(map[string]int, len(ifaces))
	perIfaceV4 := make(map[string]map[string]struct{}, len(ifaces))
	perIfaceV6 := make(map[string]map[string]struct{}, len(ifaces))

	if task.Wildcard && w.wildcard != nil {
		discovered, err := w.wildcard.Resolve(ctx, "*."+task.Domain)
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
				target := normalizeDomain(subdomain)
				if target == "" {
					continue
				}
				targets[target] = struct{}{}
			}
		}
	}

	for _, iface := range ifaces {
		perVPNDomains[iface] = 1
		perIfaceV4[iface] = make(map[string]struct{})
		perIfaceV6[iface] = make(map[string]struct{})
		for _, resolver := range w.resolvers {
			resolverName := resolverLabel(resolver)
			cnames, err := resolver.QueryCNAME(ctx, task.Domain, iface)
			if err != nil {
				w.emitQueryError(QueryError{
					Stage:     "cname",
					Domain:    task.Domain,
					Interface: iface,
					Resolver:  resolverName,
					Err:       err,
				})
				perVPNErrors[iface]++
				continue
			}
			for _, cname := range cnames {
				target := normalizeDomain(cname)
				if target != "" {
					targets[target] = struct{}{}
				}
			}
		}
	}

	allV4 := make(map[string]struct{})
	allV6 := make(map[string]struct{})
	targetList := make([]string, 0, len(targets))
	for target := range targets {
		targetList = append(targetList, target)
	}
	sort.Strings(targetList)

	for _, target := range targetList {
		if err := ctx.Err(); err != nil {
			return taskResult{}, err
		}
		for _, iface := range ifaces {
			for _, resolver := range w.resolvers {
				resolverName := resolverLabel(resolver)
				v4, err := resolver.QueryA(ctx, target, iface)
				if err != nil {
					w.emitQueryError(QueryError{
						Stage:     "a",
						Domain:    target,
						Interface: iface,
						Resolver:  resolverName,
						Err:       err,
					})
					perVPNErrors[iface]++
				} else {
					for _, ip := range v4 {
						allV4[ip] = struct{}{}
						perIfaceV4[iface][ip] = struct{}{}
					}
				}

				v6, err := resolver.QueryAAAA(ctx, target, iface)
				if err != nil {
					w.emitQueryError(QueryError{
						Stage:     "aaaa",
						Domain:    target,
						Interface: iface,
						Resolver:  resolverName,
						Err:       err,
					})
					perVPNErrors[iface]++
				} else {
					for _, ip := range v6 {
						allV6[ip] = struct{}{}
						perIfaceV6[iface][ip] = struct{}{}
					}
				}
			}
		}
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
