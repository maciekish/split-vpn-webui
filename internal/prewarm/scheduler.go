package prewarm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/settings"
)

const (
	defaultIntervalSeconds = 7200
	maxIntervalSeconds     = 7 * 24 * 3600
	defaultTimeoutSeconds  = 10
	maxTimeoutSeconds      = 60
	maxParallelism         = 64
)

var (
	// ErrRunInProgress indicates a run is already active.
	ErrRunInProgress = errors.New("prewarm run already in progress")
	// ErrRunNotActive indicates there is no active run to stop.
	ErrRunNotActive = errors.New("prewarm run is not active")
)

// Status is returned by the API status endpoint.
type Status struct {
	Running  bool       `json:"running"`
	LastRun  *RunRecord `json:"lastRun,omitempty"`
	Progress *Progress  `json:"progress,omitempty"`
}

// Scheduler runs the pre-warm worker periodically or on-demand.
type Scheduler struct {
	settings *settings.Manager
	store    *Store
	groups   GroupSource
	vpns     VPNSource
	ipset    routing.IPSetOperator
	cache    prewarmCacheManager
	logger   Logger

	now func() time.Time

	mu              sync.RWMutex
	started         bool
	running         bool
	defaultInterval time.Duration
	progress        *Progress
	lastRun         *RunRecord
	loopCancel      context.CancelFunc
	runCancel       context.CancelFunc
	progressHandler func(Progress)

	loopWG sync.WaitGroup
	runWG  sync.WaitGroup
}

type prewarmCacheManager interface {
	UpsertPrewarmSnapshot(ctx context.Context, snapshot map[string]routing.ResolverValues) error
	ClearPrewarmCache(ctx context.Context) error
}

// NewScheduler creates a scheduler with persisted run tracking.
func NewScheduler(db *sql.DB, settingsManager *settings.Manager, groups GroupSource, vpns VPNSource, ipset routing.IPSetOperator) (*Scheduler, error) {
	if settingsManager == nil {
		return nil, fmt.Errorf("settings manager is required")
	}
	if groups == nil {
		return nil, fmt.Errorf("group source is required")
	}
	if vpns == nil {
		return nil, fmt.Errorf("vpn source is required")
	}
	cacheManager, ok := groups.(prewarmCacheManager)
	if !ok {
		return nil, fmt.Errorf("group source does not support prewarm cache management")
	}
	store, err := NewStore(db)
	if err != nil {
		return nil, err
	}
	if ipset == nil {
		ipset = routing.NewIPSetManager(nil)
	}

	loadedSettings, err := settingsManager.Get()
	if err != nil {
		loadedSettings = settings.Settings{}
	}
	interval := intervalFromSettings(loadedSettings)
	lastRun, _ := store.LastRun(context.Background())

	return &Scheduler{
		settings:        settingsManager,
		store:           store,
		groups:          groups,
		vpns:            vpns,
		ipset:           ipset,
		cache:           cacheManager,
		now:             time.Now,
		defaultInterval: interval,
		lastRun:         lastRun,
	}, nil
}

// SetLogger registers an optional diagnostics logger.
func (s *Scheduler) SetLogger(logger Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = logger
}

// SetProgressHandler registers a callback invoked on progress updates.
func (s *Scheduler) SetProgressHandler(handler func(Progress)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progressHandler = handler
}

// Start launches the periodic scheduler loop.
func (s *Scheduler) Start() error {
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

func (s *Scheduler) currentInterval() time.Duration {
	current, err := s.settings.Get()
	if err != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.defaultInterval
	}
	interval := intervalFromSettings(current)
	s.mu.Lock()
	s.defaultInterval = interval
	s.mu.Unlock()
	return interval
}

