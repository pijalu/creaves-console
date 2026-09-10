package actions

import (
	"strconv"
	"sync"

	"creaves-console/models"
	"github.com/gobuffalo/pop/v6"
)

// refCache caches the consolidated-register dropdown value sets and their
// localized label maps (bugs.md: 12 auxiliary queries per register render).
//
// Invalidation is data-change driven, not TTL based: every processed event
// reports its payload values (refCacheObservePayload) and any value the cache
// does not yet know drops that field's entry; delete/cleanup/rebuild paths
// drop everything (refCacheInvalidateAll). Cold fields (never built) are
// skipped, so replaying thousands of resync events does not thrash the cache.
//
// In-process by design: the console deploys as a single app container. A
// multi-replica deployment would need a shared invalidation signal (DB
// version row / Redis) before this cache is safe.
type refCache struct {
	mu     sync.RWMutex
	fields map[string]*refFieldEntry
}

// refFieldEntry holds one tracked field's cached data per scope
// ("" = global scope) and per language for labels. knownValues is the
// global-scope value membership set (nil = cold field); it feeds the
// change-driven invalidation checks.
type refFieldEntry struct {
	values      map[string][]string
	labels      map[string]map[string]map[string]string
	knownValues map[string]bool
}

// regRefCache is the process-wide register reference cache.
var regRefCache = &refCache{fields: map[string]*refFieldEntry{}}

// refCacheReset drops all cached entries. Test hook: the SQLite test setup
// swaps databases, so cached builds must never leak between tests.
func refCacheReset() {
	regRefCache.mu.Lock()
	defer regRefCache.mu.Unlock()
	regRefCache.fields = map[string]*refFieldEntry{}
}

// refCacheInvalidateAll drops every cached entry. Used by delete/cleanup and
// rebuild paths, where values can disappear entirely.
func refCacheInvalidateAll() {
	regRefCache.mu.Lock()
	defer regRefCache.mu.Unlock()
	regRefCache.fields = map[string]*refFieldEntry{}
}

// entryLocked returns the field entry; caller must hold mu (read or write).
func (c *refCache) entryLocked(field string) *refFieldEntry {
	return c.fields[field]
}

// refCacheInvalidateField drops one field's cached data.
func refCacheInvalidateField(field string) {
	regRefCache.mu.Lock()
	defer regRefCache.mu.Unlock()
	delete(regRefCache.fields, field)
}

// observeValue drops the field entry when value is unknown to a warm cache.
// Membership is judged against the global value set, populated when the
// global scope is built. A nil set means the field was never built (cold):
// nothing cached needs invalidation.
func (c *refCache) observeValue(field, value string) {
	c.mu.RLock()
	e := c.entryLocked(field)
	if e == nil || e.knownValues == nil {
		c.mu.RUnlock()
		return // cold: nothing cached to invalidate
	}
	_, known := e.knownValues[value]
	c.mu.RUnlock()
	if known {
		return
	}
	refCacheInvalidateField(field)
}

// refCacheObservePayload reports the tracked reference values carried by a
// processed event payload. Unknown values invalidate the affected field.
func refCacheObservePayload(p models.EventPayload) {
	pairs := []struct{ field, value string }{
		{"species", p.Animal.Species},
		{"animal_type", p.Animal.AnimalType},
		{"animal_age", p.Animal.AnimalAge},
		{"discovery_city", p.Discovery.City},
		{"entry_cause", p.Discovery.EntryCause},
		{"outtake_type", p.Outtake.Type},
	}
	for _, pr := range pairs {
		if pr.value == "" {
			continue
		}
		regRefCache.observeValue(pr.field, pr.value)
	}
	if p.Animal.Year != 0 {
		regRefCache.observeValue("year", strconv.Itoa(p.Animal.Year))
	}
}

