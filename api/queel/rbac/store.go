package rbac

import (
	"errors"

	"github.com/etouraille/queel"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrAlreadyExists = errors.New("user already exists")
)

// backend is what actually persists Users behind a Store — see Open (SQLite)
// and OpenWithStore (a queel.Store, typically shared with the caller's
// domain repository). Store itself is just a thin dispatcher to whichever
// backend it was built with; every exported method here has an obvious
// same-named counterpart on backend.
type backend interface {
	createUser(id string, root bool, perms Permissions) (*User, error)
	getUser(id string) (*User, error)
	listUsers() []*User
	updateUser(id string, root bool, perms Permissions) (*User, error)
	deleteUser(id string) error
	close() error
}

// Store is the directory of Users. Its persistence is pluggable — see Open
// vs OpenWithStore — but the public API here is identical either way, and
// unchanged from this package's very first (flat-JSON-file) version: every
// caller (api/main.go, queeld, the admin socket) keeps working exactly as
// before no matter which backend sits behind it.
type Store struct {
	backend backend
}

// Open opens (or creates) a SQLite database at path — the default, for a
// single node or several processes sharing one machine's filesystem. See
// queel/rbac's package doc for why SQLite specifically.
//
// This is NOT safe across separate machines: a local file can't be shared
// that way, and putting it on a network filesystem is explicitly unsafe for
// SQLite's WAL mode (its wal-index relies on memory shared between
// processes on one machine, which a network filesystem can't provide) —
// use OpenWithStore instead once a deployment spans more than one machine.
func Open(path string) (*Store, error) {
	b, err := openSQLiteBackend(path)
	if err != nil {
		return nil, err
	}
	return &Store{backend: b}, nil
}

// OpenWithStore backs the rbac directory with store instead of a local
// SQLite file. store is typically the very same queel.Store the caller
// already built for its domain repository (see queel.NewRepository) — a
// *cluster.DistributedStore in cluster mode, most importantly, which means
// rbac reads/writes now go through the same gossip-replicated,
// quorum-consistent mechanism texts/rounds/fragments/votes already use,
// genuinely safe across separate machines rather than assuming a shared
// filesystem. Use this whenever the caller is running in cluster mode (see
// queel/bootstrap); keep using Open for a single, unclustered node.
//
// Store never closes store itself — its Close only ever releases what Open
// would have opened. The caller keeps owning store's lifecycle, since it's
// shared with whatever else is using it.
func OpenWithStore(store queel.Store) *Store {
	return &Store{backend: newKVBackend(store)}
}

// Close releases whatever Open opened. A no-op for a Store built with
// OpenWithStore — see its doc comment.
func (s *Store) Close() error {
	return s.backend.close()
}

// CreateUser adds a new user with a freshly generated UUID.
func (s *Store) CreateUser(root bool, perms Permissions) (*User, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	return s.backend.createUser(id, root, perms)
}

// CreateUserWithID adds a new user under a caller-chosen ID rather than a
// freshly generated one. It exists for bootstrapping: an operator launching
// queel for the first time needs one pre-existing root UUID (e.g. via
// QUEEL_ROOT_UUID) to hand to whatever will assign every other user's
// rights, before any other rbac user exists to do that assigning.
func (s *Store) CreateUserWithID(id string, root bool, perms Permissions) (*User, error) {
	return s.backend.createUser(id, root, perms)
}

// GetUser fetches a single user by ID.
func (s *Store) GetUser(id string) (*User, error) {
	return s.backend.getUser(id)
}

// ListUsers returns every user in the directory, oldest first. A read
// failure returns nil rather than an error, matching this method's
// signature since this package's very first (in-memory-map) version, which
// could never fail to list.
func (s *Store) ListUsers() []*User {
	return s.backend.listUsers()
}

// UpdateUser replaces an existing user's Root flag and Permissions.
func (s *Store) UpdateUser(id string, root bool, perms Permissions) (*User, error) {
	return s.backend.updateUser(id, root, perms)
}

// DeleteUser removes a user from the directory.
func (s *Store) DeleteUser(id string) error {
	return s.backend.deleteUser(id)
}
