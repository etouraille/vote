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

	// ErrEmptyRound is returned by CloseRound and ScheduleRoundClose for a
	// round in which nobody has proposed anything.
	//
	// It exists because every text now carries an open round from creation
	// onwards (see openRoundOps), so "there is a round" no longer implies
	// "there is something to resolve". Closing an empty one would splice
	// nothing and fork a byte-for-byte copy of the text — a new id, a new
	// entry in the search corpus, and a version chain one step longer, all
	// saying exactly what the previous version already said.
	ErrEmptyRound = errors.New("no proposal has been made in this round")

	// ErrOverlappingSlot is returned by ProposeEdit for a range that
	// partially covers a slot already open in the round.
	//
	// It is the one structural rule on where a zone may be opened: anyone
	// may open one anywhere, but two of them may not share a character.
	// Overlapping ones would make the outcome ambiguous — two winning
	// fragments claiming the same stretch, with no rule saying which one
	// spliceContent should write there.
	//
	// A sentinel rather than a bare message, so callers can tell this
	// client mistake from a genuine failure and say something useful about
	// it instead of relaying rune offsets.
	ErrOverlappingSlot = errors.New("this range overlaps a zone already open in this round")
)

// ErrTextSuperseded is returned by ProposeEdit when TextID has already been
// forked by a previous CloseRound: its content is frozen history now, not
// something a new round should ever be opened on again — SupersededBy is
// the current version to propose edits against instead of TextID.
type ErrTextSuperseded struct {
	TextID       string
	SupersededBy string
}

func (e *ErrTextSuperseded) Error() string {
	return fmt.Sprintf("text %s has already been forked into %s; propose edits there instead", e.TextID, e.SupersededBy)
}

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

func currentRoundPrefix() []byte { return []byte("currentround/") }

// roundCountKey stores how many rounds have ever been opened on textID, as
// a decimal string — the source of truth for Round.Number, incremented each
// time openRound runs.
func roundCountKey(textID string) []byte { return []byte("roundcount/" + textID) }

// supersededByKey stores the ID of the text CloseRound forked textID into,
// once that's happened — the forward pointer Text itself doesn't carry
// (only the fork's own PreviousTextID points backward). Its presence is
// what stops openRound from ever reopening a round on a text a fork has
// already superseded.
func supersededByKey(textID string) []byte { return []byte("supersededby/" + textID) }

func fragmentKey(id string) []byte { return []byte("fragment/" + id) }

func fragmentIndexKey(textID, slotID, fragmentID string) []byte {
	return []byte(fmt.Sprintf("fragmentindex/%s/%s/%s", textID, slotID, fragmentID))
}

func fragmentIndexPrefix(textID, slotID string) []byte {
	return []byte(fmt.Sprintf("fragmentindex/%s/%s/", textID, slotID))
}

// subscriptionKey is the primary record: does userID follow textID. Stored
// alongside subscriptionIndexKey below under a different key namespace, the
// same double-storage fragment/fragmentindex already uses — one for the
// direct "is this user subscribed to this text" check, one for "list every
// text this user is subscribed to" as a cheap prefix Scan instead of a full
// scan of every subscription ever made.
func subscriptionKey(textID, userID string) []byte {
	return []byte(fmt.Sprintf("subscription/%s/%s", textID, userID))
}

// subscriptionPrefix scans the primary record the other way round from
// subscriptionIndexPrefix: every user following one text, rather than every
// text one user follows. Free, because subscriptionKey already leads with
// the text id.
func subscriptionPrefix(textID string) []byte {
	return []byte(fmt.Sprintf("subscription/%s/", textID))
}

func subscriptionIndexKey(userID, textID string) []byte {
	return []byte(fmt.Sprintf("subscriptionindex/%s/%s", userID, textID))
}

func subscriptionIndexPrefix(userID string) []byte {
	return []byte(fmt.Sprintf("subscriptionindex/%s/", userID))
}

func voteKey(fragmentID, userID string) []byte {
	return []byte(fmt.Sprintf("vote/%s/%s", fragmentID, userID))
}

func votePrefix(fragmentID string) []byte {
	return []byte(fmt.Sprintf("vote/%s/", fragmentID))
}

// roundIndexKey lets "every round ever opened on this text" be a prefix
// scan. Rounds are stored under their own id alone (roundKey), which is
// enough to follow currentRoundKey to the open one but leaves closed ones
// unreachable from the text they belong to — the same double-storage
// fragment/fragmentindex already uses, for the same reason.
func roundIndexKey(textID, roundID string) []byte {
	return []byte(fmt.Sprintf("roundindex/%s/%s", textID, roundID))
}

func roundIndexPrefix(textID string) []byte {
	return []byte(fmt.Sprintf("roundindex/%s/", textID))
}

