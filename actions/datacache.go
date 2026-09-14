package actions

import (
	"sync"
	"time"
)

// Short-TTL in-process caches for expensive read-side aggregates
// (dashboard counters, per-instance sync checksums, annual report tables).
//
// Why TTL instead of pure change-driven invalidation: the register refcache
// (refcache.go) invalidates per observed value, which suits small dropdown
// lists. The aggregates here derive from FULL-table scans of
// consolidated_animals / event_streams, so a webhook batch of N events must
// not trigger N recomputes — and even one immediate recompute would put the
// cost on the ingest path. A short TTL bounds staleness for an internal
// monitoring tool while repeated page loads (the actual hotspot on
// low-power hardware) become cache hits. Batch boundaries (webhook request
// end, consolidation runs, deletions) still invalidate explicitly so data
// is fresh right after an ingest.

const (
	// dashboardCacheTTL bounds staleness of dashboard counters.
	dashboardCacheTTL = 2 * time.Minute
	// annualStatsCacheTTL bounds staleness of annual report tables.
	annualStatsCacheTTL = 2 * time.Minute
	// syncStatusCacheTTL bounds staleness of per-instance sync checksums.
	syncStatusCacheTTL = 5 * time.Minute
)

// dashboardAggregate is everything DashboardIndex reads from the database.
type dashboardAggregate struct {
	TotalAnimals int
	Outcome      outcomeTally
	ByInstance   map[string]int
	TotalEvents  int
	Unprocessed  int
	UniqueInsts  int
	TotalKeys    int
	ActiveKeys   int
}

type dataCaches struct {
	mu        sync.Mutex
	dashboard map[string]*cacheEntry[dashboardAggregate]
	annual    map[string]*cacheEntry[[]annualStatSection]
	syncStat  map[string]*cacheEntry[InstanceSyncStatus]
}

type cacheEntry[T any] struct {
	value   T
	expires time.Time
}

var dataCache = &dataCaches{
	dashboard: map[string]*cacheEntry[dashboardAggregate]{},
	annual:    map[string]*cacheEntry[[]annualStatSection]{},
	syncStat:  map[string]*cacheEntry[InstanceSyncStatus]{},
}

// dataCacheInvalidateAll drops every cached aggregate. Called at ingest
// batch boundaries: end of a webhook request, consolidation runs, event
// deletions, instance purge.
func dataCacheInvalidateAll() {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	dataCache.dashboard = map[string]*cacheEntry[dashboardAggregate]{}
	dataCache.annual = map[string]*cacheEntry[[]annualStatSection]{}
	dataCache.syncStat = map[string]*cacheEntry[InstanceSyncStatus]{}
}

// dataCacheReset is an alias for tests.
func dataCacheReset() { dataCacheInvalidateAll() }

func cacheGet[T any](m map[string]*cacheEntry[T], key string) (T, bool) {
	var zero T
	e, ok := m[key]
	if !ok || time.Now().After(e.expires) {
		return zero, false
	}
	return e.value, true
}

func cachePut[T any](m map[string]*cacheEntry[T], key string, value T, ttl time.Duration) {
	m[key] = &cacheEntry[T]{value: value, expires: time.Now().Add(ttl)}
}

func cachedDashboardAggregate(scope string) (dashboardAggregate, bool) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	return cacheGet(dataCache.dashboard, scope)
}

func storeDashboardAggregate(scope string, agg dashboardAggregate) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	cachePut(dataCache.dashboard, scope, agg, dashboardCacheTTL)
}

func cachedAnnualStats(key string) ([]annualStatSection, bool) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	return cacheGet(dataCache.annual, key)
}

func storeAnnualStats(key string, sections []annualStatSection) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	cachePut(dataCache.annual, key, sections, annualStatsCacheTTL)
}

func cachedSyncStatus(instanceID string) (*InstanceSyncStatus, bool) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	s, ok := cacheGet(dataCache.syncStat, instanceID)
	if !ok {
		return nil, false
	}
	cp := s
	return &cp, true
}

func storeSyncStatus(instanceID string, status *InstanceSyncStatus) {
	dataCache.mu.Lock()
	defer dataCache.mu.Unlock()
	cachePut(dataCache.syncStat, instanceID, *status, syncStatusCacheTTL)
}
