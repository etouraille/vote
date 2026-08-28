package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/etouraille/queel/rbac"
)

// TestPermissionCacheTracksTheLatestRead pins that the cache follows the
// directory rather than freezing the first answer it ever saw — otherwise a
// revocation made while the directory was up would be undone by the first
// outage after it.
func TestPermissionCacheTracksTheLatestRead(t *testing.T) {
	cache := newPermissionCache()

	cache.remember("user-1", rbac.Permissions{CanVote: true, CanCreateText: true}, true)
	cache.remember("user-1", rbac.Permissions{CanVote: true}, false)

	perms, root, found := cache.recall("user-1")
	if !found {
		t.Fatal("expected the entry to be there")
	}
	if perms.CanCreateText || root {
		t.Fatalf("recall = %+v root=%v, want only what the later read said", perms, root)
	}
	if !perms.CanVote {
		t.Fatalf("recall = %+v, want the vote right kept", perms)
	}

	if _, _, found := cache.recall("never-seen"); found {
		t.Fatal("an unknown user must not be reported as cached")
	}
}

// TestPermissionCacheEvictsTheOldestAtTheCap covers the bound. Overflowing
// must drop the least recently refreshed entry and keep serving the rest —
// an eviction costs only a fallback to the token, but evicting the wrong
// one, or everything, would cost more.
func TestPermissionCacheEvictsTheOldestAtTheCap(t *testing.T) {
	cache := newPermissionCache()

	// Filled in order, so the first written is the oldest.
	for i := 0; i < maxCachedPermissions; i++ {
		cache.remember(fmt.Sprintf("user-%d", i), rbac.Permissions{CanVote: true}, false)
	}
	if _, _, found := cache.recall("user-0"); !found {
		t.Fatal("the cache must hold exactly its cap before overflowing")
	}

	cache.remember("newcomer", rbac.Permissions{CanVote: true}, false)

	if _, _, found := cache.recall("user-0"); found {
		t.Fatal("the oldest entry must be the one dropped")
	}
	if _, _, found := cache.recall("newcomer"); !found {
		t.Fatal("the entry that caused the overflow must be the one kept")
	}
	if _, _, found := cache.recall("user-1"); !found {
		t.Fatal("overflowing must drop one entry, not clear the cache")
	}

	// Refreshing a user already present must not evict anybody: the entry
	// is replaced, not added.
	cache.remember("user-1", rbac.Permissions{CanVote: true, CanCloseText: true}, false)
	if _, _, found := cache.recall("user-2"); !found {
		t.Fatal("refreshing an existing entry must not evict another")
	}
}

// TestPermissionCacheIsSafeUnderConcurrentUse matters because every request
// touches it: reads and writes race by construction on any server with two
// callers. Run with -race, this is what catches a missing lock.
func TestPermissionCacheIsSafeUnderConcurrentUse(t *testing.T) {
	cache := newPermissionCache()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			cache.remember(fmt.Sprintf("user-%d", i%7), rbac.Permissions{CanVote: true}, false)
		}(i)
		go func(i int) {
			defer wg.Done()
			cache.recall(fmt.Sprintf("user-%d", i%7))
		}(i)
	}
	wg.Wait()
}