// tagIndexKey lets "every text carrying this label" be a prefix scan. A
// Text stores its own tags, but nothing maps a tag back to the texts that
// bear it — the same double-storage fragment/fragmentindex already uses,
// for the same reason: this store answers Get and prefix Scan, so every
// question worth asking needs its own key.
func tagIndexKey(tag, textID string) []byte {
	return []byte(fmt.Sprintf("tagindex/%s/%s", tag, textID))
}

func tagIndexPrefix(tag string) []byte {
	return []byte(fmt.Sprintf("tagindex/%s/", tag))
}

// allTagsPrefix walks every label ever used. Only for listing them, never
// on a read path that filters by one.
func allTagsPrefix() []byte { return []byte("tagindex/") }

func userChoiceKey(textID, slotID, userID string) []byte {
	return []byte(fmt.Sprintf("uservote/%s/%s/%s", textID, slotID, userID))
}

// userChoicePrefix scans every current choice recorded on one text, all
// slots and all users at once. The key leads with the text id, so the one
// scan is free; who each entry belongs to is its last segment.
func userChoicePrefix(textID string) string {
	return fmt.Sprintf("uservote/%s/", textID)
}

// CreateText creates a new text from its initial content, attributed to
// authorID (see Text.CreatedBy). It starts with no slots and no open round;
// slots only come into existence once someone calls ProposeEdit.
//
// authorID is also subscribed to the new text right away (see Subscribe) —
// otherwise, since subscribing is the only thing that reveals a text's
// vote/edit/close/delete actions, its own author would be immediately
// locked out of every action on the text they just created until they
// separately clicked "Subscribe" on it.
func (r *Repository) CreateText(title, content, authorID string, tags []string) (*Text, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	text := &Text{ID: id, Title: title, Content: content, Tags: tags, CreatedAt: time.Now(), CreatedBy: authorID}

	payload, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}

	sub := &Subscription{UserID: authorID, TextID: id, CreatedAt: text.CreatedAt}
	subPayload, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}

	// Round 1 opens with the text itself: a text with no round is one
	// nobody can propose against or vote on, and there is no step between
	// creating it and it being open for exactly that. It carries no slots
	// until someone selects a range (see openRoundOps).
	roundOps, _, err := openRoundOps(id, 1)
	if err != nil {
		return nil, err
	}

	ops := append([]WriteOp{
		{Key: textKey(id), Value: payload},
		{Key: subscriptionKey(id, authorID), Value: subPayload},
		{Key: subscriptionIndexKey(authorID, id), Value: []byte(id)},
	}, roundOps...)
	for _, tag := range tags {
		ops = append(ops, WriteOp{Key: tagIndexKey(tag, id), Value: []byte(id)})
	}

	if err := r.store.WriteBatch(ops); err != nil {
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

// RecentTexts returns up to limit texts starting after the first offset of
// them, most recently created first, skipping any version a round has
// since superseded (see IsSuperseded) — each fork is its own Text record,
// so without this a text's whole version history would compete for the
// same "recent" slots as its current head. limit <= 0 means no cap;
// offset <= 0 starts from the very beginning. Together they back the home
// page's infinite scroll: each page asks for the next `limit` texts after
// however many it's already loaded, rather than one fixed batch.
func (r *Repository) RecentTexts(limit, offset int) ([]*Text, error) {
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
		superseded, err := r.IsSuperseded(text.ID)
		if err != nil {
			return nil, err
		}
		if superseded {
			continue
		}
		texts = append(texts, &text)
	}

	sort.Slice(texts, func(i, j int) bool { return texts[i].CreatedAt.After(texts[j].CreatedAt) })

	if offset > 0 {
		if offset >= len(texts) {
			return []*Text{}, nil
		}
		texts = texts[offset:]
	}
	if limit > 0 && len(texts) > limit {
		texts = texts[:limit]
	}
	return texts, nil
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

// ScheduleRoundClose records that the current round on textID should close
// itself once closeAt has passed — the "close in N days" alternative to
// calling CloseRound directly. It doesn't touch Status, Slots, or anything
// else about the round: it stays open (competing for its slots, accepting
// votes) exactly as before, just with a due date a background worker (see
// DueScheduledRounds) will eventually act on. Calling this again before
// that happens overwrites the previous ScheduledCloseAt with the new one.
func (r *Repository) ScheduleRoundClose(textID string, closeAt time.Time, userID string) (*Round, error) {
	round, err := r.CurrentRound(textID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoOpenRound
		}
		return nil, err
	}

	// Refused rather than allowed on the chance a proposal arrives before
	// the date: if none does, the worker would find a round it can never
	// close (see ErrEmptyRound) and retry it on every tick, for good.
	if len(round.Slots) == 0 {
		return nil, ErrEmptyRound
	}

	round.ScheduledCloseAt = &closeAt
	round.ScheduledCloseBy = userID
	payload, err := json.Marshal(round)
	if err != nil {
		return nil, err
	}
	if err := r.store.Put(roundKey(round.ID), payload); err != nil {
		return nil, err
	}
	return round, nil
}

