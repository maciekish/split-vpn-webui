package routing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"split-vpn-webui/internal/settings"
)

const (
	defaultResolverIntervalSeconds = 3600
	maxResolverIntervalSeconds     = 24 * 3600
	defaultResolverTimeoutSeconds  = 10
	maxResolverTimeoutSeconds      = 60
	defaultResolverParallelism     = 6
	maxResolverParallelism         = 64
)

// ResolverScheduler executes periodic/manual resolver refresh runs.
type ResolverScheduler struct {
	manager  *Manager
	settings *settings.Manager

	domainResolver   DomainResolver
	asnResolver      ASNResolver
	wildcardResolver WildcardResolver
	customDomain     bool
	customASN        bool
	customWildcard   bool

	now func() time.Time

	mu              sync.RWMutex
	started         bool
	running         bool
	progress        *ResolverProgress
	lastRun         *ResolverRunRecord
	defaultInterval time.Duration
	loopCancel      context.CancelFunc
	runCancel       context.CancelFunc
	progressHandler func(ResolverProgress)

	loopWG sync.WaitGroup
	runWG  sync.WaitGroup
}

type resolverJob struct {
	Selector ResolverSelector
	Label    string
}

type resolverResult struct {
	job    resolverJob
	values ResolverValues
	err    error
}

// NewResolverScheduler creates a resolver scheduler with default providers.
func NewResolverScheduler(manager *Manager, settingsManager *settings.Manager) (*ResolverScheduler, error) {
	if manager == nil {
		return nil, fmt.Errorf("routing manager is required")
	}
	if settingsManager == nil {
		return nil, fmt.Errorf("settings manager is required")
	}

	current, err := settingsManager.Get()
	if err != nil {
		current = settings.Settings{}
	}
	lastRun, _ := manager.store.LastResolverRun(context.Background())

	return &ResolverScheduler{
		manager:          manager,
		settings:         settingsManager,
		domainResolver:   newDoHDomainResolver(resolverDomainTimeoutFromSettings(current)),
		asnResolver:      newRIPEASNResolver(resolverASNTimeoutFromSettings(current)),
		wildcardResolver: newCRTSHWildcardResolver(resolverWildcardTimeoutFromSettings(current)),
		now:              time.Now,
		defaultInterval:  resolverIntervalFromSettings(current),
		lastRun:          lastRun,
	}, nil
}

// NewResolverSchedulerWithDeps creates a resolver scheduler with injected resolvers (tests).
func NewResolverSchedulerWithDeps(
	manager *Manager,
	settingsManager *settings.Manager,
	domainResolver DomainResolver,
	asnResolver ASNResolver,
	wildcardResolver WildcardResolver,
) (*ResolverScheduler, error) {
	scheduler, err := NewResolverScheduler(manager, settingsManager)
	if err != nil {
		return nil, err
	}
	if domainResolver != nil {
		scheduler.domainResolver = domainResolver
		scheduler.customDomain = true
	}
	if asnResolver != nil {
		scheduler.asnResolver = asnResolver
		scheduler.customASN = true
	}
	if wildcardResolver != nil {
		scheduler.wildcardResolver = wildcardResolver
		scheduler.customWildcard = true
	}
	return scheduler, nil
}

// SetProgressHandler registers a callback for live resolver progress.
func (s *ResolverScheduler) SetProgressHandler(handler func(ResolverProgress)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progressHandler = handler
}

// Start launches the periodic resolver loop.
func (s *ResolverScheduler) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.started = true
	s.loopCancel = cancel
	s.mu.Unlock()

	s.loopWG.Add(1)
	go func() {
		defer s.loopWG.Done()
		for {
			interval := s.currentInterval()
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				_ = s.TriggerNow()
			}
		}
	}()
	return nil
}

// Stop terminates periodic scheduling and cancels an active run.
func (s *ResolverScheduler) Stop() error {
	s.mu.Lock()
	loopCancel := s.loopCancel
	runCancel := s.runCancel
	s.started = false
	s.loopCancel = nil
	s.mu.Unlock()

	if loopCancel != nil {
		loopCancel()
	}
	if runCancel != nil {
		runCancel()
	}
	s.loopWG.Wait()
	s.runWG.Wait()
	return nil
}

