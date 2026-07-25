package rbac

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/etouraille/queel"
)

// userKeyPrefix namespaces rbac.User records within a shared queel.Store —
// see OpenWithStore. Chosen to look like the rest of queel's own key
// conventions (text/<id>, round/<id>, ...) even though this package is
// otherwise independent of them.
const userKeyPrefix = "rbacuser/"

func userKey(id string) []byte {
	return []byte(userKeyPrefix + id)
}

// kvBackend backs Store with any queel.Store — in practice, whatever the
// caller already built for its domain repository: a local *queel.Engine, or
// a *cluster.DistributedStore in cluster mode. Handing it the same
// DistributedStore the caller's texts/rounds/fragments/votes already go
// through means rbac gets genuine cross-machine replication for free, via
// infrastructure that's actually designed to work over a network — unlike a
// local SQLite file, which stops being safely shareable the moment nodes
// aren't on the same filesystem anymore.
//
// Existence checks are a plain Get before Put/Delete, not a transaction —
// queel.Store has no compare-and-swap primitive to build one on. That's an
// acceptable gap for how these calls are actually made: rbac writes are
// rare, low-traffic admin actions, never a contended hot path. The one
// caller that could plausibly race — several cluster nodes each
// bootstrapping QUEEL_ROOT_UUID at startup — writes identical values
// either way, so even that race is harmless in practice.
type kvBackend struct {
	store queel.Store
}

func newKVBackend(store queel.Store) *kvBackend {
	return &kvBackend{store: store}
}

// close is a no-op: the caller owns store's lifecycle (it's shared with
// whatever domain repository is also using it), so closing it here would
// pull the rug out from under that.
func (b *kvBackend) close() error {
	return nil
}

func (b *kvBackend) createUser(id string, root bool, perms Permissions) (*User, error) {
	if _, found, err := b.store.Get(userKey(id)); err != nil {
		return nil, err
	} else if found {
		return nil, ErrAlreadyExists
	}

	user := &User{ID: id, Root: root, Permissions: perms, CreatedAt: time.Now()}
	payload, err := json.Marshal(user)
	if err != nil {
		return nil, err
	}
	if err := b.store.Put(userKey(id), payload); err != nil {
		return nil, err
	}
	return user, nil
}

func (b *kvBackend) getUser(id string) (*User, error) {
	value, found, err := b.store.Get(userKey(id))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	var user User
	if err := json.Unmarshal(value, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// listUsers returns every user, oldest first. Scan's own key order (by
// userKey, i.e. by UUID) isn't chronological, so this sorts explicitly —
// same guarantee sqliteBackend gives via ORDER BY created_at. A read
// failure returns nil rather than an error — see Store.ListUsers.
func (b *kvBackend) listUsers() []*User {
	kvs, err := b.store.Scan([]byte(userKeyPrefix))
	if err != nil {
		return nil
	}

	users := make([]*User, 0, len(kvs))
	for _, kv := range kvs {
		var user User
		if err := json.Unmarshal(kv.Value, &user); err != nil {
			return nil
		}
		users = append(users, &user)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	return users
}

func (b *kvBackend) updateUser(id string, root bool, perms Permissions) (*User, error) {
	existing, err := b.getUser(id)
	if err != nil {
		return nil, err
	}
	existing.Root = root
	existing.Permissions = perms

	payload, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	if err := b.store.Put(userKey(id), payload); err != nil {
		return nil, err
	}
	return existing, nil
}

func (b *kvBackend) deleteUser(id string) error {
	if _, found, err := b.store.Get(userKey(id)); err != nil {
		return err
	} else if !found {
		return ErrNotFound
	}
	return b.store.Delete(userKey(id))
}