// DueScheduledRounds returns every round whose ScheduledCloseAt has passed
// as of now and hasn't been closed yet — what a periodic background worker
// should call CloseRound on next. Scans the currentround/ index (one entry
// per text with a round still open) rather than every round/ ever opened,
// so the cost tracks how many rounds are open right now, not how many have
// ever existed across the app's whole history. The explicit Status check
// below is belt-and-suspenders: WriteBatch only promises to land several
// keys together, not that every Store implementation makes that atomic, so
// a round already flipped to closed elsewhere is excluded here too rather
// than trusted to have vanished from the index already.
func (r *Repository) DueScheduledRounds(now time.Time) ([]*Round, error) {
	kvs, err := r.store.Scan(currentRoundPrefix())
	if err != nil {
		return nil, err
	}

	due := make([]*Round, 0)
	for _, kv := range kvs {
		round, err := r.Round(string(kv.Value))
		if err != nil {
			return nil, err
		}
		if round.Status == RoundStatusOpen && round.ScheduledCloseAt != nil && !now.Before(*round.ScheduledCloseAt) {
			due = append(due, round)
		}
	}
	return due, nil
}

// RoundCount reports how many rounds have ever been opened on textID's
// version chain — the highest round.Number reached, whether that round is
// still open, has since closed, or (via CloseRound's fork) belongs to a
// previous version this text was forked from. 0 means no round has ever
// been opened. Unlike CurrentRound, this never goes back to zero just
// because the most recent round closed — a text's round history doesn't
// disappear the moment nobody's actively voting on it.
func (r *Repository) RoundCount(textID string) (int, error) {
	value, found, err := r.store.Get(roundCountKey(textID))
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return strconv.Atoi(string(value))
}

// IsSuperseded reports whether a round has already closed on textID and
// forked it into a newer version (see Text.PreviousTextID, ErrTextSuperseded)
// — i.e. whether a version of this text with a strictly higher round count
// now exists elsewhere in its chain. Used to filter search results down to
// just the current head of each version chain, hiding whichever earlier
// versions a round has since superseded.
func (r *Repository) IsSuperseded(textID string) (bool, error) {
	_, found, err := r.store.Get(supersededByKey(textID))
	if err != nil {
		return false, err
	}
	return found, nil
}

// Subscribe records that userID wants to follow textID — see IsSubscribed
// and SubscriptionsForUser. Idempotent: subscribing again just refreshes
// CreatedAt. Fails with ErrNotFound rather than silently subscribing to a
// text that doesn't exist.
func (r *Repository) Subscribe(userID, textID string) (*Subscription, error) {
	if _, err := r.Text(textID); err != nil {
		return nil, err
	}

	sub := &Subscription{UserID: userID, TextID: textID, CreatedAt: time.Now()}
	payload, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}
	if err := r.store.WriteBatch([]WriteOp{
		{Key: subscriptionKey(textID, userID), Value: payload},
		{Key: subscriptionIndexKey(userID, textID), Value: []byte(textID)},
	}); err != nil {
		return nil, err
	}
	return sub, nil
}

// Unsubscribe stops userID following textID, removing both the record and
// its per-user index entry — leaving either behind would make the two
// listings disagree about the same subscription.
//
// Idempotent, and deliberately so: unfollowing something you no longer
// follow is the outcome the caller wanted, not an error to handle. It does
// not check the text still exists either, for the same reason — a
// subscription outliving its text is exactly the one you most want to be
// able to drop.
func (r *Repository) Unsubscribe(userID, textID string) error {
	return r.store.WriteBatch([]WriteOp{
		{Key: subscriptionKey(textID, userID), Tombstone: true},
		{Key: subscriptionIndexKey(userID, textID), Tombstone: true},
	})
}

// IsSubscribed reports whether userID currently follows textID.
func (r *Repository) IsSubscribed(userID, textID string) (bool, error) {
	_, found, err := r.store.Get(subscriptionKey(textID, userID))
	if err != nil {
		return false, err
	}
	return found, nil
}

// SubscriptionsForUser lists the ID of every text userID currently follows.
func (r *Repository) SubscriptionsForUser(userID string) ([]string, error) {
	kvs, err := r.store.Scan(subscriptionIndexPrefix(userID))
	if err != nil {
		return nil, err
	}
	textIDs := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		textIDs = append(textIDs, string(kv.Value))
	}
	return textIDs, nil
}

// SubscribersForText lists every user currently following textID — the
// mirror image of SubscriptionsForUser, and what notification fan-out is
// built on: a text's followers are exactly who a change to it concerns.
//
// The user id is read back out of the stored Subscription rather than
// parsed off the key, so the key layout stays an implementation detail of
// the helpers above.
func (r *Repository) SubscribersForText(textID string) ([]string, error) {
	kvs, err := r.store.Scan(subscriptionPrefix(textID))
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		var sub Subscription
		if err := json.Unmarshal(kv.Value, &sub); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, sub.UserID)
	}
	return userIDs, nil
}

