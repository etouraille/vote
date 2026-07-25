package queel

import "time"

// Slot is a range [Start, End) of rune offsets into a Text's Content at the
// time a round was opened. Slots within a round never overlap, but unlike a
// full partition they don't have to cover the whole text either — a slot
// only exists where some user actually selected a range to change; anything
// else stays untouched.
type Slot struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`

	// Round is the sequential number (see Round.Number) of the round this
	// slot was opened in — carried on the slot itself, not just its parent
	// Round, so a caller holding a Slot in isolation (e.g. from Fragments,
	// which only has a slot ID) still knows which round it belongs to. Zero
	// on a Slot's zero value, matching Round.Number before any round has
	// ever been opened on a text.
	Round int `json:"round"`
}

// Text is one immutable version of the text being built. Closing a round
// never mutates a Text in place: it forks a brand new one, with a new ID,
// whose Content has the round's winning fragments spliced in — the text
// that was open for that round, and the round/slots/fragments/votes that
// produced the fork, are left exactly as they were, a permanent record of
// that step in the text's history. New proposals after a close must target
// the new Text.ID; the old one no longer has (or ever again gets) an open
// round of its own.
//
// Finalized is set once a Text is created as the outcome of a CloseRound —
// i.e. whenever PreviousTextID is set — and never for a Text created
// directly via CreateText. It marks the text as belonging to the "voted and
// finalized" corpus (e.g. for indexing into a RAG search corpus).
type Text struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Finalized bool      `json:"finalized"`
	CreatedAt time.Time `json:"createdAt"`

	// PreviousTextID is the ID of the Text this one was forked from when its
	// round closed, or "" if this is an original Text created via
	// CreateText — the root of its version chain.
	PreviousTextID string `json:"previousTextId,omitempty"`
}

// RoundStatus is the lifecycle state of a Round.
type RoundStatus string

const (
	RoundStatusOpen   RoundStatus = "open"
	RoundStatusClosed RoundStatus = "closed"
)

// Round is one voting round for a Text: the set of slots opened during it
// (each the result of at least one ProposeEdit call), still being voted on
// while Status is open. A text can only have one open round at a time.
type Round struct {
	ID     string `json:"id"`
	TextID string `json:"textId"`

	// Number is this round's 1-based position among every round ever opened
	// on TextID (first round is 1, second is 2, ...) — a stable, human-sized
	// counter alongside ID's opaque storage key. Never 0 for a real Round;
	// 0 is reserved for "no round has ever been opened on this text yet"
	// (see Slot.Round).
	Number int `json:"number"`

	Status    RoundStatus `json:"status"`
	Slots     []Slot      `json:"slots"`
	CreatedAt time.Time   `json:"createdAt"`
	ClosedAt  *time.Time  `json:"closedAt,omitempty"`
}

// Fragment is one candidate piece of content proposed for a given slot.
// Several fragments can compete for the same slot; votes decide which one
// wins when the round closes. The fragment seeded automatically from the
// text's original content when a slot is opened carries AuthorID ==
// SeedAuthorID.
type Fragment struct {
	ID        string    `json:"id"`
	TextID    string    `json:"textId"`
	SlotID    string    `json:"slotId"`
	Content   string    `json:"content"`
	AuthorID  string    `json:"authorId"`
	CreatedAt time.Time `json:"createdAt"`
}

// Vote records that a user (identified by pseudo or UUID) picked a given
// fragment.
type Vote struct {
	UserID     string    `json:"userId"`
	FragmentID string    `json:"fragmentId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// SlotResult is the outcome of resolving one slot when its round closes: the
// fragment that won its vote, and how many votes it got.
type SlotResult struct {
	SlotID   string    `json:"slotId"`
	Fragment *Fragment `json:"fragment"`
	Votes    int       `json:"votes"`
}

// TextWithSlots joins a Text with the slots of its current round — the two
// live under separate keys (see Repository.TextWithSlots), so this is a
// convenience shape for callers who want both without two round trips.
// RoundNumber is 0 and Slots is empty (never nil) when no round is
// currently open on the text — a valid result, not an error.
type TextWithSlots struct {
	Text        *Text  `json:"text"`
	RoundNumber int    `json:"roundNumber"`
	Slots       []Slot `json:"slots"`
}

// RoundOutcome is what closing a round produces: the now-closed round, the
// new Text forked from it (see Text.PreviousTextID), and how each slot was
// resolved.
type RoundOutcome struct {
	Round *Round       `json:"round"`
	Text  *Text        `json:"text"`
	Slots []SlotResult `json:"slots"`
}