// TriggerNow starts one resolver run in the background.
func (s *ResolverScheduler) TriggerNow() error {
	current, err := s.settings.Get()
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrResolverRunInProgress
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	initial := ResolverProgress{StartedAt: s.now().Unix()}
	s.running = true
	s.progress = &initial
	s.runCancel = runCancel
	s.runWG.Add(1)
	s.mu.Unlock()

	s.emitProgress(initial)
	go s.executeRun(runCtx, current)
	return nil
}

// Status returns live and historical resolver status.
func (s *ResolverScheduler) Status(ctx context.Context) (ResolverStatus, error) {
	s.mu.RLock()
	running := s.running
	progress := s.progress
	lastRun := s.lastRun
	s.mu.RUnlock()

	if lastRun == nil {
		loaded, err := s.manager.store.LastResolverRun(ctx)
		if err != nil {
			return ResolverStatus{}, err
		}
		lastRun = loaded
		if loaded != nil {
			s.mu.Lock()
			s.lastRun = loaded
			s.mu.Unlock()
		}
	}

	status := ResolverStatus{
		Running: running,
		LastRun: cloneResolverRun(lastRun),
	}
	if progress != nil {
		cloned := progress.Clone()
		status.Progress = &cloned
	}
	return status, nil
}

func (s *ResolverScheduler) executeRun(ctx context.Context, current settings.Settings) {
	defer s.runWG.Done()
	started := s.now()

	stats, runErr := s.resolveSelectors(ctx, current)
	finished := s.now()
	record := ResolverRunRecord{
		StartedAt:        started.Unix(),
		FinishedAt:       finished.Unix(),
		DurationMS:       finished.Sub(started).Milliseconds(),
		SelectorsTotal:   stats.SelectorsTotal,
		SelectorsDone:    stats.SelectorsDone,
		PrefixesResolved: stats.PrefixesResolved,
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	saved, saveErr := s.manager.store.SaveResolverRun(context.Background(), record)
	if saveErr != nil {
		saved = &record
		if saved.Error == "" {
			saved.Error = saveErr.Error()
		}
	}

	s.mu.Lock()
	s.running = false
	s.runCancel = nil
	s.lastRun = saved
	finalProgress := ResolverProgress{
		StartedAt:        started.Unix(),
		SelectorsTotal:   stats.SelectorsTotal,
		SelectorsDone:    stats.SelectorsDone,
		PrefixesResolved: stats.PrefixesResolved,
		PerProvider:      stats.PerProvider,
	}
	s.progress = &finalProgress
	s.mu.Unlock()
	s.emitProgress(finalProgress)
}

func (s *ResolverScheduler) resolveSelectors(ctx context.Context, current settings.Settings) (resolverStats, error) {
	enabled := resolverProviderFlagsFromSettings(current)
	resolvers := s.resolversForRun(current, enabled)
	groups, err := s.manager.store.List(ctx)
	if err != nil {
		return resolverStats{}, err
	}
	jobs := collectResolverJobs(groups, enabled)
	progress := ResolverProgress{
		StartedAt:      s.now().Unix(),
		SelectorsTotal: len(jobs),
		PerProvider:    make(map[string]ResolverProviderProgress),
	}
	for _, job := range jobs {
		entry := progress.PerProvider[job.Selector.Type]
		entry.SelectorsTotal++
		progress.PerProvider[job.Selector.Type] = entry
	}
	s.emitProgress(progress)
	if len(jobs) == 0 {
		return resolverStats{PerProvider: cloneResolverProviderProgress(progress.PerProvider)}, nil
	}

	parallelism := resolverParallelismFromSettings(current)
	if parallelism > len(jobs) {
		parallelism = len(jobs)
	}
	if parallelism <= 0 {
		parallelism = 1
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobCh := make(chan resolverJob)
	resultCh := make(chan resolverResult, len(jobs))
	var workers sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobCh {
				values, err := s.resolveJob(runCtx, job, resolvers)
				resultCh <- resolverResult{job: job, values: values, err: err}
				if err != nil {
					cancel()
				}
			}
		}()
	}
	go func() {
		defer close(resultCh)
		for _, job := range jobs {
			select {
			case <-runCtx.Done():
				close(jobCh)
				workers.Wait()
				return
			case jobCh <- job:
			}
		}
		close(jobCh)
		workers.Wait()
	}()

	snapshot := make(map[ResolverSelector]ResolverValues, len(jobs))
	var firstErr error
	for result := range resultCh {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
		if result.err == nil {
			snapshot[result.job.Selector] = result.values
		}

		progress.SelectorsDone++
		resolvedCount := len(result.values.V4) + len(result.values.V6)
		progress.PrefixesResolved += resolvedCount
		progress.CurrentSelector = result.job.Label
		providerProgress := progress.PerProvider[result.job.Selector.Type]
		providerProgress.SelectorsDone++
		providerProgress.PrefixesResolved += resolvedCount
		progress.PerProvider[result.job.Selector.Type] = providerProgress
		s.emitProgress(progress)
	}
	if firstErr != nil {
		return resolverStats{
			SelectorsTotal:   progress.SelectorsTotal,
			SelectorsDone:    progress.SelectorsDone,
			PrefixesResolved: progress.PrefixesResolved,
			PerProvider:      cloneResolverProviderProgress(progress.PerProvider),
		}, firstErr
	}

	if err := s.manager.store.ReplaceResolverSnapshot(ctx, snapshot); err != nil {
		return resolverStats{}, err
	}
	if err := s.manager.Apply(ctx); err != nil {
		return resolverStats{}, err
	}

	return resolverStats{
		SelectorsTotal:   progress.SelectorsTotal,
		SelectorsDone:    progress.SelectorsDone,
		PrefixesResolved: progress.PrefixesResolved,
		PerProvider:      cloneResolverProviderProgress(progress.PerProvider),
	}, nil
}