// DeleteUserSubscriptions removes every subscription userID ever made —
// both the direct record (subscriptionKey) and its entry in the per-user
// index (subscriptionIndexKey) that listed it. Scanning the index directly
// gives us the exact textID for each one, so unlike DeleteUserVotes this
// doesn't need a full scan of every subscription ever made plus a suffix
// match.
func (r *Repository) DeleteUserSubscriptions(userID string) error {
	kvs, err := r.store.Scan(subscriptionIndexPrefix(userID))
	if err != nil {
		return err
	}

	var ops []WriteOp
	for _, kv := range kvs {
		textID := string(kv.Value)
		ops = append(ops,
			WriteOp{Key: kv.Key, Tombstone: true},
			WriteOp{Key: subscriptionKey(textID, userID), Tombstone: true},
		)
	}
	if len(ops) == 0 {
		return nil
	}
	return r.store.WriteBatch(ops)
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

	// Never nil, as this type's doc promises: a Round that nobody has
	// proposed against yet carries a nil Slots, and callers marshalling
	// this to JSON would get `null` where they were told to expect `[]`.
	slots := round.Slots
	if slots == nil {
		slots = []Slot{}
	}
	return &TextWithSlots{Text: text, RoundNumber: round.Number, Slots: slots}, nil
}

// nextRoundNumber is the number the next round opened on textID will take:
// one past however many have ever been opened on it (see Round.Number).
func (r *Repository) nextRoundNumber(textID string) (int, error) {
	value, found, err := r.store.Get(roundCountKey(textID))
	if err != nil {
		return 0, err
	}
	if !found {
		return 1, nil
	}
	previous, err := strconv.Atoi(string(value))
	if err != nil {
		return 0, err
	}
	return previous + 1, nil
}

// openRoundOps is the writes that open round `number` on textID, without
// performing them.
//
// Separated from openRound so CreateText and CloseRound can fold them into
// the batch they were already writing: both now open a round as part of
// what they do, and a second WriteBatch would leave a window where the text
// exists (or the fork does) with no round on it — briefly on a single node,
// indefinitely if the second write failed.
//
// The round it opens carries no slots. That is a real state, not a
// placeholder: a slot only comes into existence when somebody selects a
// range (see ProposeEdit), so an open round with nothing in it is exactly
// "this text is open for proposals, none made yet".
func openRoundOps(textID string, number int) ([]WriteOp, *Round, error) {
	id, err := newID()
	if err != nil {
		return nil, nil, err
	}

	round := &Round{ID: id, TextID: textID, Number: number, Status: RoundStatusOpen, CreatedAt: time.Now()}
	payload, err := json.Marshal(round)
	if err != nil {
		return nil, nil, err
	}

	return []WriteOp{
		{Key: roundKey(id), Value: payload},
		{Key: currentRoundKey(textID), Value: []byte(id)},
		{Key: roundCountKey(textID), Value: []byte(strconv.Itoa(number))},
		{Key: roundIndexKey(textID, id), Value: []byte(id)},
	}, round, nil
}

func (r *Repository) openRound(textID string) (*Round, error) {
	if supersededBy, found, err := r.store.Get(supersededByKey(textID)); err != nil {
		return nil, err
	} else if found {
		return nil, &ErrTextSuperseded{TextID: textID, SupersededBy: string(supersededBy)}
	}

	number, err := r.nextRoundNumber(textID)
	if err != nil {
		return nil, err
	}

	ops, round, err := openRoundOps(textID, number)
	if err != nil {
		return nil, err
	}
	if err := r.store.WriteBatch(ops); err != nil {
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
			return "", false, fmt.Errorf("%w: [%d,%d) overlaps slot %q [%d,%d)", ErrOverlappingSlot, start, end, s.ID, s.Start, s.End)
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

// TextsByTag returns every current text carrying a label, newest first —
// the same order and the same exclusion RecentTexts applies, since this is
// that listing narrowed rather than a different one.
//
// Superseded versions are skipped: a label follows its text to each fork
// (see CloseRound), so an old version answering here would show a text the
// rest of the app has already moved past.
func (r *Repository) TextsByTag(tag string) ([]*Text, error) {
	kvs, err := r.store.Scan(tagIndexPrefix(tag))
	if err != nil {
		return nil, err
	}

	texts := make([]*Text, 0, len(kvs))
	for _, kv := range kvs {
		text, err := r.Text(string(kv.Value))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Indexed but gone: skipped rather than failing the whole
				// listing over one dangling entry.
				continue
			}
			return nil, err
		}

		superseded, err := r.IsSuperseded(text.ID)
		if err != nil {
			return nil, err
		}
		if !superseded {
			texts = append(texts, text)
		}
	}

	sort.Slice(texts, func(i, j int) bool { return texts[i].CreatedAt.After(texts[j].CreatedAt) })
	return texts, nil
}

