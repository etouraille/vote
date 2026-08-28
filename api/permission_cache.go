package main

import (
	"sync"
	"time"

	"github.com/etouraille/queel/rbac"
)

// maxCachedPermissions bounds the map so a long-lived process can't grow it
// without limit as accounts come and go. Far above any plausible number of
// people signed in at once here; the cap exists so the structure has one,
// not because it is expected to be reached.
//
// Evicting an entry costs nothing worse than the fallback that preceded
// this cache: the token, which is still there underneath.
const maxCachedPermissions = 10000

// permissionCache remembers the rights last read successfully for a user,
// so an unreachable directory degrades to something recent rather than to
// something arbitrarily old.
//
// It exists for one moment only: when requireToken cannot reach the rbac
// directory. Before it, the fallback was the caller's own token — issued
// at sign-in and never revisited, so up to a full session old. A cache
// entry is by construction no older than that: it was written by a
// successful read during this process's life, which is necessarily after
// the token was signed.
//
// Deliberately not a read-through cache. Every request still asks the
// directory, so a grant or a revocation takes effect at once; this is
// consulted only when the answer never came. Turning it into a read-through
// cache would trade that immediacy away for a saving nobody has asked for.
type permissionCache struct {
	mu      sync.RWMutex
	entries map[string]cachedRights
}

type cachedRights struct {
	perms rbac.Permissions
	root  bool

	// storedAt is what eviction sorts on, and the only reason the time is
	// kept: nothing reads it to decide whether an entry is still usable.
	// An entry has no expiry on purpose — a cached value stays fresher
	// than the token for as long as the outage lasts, and expiring it
	// would hand the request back to the very fallback this replaces,
	// precisely when the outage has gone on long enough to matter.
	storedAt time.Time
}

func newPermissionCache() *permissionCache {
	return &permissionCache{entries: make(map[string]cachedRights)}
}

// remember records rights read straight from the directory. Called on every
// successful read, so the entry tracks the account rather than drifting.
func (c *permissionCache) remember(userID string, perms rbac.Permissions, root bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= maxCachedPermissions {
		if _, present := c.entries[userID]; !present {
			c.evictOldestLocked()
		}
	}
	c.entries[userID] = cachedRights{perms: perms, root: root, storedAt: time.Now()}
}

// recall returns the last rights read for a user, and whether any were.
func (c *permissionCache) recall(userID string) (rbac.Permissions, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.entries[userID]
	return entry.perms, entry.root, found
}

// evictOldestLocked drops the least recently refreshed entry. A linear scan
// rather than a heap: it runs only at the cap, and a structure to make this
// fast would cost more to maintain on every write than it saves on the
// write that overflows.
func (c *permissionCache) evictOldestLocked() {
	var oldestID string
	var oldestAt time.Time

	for id, entry := range c.entries {
		if oldestID == "" || entry.storedAt.Before(oldestAt) {
			oldestID, oldestAt = id, entry.storedAt
		}
	}
	delete(c.entries, oldestID)
}
