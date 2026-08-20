package remotelist

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"split-vpn-webui/internal/routing"
)

const (
	fetchTimeout      = 30 * time.Second
	schedulerInterval = time.Minute
	// retryInterval bounds how quickly a failing list is retried, independent of
	// its (possibly very long) configured refresh interval.
	retryInterval = 15 * time.Minute
	// notifyTimeout bounds the routing reapply triggered by a content change.
	notifyTimeout = 2 * time.Minute
)

// ChangeHandler is invoked once per refresh pass in which at least one list's
// content changed, so routing state is only reapplied when it has to be.
type ChangeHandler func(ctx context.Context, changed []RefreshResult)

// Manager owns remote list CRUD, scheduled refreshes and change notification.
type Manager struct {
	store  *Store
	client *http.Client

	refreshMu sync.Mutex

	handlerMu sync.RWMutex
	onChange  ChangeHandler

	contentsMu  sync.RWMutex
	contents    map[string]routing.RemoteListContent
	contentsGen uint64

	loopMu     sync.Mutex
	loopCancel context.CancelFunc
	loopWG     sync.WaitGroup
}

// NewManager creates a manager backed by the shared SQLite handle.
func NewManager(db *sql.DB) (*Manager, error) {
	store, err := NewStore(db)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store:  store,
		client: &http.Client{Timeout: fetchTimeout},
	}, nil
}

// SetChangeHandler registers the callback fired after a refresh pass that
// changed at least one list.
func (m *Manager) SetChangeHandler(handler ChangeHandler) {
	m.handlerMu.Lock()
	defer m.handlerMu.Unlock()
	m.onChange = handler
}

// RemoteListContents implements routing.RemoteListProvider. Routing state is
// re-derived on every stats broadcast, so the entry set is cached and only
// re-read after a mutation. The returned map is shared between callers and must
// be treated as read-only.
func (m *Manager) RemoteListContents(ctx context.Context) (map[string]routing.RemoteListContent, error) {
	m.contentsMu.RLock()
	cached := m.contents
	generation := m.contentsGen
	m.contentsMu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	loaded, err := m.store.Contents(ctx)
	if err != nil {
		return nil, err
	}
	m.contentsMu.Lock()
	// Only publish if nothing invalidated the cache while we were reading;
	// otherwise this snapshot is already stale and must not be cached.
	if m.contentsGen == generation {
		m.contents = loaded
	}
	m.contentsMu.Unlock()
	return loaded, nil
}

// RemoteListNames implements routing.RemoteListProvider.
func (m *Manager) RemoteListNames(ctx context.Context) (map[string]struct{}, error) {
	return m.store.Names(ctx)
}

func (m *Manager) invalidateContents() {
	m.contentsMu.Lock()
	m.contents = nil
	m.contentsGen++
	m.contentsMu.Unlock()
}

// List returns all configured remote lists.
func (m *Manager) List(ctx context.Context) ([]List, error) {
	return m.store.List(ctx)
}

// Get returns one remote list by id.
func (m *Manager) Get(ctx context.Context, id int64) (*List, error) {
	return m.store.Get(ctx, id)
}

// Entries returns the cached entries of one remote list.
func (m *Manager) Entries(ctx context.Context, id int64) ([]string, error) {
	return m.store.Entries(ctx, id)
}

// Create stores a new remote list and fetches it immediately so it is usable
// (and any URL problem is reported) right away.
func (m *Manager) Create(ctx context.Context, req UpsertRequest) (*List, RefreshResult, error) {
	normalized, err := NormalizeUpsert(req)
	if err != nil {
		return nil, RefreshResult{}, err
	}
	created, err := m.store.Create(ctx, normalized)
	if err != nil {
		return nil, RefreshResult{}, err
	}
	m.invalidateContents()
	result := m.refreshAndNotify(ctx, *created)
	refreshed, getErr := m.store.Get(ctx, created.ID)
	if getErr != nil {
		return created, result, nil
	}
	return refreshed, result, nil
}