// TagCount is one label and how many current texts carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Tags lists every label in use, most used first, then alphabetically so
// the order is stable between calls rather than shifting on ties.
//
// One scan of the whole index — this feeds a list of labels to choose
// from, which is read far less often than the texts themselves, and there
// is no cheaper shape for the question "which labels exist".
//
// Counts only current versions, for the same reason TextsByTag skips
// superseded ones: a label offering three texts that resolve to two is a
// filter nobody can trust.
func (r *Repository) Tags() ([]TagCount, error) {
	kvs, err := r.store.Scan(allTagsPrefix())
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, kv := range kvs {
		tag, _, found := strings.Cut(strings.TrimPrefix(string(kv.Key), string(allTagsPrefix())), "/")
		if !found {
			continue
		}

		superseded, err := r.IsSuperseded(string(kv.Value))
		if err != nil {
			return nil, err
		}
		if !superseded {
			counts[tag]++
		}
	}

	tags := make([]TagCount, 0, len(counts))
	for tag, count := range counts {
		tags = append(tags, TagCount{Tag: tag, Count: count})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Tag < tags[j].Tag
	})
	return tags, nil
}

// UserVotes maps each slot of textID to the fragment userID currently has
// voted for in it. Slots they have not voted in are absent rather than
// present with an empty value, so a caller can tell "no vote" from "voted
// for nothing" — the latter doesn't exist.
//
// One scan of the whole text's choices, filtered here, rather than a Get
// per slot: the key leads with the text id, so a text with twenty slots
// costs one round trip instead of twenty.
//
// This is what lets a client show a vote made in an earlier session — the
// choice has always been recorded (see CastVote), it simply had no way out
// of the store until now.
func (r *Repository) UserVotes(textID, userID string) (map[string]string, error) {
	prefix := userChoicePrefix(textID)
	kvs, err := r.store.Scan([]byte(prefix))
	if err != nil {
		return nil, err
	}

	votes := make(map[string]string)
	for _, kv := range kvs {
		slotID, keyUserID, ok := strings.Cut(strings.TrimPrefix(string(kv.Key), prefix), "/")
		if !ok || keyUserID != userID {
			continue
		}
		votes[slotID] = string(kv.Value)
	}
	return votes, nil
}

// RoundsForText returns every round ever opened on this exact version of a
// text, oldest first — the closed one that forked it into the next version,
// and the open one if it still has any.
//
// "This exact version": closing forks a new Text, so a chain of versions
// has one round each rather than one text with many. Walking the whole
// history means walking the chain (see TextChain) and asking this for each
// link.
//
// Rounds opened before roundIndexKey existed have no index entry and are
// invisible here. They are still in the store under their own id; only
// this listing can't reach them.
func (r *Repository) RoundsForText(textID string) ([]*Round, error) {
	kvs, err := r.store.Scan(roundIndexPrefix(textID))
	if err != nil {
		return nil, err
	}

	rounds := make([]*Round, 0, len(kvs))
	for _, kv := range kvs {
		value, found, err := r.store.Get(roundKey(string(kv.Value)))
		if err != nil {
			return nil, err
		}
		if !found {
			// Indexed but gone: a delete that removed the round without
			// its index entry. Skipped rather than failing the listing.
			continue
		}
		var round Round
		if err := json.Unmarshal(value, &round); err != nil {
			return nil, err
		}
		rounds = append(rounds, &round)
	}

	sort.Slice(rounds, func(i, j int) bool { return rounds[i].Number < rounds[j].Number })
	return rounds, nil
}

// TextChain returns every version of a text in order, from the original
// down to the current one, whichever version's id is passed in.
//
// Both directions are walked: PreviousTextID leads back to the root, and
// supersededby leads forward to the tip. Neither alone is enough, since
// the caller may hold any link of the chain — typically the latest, which
// has no forward pointer, or an old one found in a notification.
func (r *Repository) TextChain(textID string) ([]*Text, error) {
	text, err := r.Text(textID)
	if err != nil {
		return nil, err
	}

	// Backwards to the root, collecting as we go.
	chain := []*Text{text}
	for current := text; current.PreviousTextID != ""; {
		previous, err := r.Text(current.PreviousTextID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// A version deleted out from under the chain: stop rather
				// than fail, what's left is still a true history.
				break
			}
			return nil, err
		}
		chain = append([]*Text{previous}, chain...)
		current = previous
	}

	// Forwards to the tip.
	for current := text; ; {
		value, found, err := r.store.Get(supersededByKey(current.ID))
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		next, err := r.Text(string(value))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				break
			}
			return nil, err
		}
		chain = append(chain, next)
		current = next
	}

	return chain, nil
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

