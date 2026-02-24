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
	return s.TriggerNow()
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
}

func toRoutingCacheSnapshot(snapshot map[string]CachedSetValues) map[string]routing.ResolverValues {
	out := make(map[string]routing.ResolverValues, len(snapshot))
	for setName, values := range snapshot {
		out[setName] = routing.ResolverValues{
			V4: append([]string(nil), values.V4...),
			V6: append([]string(nil), values.V6...),
		}
	}
	return out
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
	if run == nil {
		return nil
	}
	cloned := *run
	return &cloned
}

func parallelismFromSettings(current settings.Settings) int {
	if current.PrewarmParallelism <= 0 {
		return defaultParallelism
	}
	if current.PrewarmParallelism > maxParallelism {
		return maxParallelism
	}
	return current.PrewarmParallelism
}

func timeoutFromSettings(current settings.Settings) time.Duration {
	seconds := current.PrewarmDoHTimeoutSeconds
	if seconds <= 0 {
		seconds = defaultTimeoutSeconds
	}
	if seconds > maxTimeoutSeconds {
		seconds = maxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func intervalFromSettings(current settings.Settings) time.Duration {
	seconds := current.PrewarmIntervalSeconds
	if seconds <= 0 {
		seconds = defaultIntervalSeconds
	}
	if seconds > maxIntervalSeconds {
		seconds = maxIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

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
