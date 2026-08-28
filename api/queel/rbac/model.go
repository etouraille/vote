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

import (
	"encoding/json"
	"time"
)

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

	// CanEditText allows proposing a change to a text: opening a zone
	// nobody had opened yet, and competing on one already open. Both halves
	// of Repository.ProposeEdit, deliberately one right.
	//
	// They were two — canSelect and canEditSelection — on the theory that
	// opening a zone is more consequential than competing on one. It made
	// the same author welcome on one passage and refused on the next, for a
	// difference no interface ever showed. Where a zone may go is settled
	// by one structural rule instead, and by no privilege: it must not
	// overlap another (see queel.ErrOverlappingSlot).
	CanEditText bool `json:"canEditText"`

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

// UnmarshalJSON reads a stored Permissions, including ones written before
// canSelect and canEditSelection merged into canEditText.
//
// Without it the merge would be quietly destructive: the two old keys would
// simply not match any field any more, and every account that had been
// granted the right to edit would come back unable to. Nothing would fail —
// they would just stop being allowed, with no trace of why.
//
// Either old key grants the new right, since either was enough to reach the
// editor. The next write normalises the row; until then this keeps reading
// it correctly, so no migration has to run before the code ships.
func (p *Permissions) UnmarshalJSON(data []byte) error {
	// The alias sheds this method, or unmarshalling into it would recurse.
	type alias Permissions
	var raw struct {
		alias
		LegacySelect        *bool `json:"canSelect"`
		LegacyEditSelection *bool `json:"canEditSelection"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*p = Permissions(raw.alias)
	if (raw.LegacySelect != nil && *raw.LegacySelect) ||
		(raw.LegacyEditSelection != nil && *raw.LegacyEditSelection) {
		p.CanEditText = true
	}
	return nil
}

// PermBit is one bit in a Unix-style permission mask — a compact encoding
// of Permissions meant to travel inside an auth token (a JWT claim, say) so
// that a caller who already trusts the token doesn't need to round-trip to
// queel's rbac socket on every request just to know what a user may do.
// uint16 rather than uint8: the mask travels as a JSON number in a token,
// never as a byte, so widening it costs nothing and no already-signed token
// reads differently. Eight positions were nearly spent — six in use and one
// retired — and running out would have forced either a reuse of the retired
// position or a change of representation, both under pressure.
type PermBit uint16

// Written as explicit positions rather than 1 << iota. When canSelect and
// canEditSelection merged into CanEditText, dropping one from an iota block
// would have shifted every bit above it down by one — and a token already
// issued would then have decoded as a different set of rights entirely.
// Freezing the positions keeps every mask ever signed readable; 1 << 4 and
// 1 << 5 are simply retired, never reassigned. Of the sixteen PermBit now
// offers, five are in use, two are retired, and nine are free.
const (
	PermVote       PermBit = 1 << 0
	PermCreateText PermBit = 1 << 1
	PermCloseText  PermBit = 1 << 2
	PermEditText PermBit = 1 << 3
	// 1 << 4 was PermEditSelection, merged into PermEditText above.
	// 1 << 5 was PermUpdateText, removed with the route it guarded.
	PermSubscribe PermBit = 1 << 6
)

// Bits packs p into a mask, one bit per permission — see PermBit.
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
	if p.CanEditText {
		b |= PermEditText
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
		CanEditText:      b&PermEditText != 0,
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
	ActionEditText      Action = "editText"
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
	case ActionEditText:
		return u.Permissions.CanEditText
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
	case ActionEditText:
		return b&PermEditText != 0
	case ActionSubscribe:
		return b&PermSubscribe != 0
	default:
		return false
	}
}