// DeleteUserVotes removes every trace of userID casting a vote: their vote
// records (vote/<fragmentID>/<userID>, whichever fragments they're under —
// there's no index by user, so this is a full scan of both prefixes) and
// their per-slot current-choice pointers (uservote/<textID>/<slotID>/
// <userID>). Called when an account is deleted, so its votes stop counting
// toward WinningFragment instead of persisting under an ID nothing maps to
// any more.
//
// Fragments userID authored (Fragment.AuthorID) are deliberately left in
// place: that's the content itself, still competing in whatever round it
// was proposed for, and removing it out from under an in-progress vote
// would be a much more disruptive change than the account merely ceasing
// to exist.
func (r *Repository) DeleteUserVotes(userID string) error {
	suffix := "/" + userID

	votes, err := r.store.Scan([]byte("vote/"))
	if err != nil {
		return err
	}
	choices, err := r.store.Scan([]byte("uservote/"))
	if err != nil {
		return err
	}

	var ops []WriteOp
	for _, kv := range votes {
		if strings.HasSuffix(string(kv.Key), suffix) {
			ops = append(ops, WriteOp{Key: kv.Key, Tombstone: true})
		}
	}
	for _, kv := range choices {
		if strings.HasSuffix(string(kv.Key), suffix) {
			ops = append(ops, WriteOp{Key: kv.Key, Tombstone: true})
		}
	}
	if len(ops) == 0 {
		return nil
	}
	return r.store.WriteBatch(ops)
}

// DeleteUserFragments removes every fragment userID ever authored — their
// content contribution, as opposed to DeleteUserVotes' votes — along with
// whatever depended on it: its entry in the text+slot index (so
// Fragments/WinningFragment stop seeing it), every vote anyone cast for it
// (now pointing at nothing), and those voters' current-choice pointer for
// that slot, but only if it's still aimed at the fragment being removed —
// they may have already moved their vote elsewhere.
//
// The automatic seed fragment (Fragment.AuthorID == SeedAuthorID) is never
// userID, so it's never touched: a slot always keeps at least its seed,
// same invariant CloseRound/WinningFragment already rely on.
func (r *Repository) DeleteUserFragments(userID string) error {
	kvs, err := r.store.Scan([]byte("fragment/"))
	if err != nil {
		return err
	}

	var ops []WriteOp
	for _, kv := range kvs {
		var fragment Fragment
		if err := json.Unmarshal(kv.Value, &fragment); err != nil {
			return err
		}
		if fragment.AuthorID != userID {
			continue
		}

		ops = append(ops,
			WriteOp{Key: fragmentKey(fragment.ID), Tombstone: true},
			WriteOp{Key: fragmentIndexKey(fragment.TextID, fragment.SlotID, fragment.ID), Tombstone: true},
		)

		votes, err := r.store.Scan(votePrefix(fragment.ID))
		if err != nil {
			return err
		}
		for _, voteKV := range votes {
			ops = append(ops, WriteOp{Key: voteKV.Key, Tombstone: true})

			var vote Vote
			if err := json.Unmarshal(voteKV.Value, &vote); err != nil {
				return err
			}
			choiceKey := userChoiceKey(fragment.TextID, fragment.SlotID, vote.UserID)
			current, found, err := r.store.Get(choiceKey)
			if err != nil {
				return err
			}
			if found && string(current) == fragment.ID {
				ops = append(ops, WriteOp{Key: choiceKey, Tombstone: true})
			}
		}
	}

	if len(ops) == 0 {
		return nil
	}
	return r.store.WriteBatch(ops)
}

