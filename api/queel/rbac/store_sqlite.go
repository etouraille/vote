package rbac

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteBackend is Store's default backend — real transactions and file
// locking, instead of this package's very first approach (a flat JSON file,
// rewritten whole on every write, with no coordination beyond an
// in-process mutex). That was never safe for more than one process sharing
// a QUEEL_RBAC_PATH — exactly the situation several nodes on one machine
// are in. SQLite (via the pure-Go, cgo-free modernc.org/sqlite driver, in
// WAL mode) is: concurrent processes on the same machine can safely read
// and write the same file without racing each other or corrupting it.
//
// It stops being safe the moment those processes are on separate machines —
// see Store's package-level Open vs OpenWithStore doc comments.
type sqliteBackend struct {
	db *sql.DB
}

// openSQLiteBackend opens the SQLite database at path, creating it (and its
// directory, and the users table) if this is the first time. Journal mode
// WAL is what makes concurrent access from multiple processes safe: readers
// don't block the writer and vice versa, unlike SQLite's default
// rollback-journal mode. busy_timeout makes a writer that briefly finds the
// database locked by another process retry for up to 5s instead of failing
// immediately.
func openSQLiteBackend(path string) (*sqliteBackend, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows exactly one writer at a time no matter what database/sql
	// does; leaving its default connection pool (which happily opens several
	// connections under concurrent load) just means most callers contend for
	// that one write lock and some fail outright with SQLITE_BUSY once
	// busy_timeout itself is exceeded. Pinning the pool to a single
	// connection makes database/sql queue callers instead of racing them —
	// the standard fix for this with any Go SQLite driver.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id          TEXT PRIMARY KEY,
			root        INTEGER NOT NULL,
			permissions TEXT NOT NULL,
			created_at  TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, err
	}

	return &sqliteBackend{db: db}, nil
}

func (b *sqliteBackend) close() error {
	return b.db.Close()
}

// createUser checks for an existing id and inserts inside one transaction,
// so a concurrent create from another process/goroutine racing on the same
// id can't both succeed — one gets ErrAlreadyExists.
func (b *sqliteBackend) createUser(id string, root bool, perms Permissions) (*User, error) {
	permsJSON, err := json.Marshal(perms)
	if err != nil {
		return nil, err
	}
	user := &User{ID: id, Root: root, Permissions: perms, CreatedAt: time.Now()}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var exists int
	switch err := tx.QueryRow(`SELECT 1 FROM users WHERE id = ?`, id).Scan(&exists); {
	case err == nil:
		return nil, ErrAlreadyExists
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	if _, err := tx.Exec(
		`INSERT INTO users (id, root, permissions, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, boolToInt(user.Root), string(permsJSON), user.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (b *sqliteBackend) getUser(id string) (*User, error) {
	row := b.db.QueryRow(`SELECT id, root, permissions, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// listUsers returns every user in the directory, oldest first. A query
// failure returns nil rather than an error — see Store.ListUsers.
func (b *sqliteBackend) listUsers() []*User {
	rows, err := b.db.Query(`SELECT id, root, permissions, created_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil
		}
		users = append(users, user)
	}
	return users
}

func (b *sqliteBackend) updateUser(id string, root bool, perms Permissions) (*User, error) {
	permsJSON, err := json.Marshal(perms)
	if err != nil {
		return nil, err
	}

	res, err := b.db.Exec(`UPDATE users SET root = ?, permissions = ? WHERE id = ?`, boolToInt(root), string(permsJSON), id)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	return b.getUser(id)
}

func (b *sqliteBackend) deleteUser(id string) error {
	res, err := b.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner is the common subset of *sql.Row and *sql.Rows that scanUser
// needs — the standard library doesn't export a shared interface for it.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*User, error) {
	var (
		user        User
		rootInt     int
		permissions string
		createdAt   string
	)
	if err := row.Scan(&user.ID, &rootInt, &permissions, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	user.Root = rootInt != 0
	if err := json.Unmarshal([]byte(permissions), &user.Permissions); err != nil {
		return nil, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	user.CreatedAt = parsed

	return &user, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