// refCacheGetValues returns the cached distinct-value list for field+scope,
// building it via build on first use. The rebuild runs under the write lock
// (single-flight: concurrent readers wait for one shared build instead of
// stampeding the database). Build errors are returned uncached — the next
// caller retries. Returned slice is shared — treat as read-only.
func refCacheGetValues(field, scope string, build func() ([]string, error)) ([]string, error) {
	regRefCache.mu.RLock()
	if e := regRefCache.entryLocked(field); e != nil {
		if v, ok := e.values[scope]; ok {
			regRefCache.mu.RUnlock()
			return v, nil
		}
	}
	regRefCache.mu.RUnlock()

	regRefCache.mu.Lock()
	defer regRefCache.mu.Unlock()
	if e := regRefCache.fields[field]; e != nil {
		if v, ok := e.values[scope]; ok {
			return v, nil
		}
	} else {
		regRefCache.fields[field] = &refFieldEntry{
			values:      map[string][]string{},
			labels:      map[string]map[string]map[string]string{},
			knownValues: map[string]bool{},
		}
	}
	v, err := build()
	if err != nil {
		return nil, err
	}
	regRefCache.fields[field].values[scope] = v
	if scope == "" {
		// Global build refreshes the invalidation membership set.
		set := make(map[string]bool, len(v))
		for _, val := range v {
			set[val] = true
		}
		regRefCache.fields[field].knownValues = set
	}
	return v, nil
}

// refCacheGetLabels returns the cached canonical→label map for
// field+scope+lang, building it via build on first use (single-flight).
// Build errors are returned uncached. Returned map is shared — read-only.
func refCacheGetLabels(field, scope, lang string, build func() (map[string]string, error)) (map[string]string, error) {
	regRefCache.mu.RLock()
	if e := regRefCache.entryLocked(field); e != nil {
		if byLang, ok := e.labels[scope]; ok {
			if m, ok := byLang[lang]; ok {
				regRefCache.mu.RUnlock()
				return m, nil
			}
		}
	}
	regRefCache.mu.RUnlock()

	regRefCache.mu.Lock()
	defer regRefCache.mu.Unlock()
	if regRefCache.fields[field] == nil {
		regRefCache.fields[field] = &refFieldEntry{
			values:      map[string][]string{},
			labels:      map[string]map[string]map[string]string{},
			knownValues: map[string]bool{},
		}
	}
	e := regRefCache.fields[field]
	if _, ok := e.labels[scope]; !ok {
		e.labels[scope] = map[string]map[string]string{}
	}
	if m, ok := e.labels[scope][lang]; ok {
		return m, nil
	}
	m, err := build()
	if err != nil {
		return nil, err
	}
	e.labels[scope][lang] = m
	return m, nil
}

// scopeCacheKey is the cache scope key for a report scope
// ("" = global scope).
func scopeCacheKey(scope ReportScope) string {
	if scope.IsGlobal() {
		return ""
	}
	return scope.InstanceID
}

// refCacheDistinctStrings builds the sorted distinct value list for one
// string field under a report scope.
func refCacheDistinctStrings(tx *pop.Connection, scope ReportScope, field, baseWhere string) ([]string, error) {
	where, args := ScopedWhere(scope, baseWhere)
	var rows []struct {
		V string `db:"v"`
	}
	if err := tx.RawQuery("SELECT DISTINCT "+field+" as v FROM consolidated_animals "+where+" ORDER BY v", args...).All(&rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.V)
	}
	return out, nil
}

// refCacheDistinctYears builds the distinct year list, newest first, as
// strings: templates compare them directly against params["year"] (Plush
// string(int) yields "" — bugs.md #5).
func refCacheDistinctYears(tx *pop.Connection, scope ReportScope) ([]string, error) {
	where, args := ScopedWhere(scope, "")
	var rows []struct {
		Year int `db:"year"`
	}
	if err := tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals "+where+" ORDER BY year DESC", args...).All(&rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strconv.Itoa(r.Year))
	}
	return out, nil
}

// refCacheStringField returns the cached distinct values of a string field.
func refCacheStringField(tx *pop.Connection, scope ReportScope, field, baseWhere string) ([]string, error) {
	return refCacheGetValues(field, scopeCacheKey(scope), func() ([]string, error) {
		return refCacheDistinctStrings(tx, scope, field, baseWhere)
	})
}

// refCacheYearsField returns the cached distinct years (as strings).
func refCacheYearsField(tx *pop.Connection, scope ReportScope) ([]string, error) {
	return refCacheGetValues("year", scopeCacheKey(scope), func() ([]string, error) {
		return refCacheDistinctYears(tx, scope)
	})
}

// refCacheLabelsField returns the cached localized labels for a field.
func refCacheLabelsField(tx *pop.Connection, scope ReportScope, field, lang, baseWhere string) (map[string]string, error) {
	return refCacheGetLabels(field, scopeCacheKey(scope), lang, func() (map[string]string, error) {
		return localizedGroupLabels(tx, scope, field, lang, baseWhere, nil)
	})
}