// DeleteUserTexts removes every text userID created (Text.CreatedBy) —
// including every text forked from one of theirs via CloseRound, since
// CreatedBy carries forward across a fork rather than switching to whoever
// closed the round, so a single scan-and-filter by CreatedBy already covers
// a version chain's entire history. For each matching text, this is a full
// teardown: the text itself, every round ever opened on it (open or
// closed), every slot (embedded in its round, so no separate cleanup),
// every fragment proposed for any of those slots — including ones authored
// by other users, since the text those fragments belong to is gone — every
// vote cast on those fragments, and every voter's current-choice pointer
// for those slots.
//
// This is a much bigger blast radius than DeleteUserFragments/
// DeleteUserVotes: it removes other people's contributions too, wherever
// they live on a text this user created. That's inherent to actually
// deleting the text rather than just unlinking this user from it.
//
// It returns the IDs of every text it deleted — queel has no notion of a
// search index of its own, so a caller that maintains one (see this repo's
// api/search.go) needs these to purge it too. Skipping that isn't just a
// cosmetic gap: a caller that ignores this return value leaves stale
// entries an end user can still find and click into, 404ing on a text that
// no longer exists.
func (r *Repository) DeleteUserTexts(userID string) ([]string, error) {
	textKVs, err := r.store.Scan(textPrefix())
	if err != nil {
		return nil, err
	}

	var targetIDs []string
	var ops []WriteOp
	for _, kv := range textKVs {
		var text Text
		if err := json.Unmarshal(kv.Value, &text); err != nil {
			return nil, err
		}
		if text.CreatedBy != userID {
			continue
		}
		targetIDs = append(targetIDs, text.ID)
		ops = append(ops,
			WriteOp{Key: textKey(text.ID), Tombstone: true},
			WriteOp{Key: currentRoundKey(text.ID), Tombstone: true},
			WriteOp{Key: roundCountKey(text.ID), Tombstone: true},
			WriteOp{Key: supersededByKey(text.ID), Tombstone: true},
		)
	}
	if len(targetIDs) == 0 {
		return nil, nil
	}

	targets := make(map[string]bool, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = true
	}

	// Rounds aren't indexed by text, so this is a full scan filtered
	// in-memory — same trade-off RecentTexts and the other Delete* methods
	// already make at this codebase's scale.
	roundKVs, err := r.store.Scan([]byte("round/"))
	if err != nil {
		return nil, err
	}
	for _, kv := range roundKVs {
		var round Round
		if err := json.Unmarshal(kv.Value, &round); err != nil {
			return nil, err
		}
		if targets[round.TextID] {
			ops = append(ops,
				WriteOp{Key: roundKey(round.ID), Tombstone: true},
				// The index entry too, or a deleted text leaves rounds
				// listed under it that no longer exist.
				WriteOp{Key: roundIndexKey(round.TextID, round.ID), Tombstone: true})
		}
	}

	for _, textID := range targetIDs {
		// The fragment→slot index is prefixed by textID, and its values are
		// exactly the fragment IDs it points at — scanning it both finds
		// every fragment ever proposed for this text and lets us delete the
		// index entries themselves in the same pass.
		indexKVs, err := r.store.Scan([]byte("fragmentindex/" + textID + "/"))
		if err != nil {
			return nil, err
		}
		for _, kv := range indexKVs {
			fragmentID := string(kv.Value)
			ops = append(ops,
				WriteOp{Key: kv.Key, Tombstone: true},
				WriteOp{Key: fragmentKey(fragmentID), Tombstone: true},
			)

			voteKVs, err := r.store.Scan(votePrefix(fragmentID))
			if err != nil {
				return nil, err
			}
			for _, voteKV := range voteKVs {
				ops = append(ops, WriteOp{Key: voteKV.Key, Tombstone: true})
			}
		}

		uservoteKVs, err := r.store.Scan([]byte("uservote/" + textID + "/"))
		if err != nil {
			return nil, err
		}
		for _, kv := range uservoteKVs {
			ops = append(ops, WriteOp{Key: kv.Key, Tombstone: true})
		}
	}

	if err := r.store.WriteBatch(ops); err != nil {
		return nil, err
	}
	return targetIDs, nil
}

