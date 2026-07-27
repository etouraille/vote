package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/lib/pq"
)

type User struct {
	ID             string
	Email          string
	PasswordHash   string
	ValidationCode *int
	// RbacUUID is this user's ID in queel's rbac directory (see
	// queel/rbac), nil until an admin assigns permissions to them.
	RbacUUID *string
	// Pseudo is an optional display name set at registration; nil if the
	// user never set one, in which case the front falls back to email.
	Pseudo *string
}

var (
	ErrEmailTaken            = errors.New("email already registered")
	ErrUserNotFound          = errors.New("user not found")
	ErrInvalidValidationCode = errors.New("invalid validation code")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateUser registers a new account. pseudo may be empty — stored as NULL,
// not an empty string, so callers can tell "no pseudo set" apart from a
// (rejected before it gets here) blank one.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, validationCode int, pseudo string) (*User, error) {
	id := newRandomHex(8)
	var pseudoPtr *string
	if pseudo != "" {
		pseudoPtr = &pseudo
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password, validation_code, pseudo) VALUES ($1, $2, $3, $4, $5)`,
		id, email, passwordHash, validationCode, pseudoPtr,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, ValidationCode: &validationCode, Pseudo: pseudoPtr}, nil
}

// CreateUserFromGoogle registers a new account for a Google sign-in that
// has no existing match by email (see googleLoginHandler) — auto-confirmed
// since Google already proved this address belongs to them, so
// validation_code stays NULL rather than going through the usual email
// confirmation step. passwordHash should be an unusable random value
// (nobody knows it — see randomUnusablePasswordHash) rather than a real
// password, since Google accounts don't have one.
func (s *Store) CreateUserFromGoogle(ctx context.Context, email, passwordHash, pseudo string) (*User, error) {
	id := newRandomHex(8)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password, validation_code, pseudo) VALUES ($1, $2, $3, NULL, $4)`,
		id, email, passwordHash, pseudo,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, Pseudo: &pseudo}, nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password, validation_code, rbac_uuid, pseudo FROM users WHERE email = $1`, email,
	))
}

// UserByID fetches a single user by their api-issued ID (as opposed to
// UserByEmail's lookup key), for admin operations addressed by ID.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password, validation_code, rbac_uuid, pseudo FROM users WHERE id = $1`, id,
	))
}

// ListUsers returns every registered user, ordered by email — for the
// admin backoffice, which needs the full account list to assign rights to.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, password, validation_code, rbac_uuid, pseudo FROM users ORDER BY email`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user, err := scanUserFields(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows — scanUserFields
// populates a User from either, so UserByEmail/UserByID (single row) and
// ListUsers (many rows) share the same field mapping.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUserFields(row rowScanner) (*User, error) {
	var user User
	var validationCode sql.NullInt32
	var rbacUUID sql.NullString
	var pseudo sql.NullString
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &validationCode, &rbacUUID, &pseudo); err != nil {
		return nil, err
	}
	if validationCode.Valid {
		code := int(validationCode.Int32)
		user.ValidationCode = &code
	}
	if rbacUUID.Valid {
		user.RbacUUID = &rbacUUID.String
	}
	if pseudo.Valid {
		user.Pseudo = &pseudo.String
	}
	return &user, nil
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	user, err := scanUserFields(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

// SetRbacUUID links user id to the given rbac directory UUID, admin-assigned
// the first time permissions are granted to them (see admin.go).
func (s *Store) SetRbacUUID(ctx context.Context, id, rbacUUID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET rbac_uuid = $1 WHERE id = $2`, rbacUUID, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Store) ConfirmUser(ctx context.Context, email string, code int) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET validation_code = NULL WHERE email = $1 AND validation_code = $2`,
		email, code,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrInvalidValidationCode
	}
	return nil
}

func newRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