// Stop terminates periodic scheduling and cancels an active run.
func (s *Scheduler) Stop() error {
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

// TriggerNow starts a run in the background.
func (s *Scheduler) TriggerNow() error {
	current, err := s.settings.Get()
	if err != nil {
		return err
	}
	if err := validateQuerySettings(current); err != nil {
		s.logWarnf("prewarm trigger rejected: %v", err)
		return err
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrRunInProgress
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	initial := Progress{
		StartedAt: s.now().Unix(),
		PerVPN:    map[string]VPNProgress{},
	}
	s.running = true
	s.progress = &initial
	s.runCancel = runCancel
	s.runWG.Add(1)
	s.mu.Unlock()

	s.emitProgress(initial)
	s.logInfof(
		"prewarm run started interval=%ds timeout=%ds parallelism=%d extra_nameservers=%d ecs_profiles=%d",
		current.PrewarmIntervalSeconds,
		timeoutFromSettings(current)/time.Second,
		parallelismFromSettings(current),
		lenOrZero(current.PrewarmExtraNameservers),
		lenOrZero(current.PrewarmECSProfiles),
	)
	go s.executeRun(runCtx, current)
	return nil
}

// ClearCacheAndRun clears pre-warm cache rows and immediately starts a new run.
func (s *Scheduler) ClearCacheAndRun() error {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()
	if running {
		return ErrRunInProgress
	}
	if s.cache != nil {
		if err := s.cache.ClearPrewarmCache(context.Background()); err != nil {
			return err
		}
	}
	s.logInfof("prewarm cache cleared by request")
	return s.TriggerNow()
}

// CancelRun stops the currently active pre-warm run while keeping the scheduler active.
func (s *Scheduler) CancelRun() error {
	s.mu.RLock()
	running := s.running
	runCancel := s.runCancel
	s.mu.RUnlock()
	if !running || runCancel == nil {
		return ErrRunNotActive
	}
	s.logWarnf("prewarm run cancellation requested")
	runCancel()
	return nil
}

func (s *Scheduler) executeRun(ctx context.Context, current settings.Settings) {
	defer s.runWG.Done()
	started := s.now()

	timeout := timeoutFromSettings(current)
	extraNameservers, queryErr := nameserversFromSettings(current)
	if queryErr != nil {
		s.finishRun(started, RunStats{}, queryErr)
		return
	}
	ecsProfiles, queryErr := ecsProfilesFromSettings(current)
	if queryErr != nil {
		s.finishRun(started, RunStats{}, queryErr)
		return
	}
	doh := NewCloudflareDoHClient(timeout)
	worker, err := NewWorker(s.groups, s.vpns, doh, s.ipset, WorkerOptions{
		Parallelism:      parallelismFromSettings(current),
		Timeout:          timeout,
		ExtraNameservers: extraNameservers,
		ECSProfiles:      ecsProfiles,
		WildcardResolver: newCRTSHWildcardResolver(timeout),
		ErrorCallback: func(event QueryError) {
			s.logDebugf(
				"prewarm query error stage=%s iface=%s domain=%s resolver=%s err=%v",
				event.Stage,
				event.Interface,
				event.Domain,
				event.Resolver,
				event.Err,
			)
		},
		ProgressCallback: func(progress Progress) {
			s.mu.Lock()
			cloned := progress.Clone()
			s.progress = &cloned
			s.mu.Unlock()
			s.emitProgress(cloned)
		},
	})

	var (
		stats  RunStats
		runErr error
	)
	if err != nil {
		runErr = err
	} else {
		stats, runErr = worker.Run(ctx)
	}
	if worker != nil && s.cache != nil {
		cacheErr := s.cache.UpsertPrewarmSnapshot(context.Background(), toRoutingCacheSnapshot(stats.CacheSnapshot))
		if cacheErr != nil {
			if runErr == nil {
				runErr = cacheErr
			} else {
				runErr = errors.Join(runErr, cacheErr)
			}
		}
	}

	s.finishRun(started, stats, runErr)
}

func (s *Scheduler) finishRun(started time.Time, stats RunStats, runErr error) {
	stats = s.mergeStatsWithCurrentProgress(started, stats)
	finished := s.now()
	record := RunRecord{
		StartedAt:    started.Unix(),
		FinishedAt:   finished.Unix(),
		DurationMS:   finished.Sub(started).Milliseconds(),
		DomainsTotal: stats.DomainsTotal,
		DomainsDone:  stats.DomainsDone,
		IPsInserted:  stats.IPsInserted,
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	saved, saveErr := s.store.SaveRun(context.Background(), record)
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
	if stats.Progress.TotalDomains > 0 {
		finalProgress := stats.Progress.Clone()
		s.progress = &finalProgress
	} else if s.progress == nil {
		zero := Progress{StartedAt: started.Unix(), PerVPN: map[string]VPNProgress{}}
		s.progress = &zero
	}
	emit := s.progress
	s.mu.Unlock()

	if emit != nil {
		s.emitProgress(*emit)
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			s.logWarnf(
				"prewarm run canceled duration_ms=%d domains=%d/%d ips=%d errors=%d",
				record.DurationMS,
				record.DomainsDone,
				record.DomainsTotal,
				record.IPsInserted,
				progressErrorCount(stats.Progress),
			)
			return
		}
		s.logErrorf(
			"prewarm run failed duration_ms=%d domains=%d/%d ips=%d errors=%d err=%v",
			record.DurationMS,
			record.DomainsDone,
			record.DomainsTotal,
			record.IPsInserted,
			progressErrorCount(stats.Progress),
			runErr,
		)
		return
	}
	s.logInfof(
		"prewarm run finished duration_ms=%d domains=%d/%d ips=%d errors=%d",
		record.DurationMS,
		record.DomainsDone,
		record.DomainsTotal,
		record.IPsInserted,
		progressErrorCount(stats.Progress),
	)
}

func toRoutingCacheSnapshot(snapshot map[string]CachedSetValues) map[string]routing.ResolverValues {
	return cacheSnapshotToResolverValues(snapshot)
}

// Status returns live and historical scheduler state.
func (s *Scheduler) Status(ctx context.Context) (Status, error) {
	s.mu.RLock()
	running := s.running
	lastRun := s.lastRun
	progress := s.progress
	s.mu.RUnlock()

	if lastRun == nil {
		loaded, err := s.store.LastRun(ctx)
		if err != nil {
			return Status{}, err
		}
		lastRun = loaded
		if loaded != nil {
			s.mu.Lock()
			s.lastRun = loaded
			s.mu.Unlock()
		}
	}

	status := Status{
		Running: running,
		LastRun: cloneRunRecord(lastRun),
	}
	if progress != nil {
		cloned := progress.Clone()
		status.Progress = &cloned
	}
	return status, nil
}

func (s *Scheduler) emitProgress(progress Progress) {
	s.mu.RLock()
	handler := s.progressHandler
	s.mu.RUnlock()
	if handler != nil {
		handler(progress.Clone())
	}
}

func cloneRunRecord(run *RunRecord) *RunRecord {
	return cloneStoredRunRecord(run)
}

func parallelismFromSettings(current settings.Settings) int {
	return configuredParallelism(current)
}

func timeoutFromSettings(current settings.Settings) time.Duration {
	return configuredTimeout(current)
}

func intervalFromSettings(current settings.Settings) time.Duration {
	return configuredInterval(current)
}