func (s *ResolverScheduler) resolveJob(ctx context.Context, job resolverJob, resolvers runResolvers) (ResolverValues, error) {
	switch job.Selector.Type {
	case "domain":
		if resolvers.domain == nil {
			return ResolverValues{}, nil
		}
		return resolvers.domain.Resolve(ctx, job.Selector.Key)
	case "asn":
		if resolvers.asn == nil {
			return ResolverValues{}, nil
		}
		return resolvers.asn.Resolve(ctx, job.Selector.Key)
	case "wildcard":
		if resolvers.wildcard == nil || resolvers.domain == nil {
			return ResolverValues{}, nil
		}
		domains, err := resolvers.wildcard.Resolve(ctx, job.Selector.Key)
		if err != nil {
			return ResolverValues{}, err
		}
		if len(domains) == 0 {
			domains = []string{strings.TrimPrefix(job.Selector.Key, "*.")}
		}
		v4 := make(map[string]struct{})
		v6 := make(map[string]struct{})
		for _, domain := range domains {
			values, err := resolvers.domain.Resolve(ctx, domain)
			if err != nil {
				continue
			}
			for _, cidr := range values.V4 {
				v4[cidr] = struct{}{}
			}
			for _, cidr := range values.V6 {
				v6[cidr] = struct{}{}
			}
		}
		return ResolverValues{V4: mapKeysSorted(v4), V6: mapKeysSorted(v6)}, nil
	default:
		return ResolverValues{}, fmt.Errorf("unknown selector type %q", job.Selector.Type)
	}
}

func (s *ResolverScheduler) resolversForRun(current settings.Settings, enabled resolverProviderFlags) runResolvers {
	// Non-custom resolvers are rebuilt per run so timeout setting changes are
	// applied immediately without requiring a process restart.
	result := runResolvers{}
	if enabled.Domain || enabled.Wildcard {
		result.domain = newDoHDomainResolver(resolverDomainTimeoutFromSettings(current))
	}
	if enabled.ASN {
		result.asn = newRIPEASNResolver(resolverASNTimeoutFromSettings(current))
	}
	if enabled.Wildcard {
		result.wildcard = newCRTSHWildcardResolver(resolverWildcardTimeoutFromSettings(current))
	}

	s.mu.RLock()
	if (enabled.Domain || enabled.Wildcard) && s.customDomain && s.domainResolver != nil {
		result.domain = s.domainResolver
	}
	if enabled.ASN && s.customASN && s.asnResolver != nil {
		result.asn = s.asnResolver
	}
	if enabled.Wildcard && s.customWildcard && s.wildcardResolver != nil {
		result.wildcard = s.wildcardResolver
	}
	s.mu.RUnlock()
	return result
}