// DeleteText removes a single text outright — itself, every round ever
// opened on it, every fragment proposed for any of those rounds' slots
// (whoever authored them), every vote on those fragments, and every voter's
// current-choice pointer for those slots. Unlike DeleteUserTexts, this
// takes no creator into account: it deletes exactly the ID given, an
// explicit admin action rather than a consequence of removing an account.
// It does not follow supersededByKey to also remove whatever this text was
// later forked into — a fork is its own independent text with its own ID;
// delete it explicitly too if that's the intent.
func (r *Repository) DeleteText(textID string) error {
	text, err := r.Text(textID)
	if err != nil {
		return err
	}

	ops := []WriteOp{
		{Key: textKey(textID), Tombstone: true},
		{Key: currentRoundKey(textID), Tombstone: true},
		{Key: roundCountKey(textID), Tombstone: true},
		{Key: supersededByKey(textID), Tombstone: true},
	}
	// The label index outlives the text otherwise, and filtering by that
	// label would return an id nothing can load.
	for _, tag := range text.Tags {
		ops = append(ops, WriteOp{Key: tagIndexKey(tag, textID), Tombstone: true})
	}

	// Rounds aren't indexed by text, so this is a full scan filtered
	// in-memory — same trade-off RecentTexts and the other Delete* methods
	// already make at this codebase's scale.
	roundKVs, err := r.store.Scan([]byte("round/"))
	if err != nil {
		return err
	}
	for _, kv := range roundKVs {
		var round Round
		if err := json.Unmarshal(kv.Value, &round); err != nil {
			return err
		}
		if round.TextID == textID {
			ops = append(ops,
				WriteOp{Key: roundKey(round.ID), Tombstone: true},
				WriteOp{Key: roundIndexKey(round.TextID, round.ID), Tombstone: true})
		}
	}

	// The fragment→slot index is prefixed by textID, and its values are
	// exactly the fragment IDs it points at — scanning it both finds every
	// fragment ever proposed for this text and lets us delete the index
	// entries themselves in the same pass.
	indexKVs, err := r.store.Scan([]byte("fragmentindex/" + textID + "/"))
	if err != nil {
		return err
	}
	for _, kv := range indexKVs {
		fragmentID := string(kv.Value)
		ops = append(ops,
			WriteOp{Key: kv.Key, Tombstone: true},
			WriteOp{Key: fragmentKey(fragmentID), Tombstone: true},
		)

		voteKVs, err := r.store.Scan(votePrefix(fragmentID))
		if err != nil {
			return err
		}
		for _, voteKV := range voteKVs {
			ops = append(ops, WriteOp{Key: voteKV.Key, Tombstone: true})
		}
	}

	uservoteKVs, err := r.store.Scan([]byte("uservote/" + textID + "/"))
	if err != nil {
		return err
	}
	for _, kv := range uservoteKVs {
		ops = append(ops, WriteOp{Key: kv.Key, Tombstone: true})
	}

	// Every subscription to this text, from whichever users made them —
	// not just the one being cascaded from a deleted account (see
	// DeleteUserSubscriptions) — must go too, or their per-user index would
	// keep pointing at a text that no longer exists. subscriptionKey's
	// prefix is exactly "subscription/<textID>/", so this scan finds all of
	// them; each key's suffix after that prefix is the subscriber's userID,
	// needed to also remove their reverse index entry.
	subscriptionPrefix := "subscription/" + textID + "/"
	subscriptionKVs, err := r.store.Scan([]byte(subscriptionPrefix))
	if err != nil {
		return err
	}
	for _, kv := range subscriptionKVs {
		userID := strings.TrimPrefix(string(kv.Key), subscriptionPrefix)
		ops = append(ops,
			WriteOp{Key: kv.Key, Tombstone: true},
			WriteOp{Key: subscriptionIndexKey(userID, textID), Tombstone: true},
		)
	}

	return r.store.WriteBatch(ops)
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
//
// Every subscriber of the old text is carried forward to the fork: a
// subscription tracks interest in a text's ongoing evolution, not one
// frozen version of it, so leaving it pointed at the old ID would silently
// strand it on a version that's now superseded — invisible to search and
// RecentTexts (see IsSuperseded), and with no round of its own to ever act
// on again. Each subscriber's old subscription/index pair is replaced by
// one pointing at the fork rather than left in place alongside it.
func (r *Repository) CloseRound(textID string) (*RoundOutcome, error) {
	round, err := r.CurrentRound(textID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoOpenRound
		}
		return nil, err
	}

	if len(round.Slots) == 0 {
		return nil, ErrEmptyRound
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
		// Carried over: a round settles how the text is worded, never what
		// it is about.
		Tags:           text.Tags,
		Finalized:      true,
		CreatedAt:      time.Now(),
		PreviousTextID: text.ID,
		CreatedBy:      text.CreatedBy,
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
	// text's "current round" pointer, seeding the new text's round counter
	// from the round that just closed (so numbering continues across the
	// version chain instead of restarting at 1), and marking the old text
	// superseded (so openRound refuses to ever reopen a round on it — see
	// ErrTextSuperseded) are all independent once the winners are resolved:
	// one round trip covers all five. The old text itself is never written
	// to again.
	// The fork opens its own round straight away, so a text is open for
	// proposals from creation to its latest version without a gap — closing
	// a round advances the deliberation rather than ending it. This also
	// writes roundCountKey(newText.ID), which is why the count isn't
	// carried forward here: the new round's own number is the count.
	nextRoundOps, _, err := openRoundOps(newText.ID, round.Number+1)
	if err != nil {
		return nil, err
	}

	ops := append([]WriteOp{
		{Key: textKey(newText.ID), Value: newTextPayload},
		{Key: roundKey(round.ID), Value: roundPayload},
		{Key: currentRoundKey(textID), Tombstone: true},
		{Key: supersededByKey(textID), Value: []byte(newText.ID)},
	}, nextRoundOps...)

	// Moved to the fork, exactly as the subscriptions below are: a label
	// describes the text, and the fork is what the text now is. Left on the
	// old id, the filter would return a version nothing else shows.
	for _, tag := range text.Tags {
		ops = append(ops,
			WriteOp{Key: tagIndexKey(tag, textID), Tombstone: true},
			WriteOp{Key: tagIndexKey(tag, newText.ID), Value: []byte(newText.ID)})
	}

	subscriptionPrefix := "subscription/" + textID + "/"
	subscriberKVs, err := r.store.Scan([]byte(subscriptionPrefix))
	if err != nil {
		return nil, err
	}
	for _, kv := range subscriberKVs {
		userID := strings.TrimPrefix(string(kv.Key), subscriptionPrefix)

		migrated := &Subscription{UserID: userID, TextID: newText.ID, CreatedAt: now}
		migratedPayload, err := json.Marshal(migrated)
		if err != nil {
			return nil, err
		}

		ops = append(ops,
			WriteOp{Key: kv.Key, Tombstone: true},
			WriteOp{Key: subscriptionIndexKey(userID, textID), Tombstone: true},
			WriteOp{Key: subscriptionKey(newText.ID, userID), Value: migratedPayload},
			WriteOp{Key: subscriptionIndexKey(userID, newText.ID), Value: []byte(newText.ID)},
		)
	}

	if err := r.store.WriteBatch(ops); err != nil {
		return nil, err
	}

	return &RoundOutcome{Round: round, Text: newText, Slots: slotResults}, nil
}
