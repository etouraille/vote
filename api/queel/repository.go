package queel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrNoOpenRound = errors.New("no open round for this text")
)

// SeedAuthorID marks the fragment automatically created from a text's
// current content when a slot is opened on it — the "no change" baseline
// every proposal for that slot competes against.
const SeedAuthorID = "seed"

// WriteOp is one write within a WriteBatch: a Put if Tombstone is false, a
// Delete if it's true (Value is ignored for deletes).
type WriteOp struct {
	Key       []byte
	Value     []byte
	Tombstone bool
}

// Store is what Repository needs from its storage layer. *Engine satisfies
// it directly for a single local node; cluster.DistributedStore adapts a
// cluster.Coordinator to it too, so the exact same Repository — and all the
// domain logic in this file — runs unchanged whether it's backed by one
// machine or a replicated, quorum-consistent cluster.
type Store interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, bool, error)
	Delete(key []byte) error
	Scan(prefix []byte) ([]KV, error)

	// WriteBatch applies several independent writes together. Over a local
	// Engine this is just a loop; over a distributed Store, batching several
	// keys destined for the same node into one request is what makes
	// Repository operations that touch multiple keys cost roughly one round
	// trip per node instead of one per key.
	WriteBatch(ops []WriteOp) error
}

// Repository implements the text/round/fragment/vote domain model on top of
// a Store.
//
// Fragments are stored twice on purpose: once under their own ID so a vote
// (which only carries a fragment ID, per the /vote/{fragmentId,userId}
// shape) can look one up directly, and once under an index key namespaced by
// text+slot so "list the fragments competing for this slot" is a cheap
// prefix Scan instead of a full scan of every fragment ever created.
type Repository struct {
	store Store
}

func NewRepository(store Store) *Repository {
	return &Repository{store: store}
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func textKey(id string) []byte { return []byte("text/" + id) }

func textPrefix() []byte { return []byte("text/") }

func roundKey(id string) []byte { return []byte("round/" + id) }

func currentRoundKey(textID string) []byte { return []byte("currentround/" + textID) }

// roundCountKey stores how many rounds have ever been opened on textID, as
// a decimal string — the source of truth for Round.Number, incremented each
// time openRound runs.
func roundCountKey(textID string) []byte { return []byte("roundcount/" + textID) }

func fragmentKey(id string) []byte { return []byte("fragment/" + id) }

func fragmentIndexKey(textID, slotID, fragmentID string) []byte {
	return []byte(fmt.Sprintf("fragmentindex/%s/%s/%s", textID, slotID, fragmentID))
}

func fragmentIndexPrefix(textID, slotID string) []byte {
	return []byte(fmt.Sprintf("fragmentindex/%s/%s/", textID, slotID))
}

func voteKey(fragmentID, userID string) []byte {
	return []byte(fmt.Sprintf("vote/%s/%s", fragmentID, userID))
}

func votePrefix(fragmentID string) []byte {
	return []byte(fmt.Sprintf("vote/%s/", fragmentID))
}

func userChoiceKey(textID, slotID, userID string) []byte {
	return []byte(fmt.Sprintf("uservote/%s/%s/%s", textID, slotID, userID))
}

// CreateText creates a new text from its initial content. It starts with no
// slots and no open round; slots only come into existence once someone
// calls ProposeEdit.
func (r *Repository) CreateText(title, content string) (*Text, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	text := &Text{ID: id, Title: title, Content: content, CreatedAt: time.Now()}

	payload, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	if err := r.store.Put(textKey(id), payload); err != nil {
		return nil, err
	}
	return text, nil
}

// Text fetches a text by ID.
func (r *Repository) Text(id string) (*Text, error) {
	value, found, err := r.store.Get(textKey(id))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	var text Text
	if err := json.Unmarshal(value, &text); err != nil {
		return nil, err
	}
	return &text, nil
}

// RecentTexts returns up to limit texts (every version of every text —
// including versions CloseRound has since superseded, since each fork is
// its own Text record), most recently created first. limit <= 0 means no
// cap.
func (r *Repository) RecentTexts(limit int) ([]*Text, error) {
	kvs, err := r.store.Scan(textPrefix())
	if err != nil {
		return nil, err
	}

	texts := make([]*Text, 0, len(kvs))
	for _, kv := range kvs {
		var text Text
		if err := json.Unmarshal(kv.Value, &text); err != nil {
			return nil, err
		}
		texts = append(texts, &text)
	}

	sort.Slice(texts, func(i, j int) bool { return texts[i].CreatedAt.After(texts[j].CreatedAt) })
	if limit > 0 && len(texts) > limit {
		texts = texts[:limit]
	}
	return texts, nil
}

// UpdateText overwrites a text's title and content directly, addressed only
// by its ID. Unlike ProposeEdit/CastVote/CloseRound, it never touches slots,
// fragments, or the current round — it's a plain, immediate write for
// editing a text outside of the voting workflow entirely, whether or not a
// round happens to be open on it.
func (r *Repository) UpdateText(id, title, content string) (*Text, error) {
	text, err := r.Text(id)
	if err != nil {
		return nil, err
	}

	text.Title = title
	text.Content = content

	payload, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	if err := r.store.Put(textKey(id), payload); err != nil {
		return nil, err
	}
	return text, nil
}

// Round fetches a round by ID.
func (r *Repository) Round(id string) (*Round, error) {
	value, found, err := r.store.Get(roundKey(id))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	var round Round
	if err := json.Unmarshal(value, &round); err != nil {
		return nil, err
	}
	return &round, nil
}

// CurrentRound returns the open round for a text, if any.
func (r *Repository) CurrentRound(textID string) (*Round, error) {
	value, found, err := r.store.Get(currentRoundKey(textID))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	return r.Round(string(value))
}

// TextWithSlots fetches a text together with the slots of its current
// round, if any. A text with no open round (never edited yet, or its last
// round already closed) isn't an error case here — it's reported as
// RoundNumber 0 with an empty Slots list.
func (r *Repository) TextWithSlots(id string) (*TextWithSlots, error) {
	text, err := r.Text(id)
	if err != nil {
		return nil, err
	}

	round, err := r.CurrentRound(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &TextWithSlots{Text: text, Slots: []Slot{}}, nil
		}
		return nil, err
	}

	return &TextWithSlots{Text: text, RoundNumber: round.Number, Slots: round.Slots}, nil
}

