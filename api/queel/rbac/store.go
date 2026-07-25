package rbac

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrAlreadyExists = errors.New("user already exists")
)

// Store is the in-memory directory of Users, backed by a single flat JSON
// file on disk. Every write re-serializes the whole directory and replaces
// the file atomically (write to a temp file, then rename) — simple and
// crash-safe, and the file stays a plain, human-readable list you can
// inspect or hand-edit even if nothing else in queel is running.
type Store struct {
	path string

	mu    sync.RWMutex
	users map[string]*User
}

// Open loads the directory from path, creating an empty one if the file
// doesn't exist yet — it's created on the first write, not here.
func Open(path string) (*Store, error) {
	s := &Store{path: path, users: make(map[string]*User)}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}

	var users []*User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, err
	}
	for _, u := range users {
		s.users[u.ID] = u
	}
	return s, nil
}

// persist rewrites the flat file from the current in-memory state. Callers
// must hold s.mu (write lock).
func (s *Store) persist() error {
	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })

	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}

	if dir := filepath.Dir(s.path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// CreateUser adds a new user with a freshly generated UUID and persists the
// directory immediately.
func (s *Store) CreateUser(root bool, perms Permissions) (*User, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	return s.createUser(id, root, perms)
}

// CreateUserWithID adds a new user under a caller-chosen ID rather than a
// freshly generated one. It exists for bootstrapping: an operator launching
// queel for the first time needs one pre-existing root UUID (e.g. via
// QUEEL_ROOT_UUID) to hand to whatever will assign every other user's
// rights, before any other rbac user exists to do that assigning.
func (s *Store) CreateUserWithID(id string, root bool, perms Permissions) (*User, error) {
	return s.createUser(id, root, perms)
}

func (s *Store) createUser(id string, root bool, perms Permissions) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[id]; exists {
		return nil, ErrAlreadyExists
	}

	user := &User{ID: id, Root: root, Permissions: perms, CreatedAt: time.Now()}
	s.users[id] = user
	if err := s.persist(); err != nil {
		delete(s.users, id)
		return nil, err
	}
	return user, nil
}

// GetUser fetches a single user by ID.
func (s *Store) GetUser(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	copied := *user
	return &copied, nil
}

// ListUsers returns every user in the directory, oldest first.
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		copied := *u
		users = append(users, &copied)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	return users
}

// UpdateUser replaces an existing user's Root flag and Permissions.
func (s *Store) UpdateUser(id string, root bool, perms Permissions) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}

	previous := *user
	user.Root = root
	user.Permissions = perms
	if err := s.persist(); err != nil {
		*user = previous
		return nil, err
	}

	copied := *user
	return &copied, nil
}

// DeleteUser removes a user from the directory.
func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return ErrNotFound
	}

	delete(s.users, id)
	if err := s.persist(); err != nil {
		s.users[id] = user
		return err
	}
	return nil
}
