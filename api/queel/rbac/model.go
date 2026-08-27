// Package rbac is queel's user/role directory: who is allowed to vote,
// create texts, close a round, open a selection, or propose content for
// one. It is deliberately independent of queel's own LSM-tree Store — the
// directory lives in its own small SQLite database (see Store), administered
// over a local-only Unix socket, so that "who can do what" stays simple to
// inspect (any sqlite3 client works) and reason about even if the storage
// engine itself is unavailable. SQLite specifically — rather than an
// in-memory map with a hand-rolled flat file behind it, this package's
// original design — because several api processes sharing one
// QUEEL_RBAC_PATH (several cluster nodes on one machine, e.g.) need real
// transactions and locking to do that safely.
package rbac

import "time"

// Permissions are the individual actions a non-root user may be granted.
// A root User bypasses all of them — see User.Can.
type Permissions struct {
	// CanVote allows casting a vote for a fragment (Repository.CastVote).
	CanVote bool `json:"canVote"`

	// CanCreateText allows creating a brand new text (Repository.CreateText).
	CanCreateText bool `json:"canCreateText"`

	// CanCloseText allows closing the current voting round on a text,
	// finalizing it (Repository.CloseRound).
	CanCloseText bool `json:"canCloseText"`

	// CanSelect allows carving out a new slot — selecting a [start, end)
	// range of a text that doesn't already overlap an open one. This is the
	// more consequential half of Repository.ProposeEdit: it locks in a
	// range every competing proposal for that slot must then respect.
	CanSelect bool `json:"canSelect"`

	// CanEditSelection allows proposing content for a slot, whether newly
	// selected or already open — the other half of Repository.ProposeEdit.
	CanEditSelection bool `json:"canEditSelection"`

	// CanUpdateText allows Repository.UpdateText — overwriting a text's
	// content directly, bypassing the voting workflow entirely. Kept
	// separate from the others because it's a strictly more powerful,
	// vote-bypassing action than any of them.
	CanUpdateText bool `json:"canUpdateText"`

	// CanSubscribe allows following a text (Repository.Subscribe).
	//
	// Unlike the others this gates no change to any text: a subscription
	// is a personal focus signal. It matters because following a text is
	// what surfaces its vote/edit/close actions in the front ends, and
	// what puts someone on the notification fan-out's recipient list — so
	// withholding it is how an install keeps a user read-only without
	// having to revoke each acting permission one by one.
	CanSubscribe bool `json:"canSubscribe"`
}

// PermBit is one bit in a Unix-style permission mask — a compact encoding
// of Permissions meant to travel inside an auth token (a JWT claim, say) so
// that a caller who already trusts the token doesn't need to round-trip to
// queel's rbac socket on every request just to know what a user may do.
type PermBit uint8

const (
	PermVote PermBit = 1 << iota
	PermCreateText
	PermCloseText
	PermSelect
	PermEditSelection
	PermUpdateText
	PermSubscribe
)

// Bits packs p into a single byte, one bit per permission — see PermBit.
func (p Permissions) Bits() PermBit {
	var b PermBit
	if p.CanVote {
		b |= PermVote
	}
	if p.CanCreateText {
		b |= PermCreateText
	}
	if p.CanCloseText {
		b |= PermCloseText
	}
	if p.CanSelect {
		b |= PermSelect
	}
	if p.CanEditSelection {
		b |= PermEditSelection
	}
	if p.CanUpdateText {
		b |= PermUpdateText
	}
	if p.CanSubscribe {
		b |= PermSubscribe
	}
	return b
}

// PermissionsFromBits unpacks a mask produced by Permissions.Bits.
func PermissionsFromBits(b PermBit) Permissions {
	return Permissions{
		CanVote:          b&PermVote != 0,
		CanCreateText:    b&PermCreateText != 0,
		CanCloseText:     b&PermCloseText != 0,
		CanSelect:        b&PermSelect != 0,
		CanEditSelection: b&PermEditSelection != 0,
		CanUpdateText:    b&PermUpdateText != 0,
		CanSubscribe:     b&PermSubscribe != 0,
	}
}

// Action identifies one of the operations a User.Can check can be asked
// about — named after the Permissions field it maps to, not the underlying
// Repository method, so callers don't need to know queel's internals to use
// this package.
type Action string

const (
	ActionVote          Action = "vote"
	ActionCreateText    Action = "createText"
	ActionCloseText     Action = "closeText"
	ActionSelect        Action = "select"
	ActionEditSelection Action = "editSelection"
	ActionUpdateText    Action = "updateText"
	ActionSubscribe     Action = "subscribe"
)

// User is one entry in the flat-file directory, identified by a UUID
// assigned at creation.
type User struct {
	ID          string      `json:"id"`
	Root        bool        `json:"root"`
	Permissions Permissions `json:"permissions"`
	CreatedAt   time.Time   `json:"createdAt"`
}

// Can reports whether this user is allowed to perform action. A root user
// can do everything, unconditionally — that's the whole point of Root.
func (u *User) Can(action Action) bool {
	if u.Root {
		return true
	}
	switch action {
	case ActionVote:
		return u.Permissions.CanVote
	case ActionCreateText:
		return u.Permissions.CanCreateText
	case ActionCloseText:
		return u.Permissions.CanCloseText
	case ActionSelect:
		return u.Permissions.CanSelect
	case ActionEditSelection:
		return u.Permissions.CanEditSelection
	case ActionUpdateText:
		return u.Permissions.CanUpdateText
	case ActionSubscribe:
		return u.Permissions.CanSubscribe
	default:
		return false
	}
}

// Allows reports whether a bitmask grants action — the same mapping as
// User.Can's non-root branch, for callers (e.g. the HTTP API's token
// verification) that only hold the compact mask, not a full Permissions
// struct. Root is a separate claim; it isn't representable as a PermBit.
func (b PermBit) Allows(action Action) bool {
	switch action {
	case ActionVote:
		return b&PermVote != 0
	case ActionCreateText:
		return b&PermCreateText != 0
	case ActionCloseText:
		return b&PermCloseText != 0
	case ActionSelect:
		return b&PermSelect != 0
	case ActionEditSelection:
		return b&PermEditSelection != 0
	case ActionUpdateText:
		return b&PermUpdateText != 0
	case ActionSubscribe:
		return b&PermSubscribe != 0
	default:
		return false
	}
}
