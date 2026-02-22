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
	prewarmIPSetTimeoutSec   = 43200
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

// WorkerOptions controls worker runtime behavior.
type WorkerOptions struct {
	Parallelism      int
	ProgressCallback func(Progress)
	InterfaceActive  func(name string) (bool, error)
	InterfaceList    func() ([]string, error)
}

// Worker executes one DNS pre-warm pass.
type Worker struct {
	groups    GroupSource
	vpns      VPNSource
	doh       DoHClient
	ipset     routing.IPSetOperator
	parallel  int
	progress  func(Progress)
	ifaceUp   func(name string) (bool, error)
	ifaceList func() ([]string, error)
}

type domainTask struct {
	GroupName string
	SetV4     string
	SetV6     string
	Domain    string
}

type taskResult struct {
	Inserted      int
	PerVPNIPs     map[string]int
	PerVPNErrors  map[string]int
	PerVPNDomains map[string]int
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
	return &Worker{
		groups:    groups,
		vpns:      vpns,
		doh:       doh,
		ipset:     ipset,
		parallel:  parallelism,
		progress:  opts.ProgressCallback,
		ifaceUp:   ifaceActive,
		ifaceList: ifaceList,
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
			DomainsTotal: 0,
			DomainsDone:  0,
			IPsInserted:  0,
			Progress:     progress,
		}, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan domainTask)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		runErr  error
		errOnce sync.Once
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
				snapshot := progress.Clone()
				mu.Unlock()
				w.emitProgress(snapshot)
			}
		}()
	}

	for _, task := range tasks {
		select {
		case <-runCtx.Done():
			close(jobs)
			wg.Wait()
			if runErr != nil {
				return RunStats{}, runErr
			}
			return RunStats{}, runCtx.Err()
		case jobs <- task:
		}
	}
	close(jobs)
	wg.Wait()

	if runErr != nil {
		return RunStats{}, runErr
	}
	if err := runCtx.Err(); err != nil {
		return RunStats{}, err
	}
	final := progress.Clone()
	return RunStats{
		DomainsTotal: final.TotalDomains,
		DomainsDone:  final.ProcessedDomains,
		IPsInserted:  final.TotalIPs,
		Progress:     final,
	}, nil
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

func buildTasks(groups []routing.DomainGroup) ([]domainTask, error) {
	tasks := make([]domainTask, 0)
	for _, group := range groups {
		for ruleIndex, rule := range group.Rules {
			sets := routing.RuleSetNames(group.Name, ruleIndex)
			domainList := append([]string(nil), rule.Domains...)
			domainList = append(domainList, rule.WildcardDomains...)
			for _, rawDomain := range domainList {
				domain := normalizeDomain(rawDomain)
				if domain == "" {
					continue
				}
				tasks = append(tasks, domainTask{
					GroupName: group.Name,
					SetV4:     sets.DestinationV4,
					SetV6:     sets.DestinationV6,
					Domain:    domain,
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

func (w *Worker) processTask(ctx context.Context, task domainTask, ifaces []string) (taskResult, error) {
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
		cnames, err := w.doh.QueryCNAME(ctx, task.Domain, iface)
		if err != nil {
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
			v4, err := w.doh.QueryA(ctx, target, iface)
			if err != nil {
				perVPNErrors[iface]++
			} else {
				for _, ip := range v4 {
					allV4[ip] = struct{}{}
					perIfaceV4[iface][ip] = struct{}{}
				}
			}

			v6, err := w.doh.QueryAAAA(ctx, target, iface)
			if err != nil {
				perVPNErrors[iface]++
			} else {
				for _, ip := range v6 {
					allV6[ip] = struct{}{}
					perIfaceV6[iface][ip] = struct{}{}
				}
			}
		}
	}

	v4List := mapKeysSorted(allV4)
	v6List := mapKeysSorted(allV6)
	for _, ip := range v4List {
		if err := w.ipset.AddIP(task.SetV4, ip, prewarmIPSetTimeoutSec); err != nil {
			return taskResult{}, err
		}
	}
	for _, ip := range v6List {
		if err := w.ipset.AddIP(task.SetV6, ip, prewarmIPSetTimeoutSec); err != nil {
			return taskResult{}, err
		}
	}
	for _, iface := range ifaces {
		perVPNIPs[iface] = len(perIfaceV4[iface]) + len(perIfaceV6[iface])
	}
	return taskResult{
		Inserted:      len(v4List) + len(v6List),
		PerVPNIPs:     perVPNIPs,
		PerVPNErrors:  perVPNErrors,
		PerVPNDomains: perVPNDomains,
	}, nil
}

func mapKeysSorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func (w *Worker) emitProgress(progress Progress) {
	if w.progress == nil {
		return
	}
	w.progress(progress.Clone())
}