func (r *Repository) openRound(textID string) (*Round, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}

	number := 1
	if value, found, err := r.store.Get(roundCountKey(textID)); err != nil {
		return nil, err
	} else if found {
		previous, err := strconv.Atoi(string(value))
		if err != nil {
			return nil, err
		}
		number = previous + 1
	}

	round := &Round{ID: id, TextID: textID, Number: number, Status: RoundStatusOpen, CreatedAt: time.Now()}

	payload, err := json.Marshal(round)
	if err != nil {
		return nil, err
	}
	if err := r.store.WriteBatch([]WriteOp{
		{Key: roundKey(id), Value: payload},
		{Key: currentRoundKey(textID), Value: []byte(id)},
		{Key: roundCountKey(textID), Value: []byte(strconv.Itoa(number))},
	}); err != nil {
		return nil, err
	}
	return round, nil
}

// resolveSlot decides what ProposeEdit should do with a [start,end) range
// against a round's existing slots: reuse the matching slot if the range is
// identical to one already open, reject it if it partially overlaps one, or
// signal that a new slot should be created if it's fully disjoint from all
// of them.
func resolveSlot(existing []Slot, start, end int) (slotID string, isNew bool, err error) {
	for _, s := range existing {
		if s.Start == start && s.End == end {
			return s.ID, false, nil
		}
		if start < s.End && end > s.Start {
			return "", false, fmt.Errorf("range [%d,%d) overlaps existing slot %q [%d,%d)", start, end, s.ID, s.Start, s.End)
		}
	}
	id, err := newID()
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (r *Repository) addFragment(textID, slotID, content, authorID string) (*Fragment, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	fragment := &Fragment{
		ID:        id,
		TextID:    textID,
		SlotID:    slotID,
		Content:   content,
		AuthorID:  authorID,
		CreatedAt: time.Now(),
	}

	payload, err := json.Marshal(fragment)
	if err != nil {
		return nil, err
	}
	if err := r.store.WriteBatch([]WriteOp{
		{Key: fragmentKey(id), Value: payload},
		{Key: fragmentIndexKey(textID, slotID, id), Value: []byte(id)},
	}); err != nil {
		return nil, err
	}
	return fragment, nil
}

// ProposeEdit selects a range [start,end) of the text's current content and
// proposes replacing it with content. A round is opened automatically on the
// first proposal for a text that has none running. If the range exactly
// matches a slot already active in the round, this just adds another
// fragment competing for it; if the range is disjoint from every existing
// slot, a new slot is created and seeded with the untouched original content
// as its first competitor. A range that partially overlaps an existing slot
// is rejected.
func (r *Repository) ProposeEdit(textID string, start, end int, content, authorID string) (*Fragment, error) {
	text, err := r.Text(textID)
	if err != nil {
		return nil, err
	}
	contentLen := len([]rune(text.Content))
	if start < 0 || end <= start || end > contentLen {
		return nil, fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, contentLen)
	}

	round, err := r.CurrentRound(textID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if round, err = r.openRound(textID); err != nil {
			return nil, err
		}
	}

	slotID, isNewSlot, err := resolveSlot(round.Slots, start, end)
	if err != nil {
		return nil, err
	}

	if isNewSlot {
		seedID, err := newID()
		if err != nil {
			return nil, err
		}
		runes := []rune(text.Content)
		seed := &Fragment{
			ID:        seedID,
			TextID:    textID,
			SlotID:    slotID,
			Content:   string(runes[start:end]),
			AuthorID:  SeedAuthorID,
			CreatedAt: time.Now(),
		}
		seedPayload, err := json.Marshal(seed)
		if err != nil {
			return nil, err
		}

		round.Slots = append(round.Slots, Slot{ID: slotID, Start: start, End: end, Round: round.Number})
		roundPayload, err := json.Marshal(round)
		if err != nil {
			return nil, err
		}

		// Seeding the new slot's baseline fragment and persisting the round's
		// updated slot list are independent writes: one round trip covers both.
		if err := r.store.WriteBatch([]WriteOp{
			{Key: fragmentKey(seedID), Value: seedPayload},
			{Key: fragmentIndexKey(textID, slotID, seedID), Value: []byte(seedID)},
			{Key: roundKey(round.ID), Value: roundPayload},
		}); err != nil {
			return nil, err
		}
	}

	return r.addFragment(textID, slotID, content, authorID)
}

