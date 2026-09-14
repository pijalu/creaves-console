package actions

import (
	"sync"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
)

// Short-TTL cache for session user lookups (SetCurrentUser runs one Find per
// authenticated request; user records change only through UsersResource, so
// a 60s TTL plus explicit invalidation on update/delete keeps staleness
// invisible in practice).

const userCacheTTL = time.Minute

type userCacheEntry struct {
	user    *models.User // nil = known-missing (deleted user id)
	expires time.Time
}

var userCache = struct {
	mu sync.Mutex
	m  map[string]*userCacheEntry
}{m: map[string]*userCacheEntry{}}

// cachedUserByID resolves a session user id, serving repeat requests from
// the cache. The returned pointer is a per-request copy so handlers mutating
// the user never corrupt the cached record. found=false mirrors a Find miss.
func cachedUserByID(tx *pop.Connection, id string) (u *models.User, found bool, err error) {
	userCache.mu.Lock()
	e, ok := userCache.m[id]
	if ok && time.Now().After(e.expires) {
		delete(userCache.m, id)
		ok = false
	}
	if ok {
		userCache.mu.Unlock()
		if e.user == nil {
			return nil, false, nil
		}
		cp := *e.user
		return &cp, true, nil
	}
	userCache.mu.Unlock()

	user := &models.User{}
	if err := tx.Find(user, id); err != nil {
		// Cache the miss too: a deleted user id must not hit the DB on every
		// request until the session cookie is dropped.
		userCache.mu.Lock()
		userCache.m[id] = &userCacheEntry{user: nil, expires: time.Now().Add(userCacheTTL)}
		userCache.mu.Unlock()
		return nil, false, nil //nolint:nilerr // miss is a control-flow result, not an error
	}

	userCache.mu.Lock()
	userCache.m[id] = &userCacheEntry{user: user, expires: time.Now().Add(userCacheTTL)}
	userCache.mu.Unlock()

	cp := *user
	return &cp, true, nil
}

// userCacheInvalidate drops one cached user (update/delete paths).
func userCacheInvalidate(id string) {
	userCache.mu.Lock()
	delete(userCache.m, id)
	userCache.mu.Unlock()
}

// userCacheReset drops every cached user (tests).
func userCacheReset() {
	userCache.mu.Lock()
	userCache.m = map[string]*userCacheEntry{}
	userCache.mu.Unlock()
}