// Update overwrites a remote list. It re-fetches when the source changed so the
// stored entries never lag behind the definition.
func (m *Manager) Update(ctx context.Context, id int64, req UpsertRequest) (*List, RefreshResult, error) {
	normalized, err := NormalizeUpsert(req)
	if err != nil {
		return nil, RefreshResult{}, err
	}
	previous, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, RefreshResult{}, err
	}
	updated, err := m.store.Update(ctx, id, normalized)
	if err != nil {
		return nil, RefreshResult{}, err
	}
	m.invalidateContents()

	sourceChanged := previous.URL != updated.URL || previous.Kind != updated.Kind
	if sourceChanged {
		result := m.refreshAndNotify(ctx, *updated)
		refreshed, getErr := m.store.Get(ctx, id)
		if getErr == nil {
			updated = refreshed
		}
		return updated, result, nil
	}
	if previous.Enabled != updated.Enabled {
		// Enabling or disabling a list changes which entries apply, even though
		// no fetch happened.
		m.notify(ctx, []RefreshResult{{Name: updated.Name, Kind: updated.Kind, Changed: true, EntryCount: updated.EntryCount}})
	}
	return updated, RefreshResult{Name: updated.Name, Kind: updated.Kind}, nil
}

// Delete removes a remote list. Routing state is reapplied so its entries stop
// matching immediately.
func (m *Manager) Delete(ctx context.Context, id int64) error {
	existing, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
	m.invalidateContents()
	m.notify(ctx, []RefreshResult{{Name: existing.Name, Kind: existing.Kind, Changed: true}})
	return nil
}

// Refresh re-fetches one list regardless of its schedule.
func (m *Manager) Refresh(ctx context.Context, id int64) (RefreshResult, error) {
	list, err := m.store.Get(ctx, id)
	if err != nil {
		return RefreshResult{}, err
	}
	return m.refreshAndNotify(ctx, *list), nil
}

// RefreshAll re-fetches every enabled list regardless of its schedule.
// Disabled lists are skipped: disabling a list must stop outbound requests to
// its URL, not just stop its entries from applying.
func (m *Manager) RefreshAll(ctx context.Context) ([]RefreshResult, error) {
	return m.refreshPass(ctx, true)
}

// Start launches the periodic refresh loop.
func (m *Manager) Start() error {
	m.loopMu.Lock()
	if m.loopCancel != nil {
		m.loopMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.loopCancel = cancel
	m.loopMu.Unlock()

	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		ticker := time.NewTicker(schedulerInterval)
		defer ticker.Stop()
		// Catch up on anything that came due while the service was down.
		m.runDuePass(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runDuePass(ctx)
			}
		}
	}()
	return nil
}

// Stop terminates the periodic refresh loop.
func (m *Manager) Stop() error {
	m.loopMu.Lock()
	cancel := m.loopCancel
	m.loopCancel = nil
	m.loopMu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.loopWG.Wait()
	return nil
}

func (m *Manager) runDuePass(ctx context.Context) {
	if _, err := m.refreshPass(ctx, false); err != nil && ctx.Err() == nil {
		log.Printf("remote list refresh pass failed: %v", err)
	}
}

// refreshPass refreshes every list that is due (or all of them when forced) and
// notifies once if anything changed.
func (m *Manager) refreshPass(ctx context.Context, force bool) ([]RefreshResult, error) {
	results, err := m.refreshDueLists(ctx, force)
	changed := make([]RefreshResult, 0, len(results))
	for _, result := range results {
		if result.Changed {
			changed = append(changed, result)
		}
	}
	if len(changed) > 0 {
		m.notify(ctx, changed)
	}
	return results, err
}

// refreshDueLists performs the fetches for one pass. The change handler runs
// outside this lock so a long routing reapply cannot block a manual refresh.
func (m *Manager) refreshDueLists(ctx context.Context, force bool) ([]RefreshResult, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	lists, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]RefreshResult, 0, len(lists))
	now := time.Now().Unix()
	for _, list := range lists {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if !list.Enabled {
			continue
		}
		if !force && !isDue(list, now) {
			continue
		}
		results = append(results, m.refreshOne(ctx, list))
	}
	return results, nil
}

func (m *Manager) refreshAndNotify(ctx context.Context, list List) RefreshResult {
	m.refreshMu.Lock()
	result := m.refreshOne(ctx, list)
	m.refreshMu.Unlock()
	if result.Changed {
		m.notify(ctx, []RefreshResult{result})
	}
	return result
}