// Fragment fetches a single fragment by ID.
func (r *Repository) Fragment(id string) (*Fragment, error) {
	value, found, err := r.store.Get(fragmentKey(id))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	var fragment Fragment
	if err := json.Unmarshal(value, &fragment); err != nil {
		return nil, err
	}
	return &fragment, nil
}

// Fragments lists every candidate fragment proposed for a given slot, via
// the text+slot index rather than scanning all fragments.
func (r *Repository) Fragments(textID, slotID string) ([]*Fragment, error) {
	kvs, err := r.store.Scan(fragmentIndexPrefix(textID, slotID))
	if err != nil {
		return nil, err
	}
	fragments := make([]*Fragment, 0, len(kvs))
	for _, kv := range kvs {
		fragment, err := r.Fragment(string(kv.Value))
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// CastVote records that userID votes for fragmentID. A user only ever has one
// active vote per slot: voting for a different fragment competing for the
// same slot withdraws their previous vote there.
func (r *Repository) CastVote(fragmentID, userID string) error {
	fragment, err := r.Fragment(fragmentID)
	if err != nil {
		return err
	}

	choiceKey := userChoiceKey(fragment.TextID, fragment.SlotID, userID)
	previous, found, err := r.store.Get(choiceKey)
	if err != nil {
		return err
	}

	vote := &Vote{UserID: userID, FragmentID: fragmentID, CreatedAt: time.Now()}
	payload, err := json.Marshal(vote)
	if err != nil {
		return err
	}

	ops := []WriteOp{
		{Key: voteKey(fragmentID, userID), Value: payload},
		{Key: choiceKey, Value: []byte(fragmentID)},
	}
	if found && string(previous) != fragmentID {
		ops = append(ops, WriteOp{Key: voteKey(string(previous), userID), Tombstone: true})
	}
	return r.store.WriteBatch(ops)
}

// VoteCount returns how many users currently have fragmentID as their active
// vote.
func (r *Repository) VoteCount(fragmentID string) (int, error) {
	kvs, err := r.store.Scan(votePrefix(fragmentID))
	if err != nil {
		return 0, err
	}
	return len(kvs), nil
}

type scoredFragment struct {
	fragment *Fragment
	votes    int
}

// isBetter reports whether candidate should replace current as the leading
// fragment: most votes wins; ties go to whichever fragment was proposed
// first; a remaining tie (identical timestamps) falls back to comparing IDs
// so the result is always deterministic.
func isBetter(candidate, current scoredFragment) bool {
	if candidate.votes != current.votes {
		return candidate.votes > current.votes
	}
	if !candidate.fragment.CreatedAt.Equal(current.fragment.CreatedAt) {
		return candidate.fragment.CreatedAt.Before(current.fragment.CreatedAt)
	}
	return candidate.fragment.ID < current.fragment.ID
}

// WinningFragment returns whichever fragment currently has the most votes
// among those competing for a slot. Returns ErrNotFound if no fragment has
// been proposed for that slot at all.
func (r *Repository) WinningFragment(textID, slotID string) (*Fragment, error) {
	fragments, err := r.Fragments(textID, slotID)
	if err != nil {
		return nil, err
	}
	if len(fragments) == 0 {
		return nil, ErrNotFound
	}

	best := scoredFragment{fragment: fragments[0]}
	if best.votes, err = r.VoteCount(fragments[0].ID); err != nil {
		return nil, err
	}

	for _, fragment := range fragments[1:] {
		votes, err := r.VoteCount(fragment.ID)
		if err != nil {
			return nil, err
		}
		candidate := scoredFragment{fragment: fragment, votes: votes}
		if isBetter(candidate, best) {
			best = candidate
		}
	}
	return best.fragment, nil
}

// spliceContent rebuilds content by walking its slots in Start order and
// swapping in each one's winning fragment content in place of its original
// range; whatever falls outside any slot (gaps, before the first slot, after
// the last) is copied through untouched. Winner content can be a different
// length than the range it replaces.
func spliceContent(content string, slots []Slot, winners map[string]*Fragment) string {
	sorted := make([]Slot, len(slots))
	copy(sorted, slots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	runes := []rune(content)
	var b strings.Builder
	cursor := 0
	for _, slot := range sorted {
		b.WriteString(string(runes[cursor:slot.Start]))
		b.WriteString(winners[slot.ID].Content)
		cursor = slot.End
	}
	b.WriteString(string(runes[cursor:]))
	return b.String()
}

// CloseRound finalizes the current voting round of a text: for every slot
// opened during the round, the winning fragment is resolved and spliced
// into a brand new Text forked from the one the round was open on (see
// Text.PreviousTextID) — the original text, and the round/slots/fragments/
// votes that produced the fork, are left untouched as a permanent record.
// The new Text is what any further ProposeEdit calls should target; the old
// one has no open round any more and none will open on it again.
func (r *Repository) CloseRound(textID string) (*RoundOutcome, error) {
	round, err := r.CurrentRound(textID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoOpenRound
		}
		return nil, err
	}

	text, err := r.Text(textID)
	if err != nil {
		return nil, err
	}

	winners := make(map[string]*Fragment, len(round.Slots))
	slotResults := make([]SlotResult, 0, len(round.Slots))
	for _, slot := range round.Slots {
		winner, err := r.WinningFragment(textID, slot.ID)
		if err != nil {
			return nil, err
		}
		votes, err := r.VoteCount(winner.ID)
		if err != nil {
			return nil, err
		}
		winners[slot.ID] = winner
		slotResults = append(slotResults, SlotResult{SlotID: slot.ID, Fragment: winner, Votes: votes})
	}

	newTextID, err := newID()
	if err != nil {
		return nil, err
	}
	newText := &Text{
		ID:             newTextID,
		Title:          text.Title,
		Content:        spliceContent(text.Content, round.Slots, winners),
		Finalized:      true,
		CreatedAt:      time.Now(),
		PreviousTextID: text.ID,
	}
	newTextPayload, err := json.Marshal(newText)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	round.Status = RoundStatusClosed
	round.ClosedAt = &now
	roundPayload, err := json.Marshal(round)
	if err != nil {
		return nil, err
	}

	// Creating the new text, closing the round in place, clearing the old
	// text's "current round" pointer, and seeding the new text's round
	// counter from the round that just closed (so numbering continues
	// across the version chain instead of restarting at 1) are all
	// independent once the winners are resolved: one round trip covers all
	// four. The old text itself is never written to again.
	if err := r.store.WriteBatch([]WriteOp{
		{Key: textKey(newText.ID), Value: newTextPayload},
		{Key: roundKey(round.ID), Value: roundPayload},
		{Key: currentRoundKey(textID), Tombstone: true},
		{Key: roundCountKey(newText.ID), Value: []byte(strconv.Itoa(round.Number))},
	}); err != nil {
		return nil, err
	}

	return &RoundOutcome{Round: round, Text: newText, Slots: slotResults}, nil
}