// refreshOne downloads one list and persists the outcome. Entries are only
// rewritten when the parsed content differs from the stored fingerprint, so an
// unchanged source never triggers routing side effects.
func (m *Manager) refreshOne(ctx context.Context, list List) RefreshResult {
	result := RefreshResult{Name: list.Name, Kind: list.Kind, EntryCount: list.EntryCount, Skipped: list.SkippedCount}

	state, err := m.store.FetchState(ctx, list.ID)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	response, err := fetchList(fetchCtx, m.client, list.URL, state)
	if err != nil {
		result.Error = err.Error()
		m.recordFailure(ctx, list, err)
		return result
	}
	if response.NotModified {
		if err := m.store.MarkUnchanged(ctx, list.ID, fetchState{
			ETag:         response.ETag,
			LastModified: response.LastModified,
			ContentHash:  state.ContentHash,
		}, list.SkippedCount); err != nil {
			result.Error = err.Error()
		}
		return result
	}

	entries, skipped, err := parseBody(list.Kind, response.Body)
	if err != nil {
		result.Error = err.Error()
		m.recordFailure(ctx, list, err)
		return result
	}

	if err := validateParsedEntries(list, entries, skipped); err != nil {
		result.Error = err.Error()
		m.recordFailure(ctx, list, err)
		return result
	}

	newState := fetchState{
		ETag:         response.ETag,
		LastModified: response.LastModified,
		ContentHash:  hashEntries(entries),
	}
	if newState.ContentHash == state.ContentHash && list.EntryCount == len(entries) {
		if err := m.store.MarkUnchanged(ctx, list.ID, newState, skipped); err != nil {
			result.Error = err.Error()
		}
		result.Skipped = skipped
		return result
	}

	if err := m.store.SaveEntries(ctx, list.ID, entries, skipped, newState); err != nil {
		result.Error = err.Error()
		m.recordFailure(ctx, list, err)
		return result
	}
	m.invalidateContents()
	result.Changed = true
	result.EntryCount = len(entries)
	result.Skipped = skipped
	log.Printf("remote list %q refreshed: %d entries (%d skipped)", list.Name, len(entries), skipped)
	return result
}

// validateParsedEntries rejects a response that parsed to nothing usable. A
// captive portal, CDN error page or truncated transfer answers 200 with a body
// that yields no entries; accepting it would silently empty the list's ipset and
// drop the traffic it was steering back onto the WAN.
func validateParsedEntries(list List, entries []string, skipped int) error {
	if len(entries) > 0 {
		return nil
	}
	if skipped > 0 {
		return fmt.Errorf("response contained no usable %s entries (%d unparsable lines)", list.Kind, skipped)
	}
	if list.EntryCount > 0 {
		return fmt.Errorf("response was empty while %d entries are cached", list.EntryCount)
	}
	// Genuinely empty source and nothing cached to lose.
	return nil
}

func (m *Manager) recordFailure(ctx context.Context, list List, cause error) {
	if err := m.store.MarkFailed(ctx, list.ID, cause.Error()); err != nil {
		log.Printf("remote list %q: failed to record fetch error: %v", list.Name, err)
		return
	}
	log.Printf("remote list %q refresh failed: %v", list.Name, cause)
}

// notify runs the change handler on a detached context so a disconnecting HTTP
// client can never abort a half-applied routing update.
func (m *Manager) notify(_ context.Context, changed []RefreshResult) {
	m.handlerMu.RLock()
	handler := m.onChange
	m.handlerMu.RUnlock()
	if handler == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	handler(notifyCtx, changed)
}

// isDue reports whether an enabled list should be refreshed now. A list whose
// last attempt failed (or never ran) uses the shorter retry window instead of
// its configured interval.
func isDue(list List, now int64) bool {
	if !list.Enabled {
		return false
	}
	interval := int64(list.RefreshIntervalSeconds)
	if interval < MinRefreshIntervalSeconds {
		interval = DefaultRefreshIntervalSeconds
	}
	if list.LastSuccessAt <= 0 || list.LastSuccessAt < list.LastFetchAt {
		retry := int64(retryInterval / time.Second)
		if interval < retry {
			retry = interval
		}
		return now-list.LastFetchAt >= retry
	}
	return now-list.LastSuccessAt >= interval
}

// ReplaceAll replaces every remote list definition (backup restore) and
// re-fetches them so entries match the restored configuration.
func (m *Manager) ReplaceAll(ctx context.Context, lists []UpsertRequest) error {
	normalized := make([]UpsertRequest, 0, len(lists))
	for _, list := range lists {
		item, err := NormalizeUpsert(list)
		if err != nil {
			return err
		}
		normalized = append(normalized, item)
	}
	if err := m.store.ReplaceAll(ctx, normalized); err != nil {
		return err
	}
	m.invalidateContents()
	if len(normalized) == 0 {
		return nil
	}
	if _, err := m.refreshDueLists(ctx, true); err != nil {
		return err
	}
	return nil
}
