package queel

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	e, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return NewRepository(e)
}

// runeRange finds target inside content and returns its [start,end) range in
// rune offsets, so tests never have to hand-count characters.
func runeRange(content, target string) (start, end int) {
	byteIdx := strings.Index(content, target)
	if byteIdx < 0 {
		panic(fmt.Sprintf("substring %q not found in %q", target, content))
	}
	start = len([]rune(content[:byteIdx]))
	end = start + len([]rune(target))
	return start, end
}

func TestRecentTextsMostRecentFirstAndLimited(t *testing.T) {
	repo := newTestRepository(t)

	var ids []string
	for i := 0; i < 6; i++ {
		text, err := repo.CreateText(fmt.Sprintf("Text %d", i), "content", "creator")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, text.ID)
		time.Sleep(time.Millisecond) // force distinct, increasing CreatedAt
	}

	recent, err := repo.RecentTexts(4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 4 {
		t.Fatalf("expected 4 texts, got %d", len(recent))
	}
	want := []string{ids[5], ids[4], ids[3], ids[2]}
	for i, text := range recent {
		if text.ID != want[i] {
			t.Fatalf("recent[%d].ID = %q, want %q", i, text.ID, want[i])
		}
	}
}

// TestRecentTextsOffsetPaginates backs the home page's infinite scroll:
// fetching page after page with an increasing offset must walk through
// every text exactly once, in the same order a single unpaged call would
// return them, with no gaps or repeats at the page boundaries.
func TestRecentTextsOffsetPaginates(t *testing.T) {
	repo := newTestRepository(t)

	var ids []string
	for i := 0; i < 6; i++ {
		text, err := repo.CreateText(fmt.Sprintf("Text %d", i), "content", "creator")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, text.ID)
		time.Sleep(time.Millisecond)
	}
	want := []string{ids[5], ids[4], ids[3], ids[2], ids[1], ids[0]}

	page1, err := repo.RecentTexts(4, 0)
	if err != nil {
		t.Fatal(err)
	}
	page2, err := repo.RecentTexts(4, 4)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, text := range page1 {
		got = append(got, text.ID)
	}
	for _, text := range page2 {
		got = append(got, text.ID)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d texts across both pages, want %d: %v", len(got), len(want), got)
	}
	for i, id := range got {
		if id != want[i] {
			t.Fatalf("text[%d] = %q, want %q (got %v)", i, id, want[i], got)
		}
	}
}

// TestRecentTextsOffsetPastTheEndIsEmpty makes sure paginating past the
// last text stops cleanly instead of erroring — the infinite-scroll front
// end uses an empty page as its "nothing more to load" signal.
func TestRecentTextsOffsetPastTheEndIsEmpty(t *testing.T) {
	repo := newTestRepository(t)
	if _, err := repo.CreateText("Only one", "content", "creator"); err != nil {
		t.Fatal(err)
	}

	texts, err := repo.RecentTexts(4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 0 {
		t.Fatalf("expected an empty page past the end, got %d texts", len(texts))
	}
}

// TestRecentTextsExcludesSupersededVersions proves RecentTexts backfills
// the limit with older-but-current texts rather than just returning fewer
// results once a superseded version drops out — the same list a "4 most
// recent" home page grid asks for should never surface a stale fork just
// because it happens to sort earlier by CreatedAt.
func TestRecentTextsExcludesSupersededVersions(t *testing.T) {
	repo := newTestRepository(t)

	original, err := repo.CreateText("Original", "Nous le peuple francais.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	start, end := runeRange(original.Content, "francais")
	if _, err := repo.ProposeEdit(original.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	fork := outcome.Text
	time.Sleep(time.Millisecond)

	var otherIDs []string
	for i := 0; i < 3; i++ {
		text, err := repo.CreateText(fmt.Sprintf("Other %d", i), "content", "creator")
		if err != nil {
			t.Fatal(err)
		}
		otherIDs = append(otherIDs, text.ID)
		time.Sleep(time.Millisecond)
	}

	recent, err := repo.RecentTexts(4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 4 {
		t.Fatalf("expected the limit backfilled to 4 texts despite one superseded version existing, got %d: %+v", len(recent), recent)
	}
	for _, text := range recent {
		if text.ID == original.ID {
			t.Fatalf("superseded original text %q must not appear in RecentTexts", original.ID)
		}
	}
	want := []string{otherIDs[2], otherIDs[1], otherIDs[0], fork.ID}
	for i, text := range recent {
		if text.ID != want[i] {
			t.Fatalf("recent[%d].ID = %q, want %q", i, text.ID, want[i])
		}
	}
}

// TestCreateTextOpensAnEmptyFirstRound is the other half of the invariant
// that a text is open for proposals from the moment it exists: round 1 is
// there straight away, and it is empty until someone selects a range.
func TestCreateTextOpensAnEmptyFirstRound(t *testing.T) {
	repo := newTestRepository(t)

	text, err := repo.CreateText("Constitution", "Nous, le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if text.Content != "Nous, le peuple." {
		t.Fatalf("Content = %q", text.Content)
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatalf("expected an open round on a fresh text, got %v", err)
	}
	if round.Number != 1 {
		t.Fatalf("Number = %d, want the first round to be 1", round.Number)
	}
	if len(round.Slots) != 0 {
		t.Fatalf("Slots = %v, want none until somebody proposes", round.Slots)
	}
}

// TestCreateTextSubscribesTheAuthor proves an author never gets locked out
// of their own text's actions by the subscription gate — CreateText must
// subscribe them to it immediately, not leave that to a separate "click
// Subscribe" step.
func TestCreateTextSubscribesTheAuthor(t *testing.T) {
	repo := newTestRepository(t)

	text, err := repo.CreateText("Constitution", "Nous, le peuple.", "alice")
	if err != nil {
		t.Fatal(err)
	}

	subscribed, err := repo.IsSubscribed("alice", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("expected the author to be subscribed to the text they just created")
	}

	subs, err := repo.SubscriptionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0] != text.ID {
		t.Fatalf("SubscriptionsForUser(alice) = %v, want exactly [%q]", subs, text.ID)
	}

	// A second, unrelated author creating their own text must not be
	// subscribed to alice's.
	otherSubscribed, err := repo.IsSubscribed("bob", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherSubscribed {
		t.Fatal("bob must not be subscribed to a text alice created")
	}
}

func TestUpdateTextOverwritesTitleAndContent(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Brouillon", "Nous, le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateText(text.ID, "Constitution", "Nous, le peuple, ordonnons.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != text.ID {
		t.Fatalf("ID changed: got %q, want %q", updated.ID, text.ID)
	}
	if updated.Title != "Constitution" || updated.Content != "Nous, le peuple, ordonnons." {
		t.Fatalf("got Title=%q Content=%q", updated.Title, updated.Content)
	}

	fetched, err := repo.Text(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Title != "Constitution" || fetched.Content != "Nous, le peuple, ordonnons." {
		t.Fatalf("update not persisted: got Title=%q Content=%q", fetched.Title, fetched.Content)
	}
}

func TestUpdateTextUnknownText(t *testing.T) {
	repo := newTestRepository(t)
	if _, err := repo.UpdateText("does-not-exist", "x", "y"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestUpdateTextIgnoresVotingState is the point of the whole method: it's
// addressed only by text ID and must work — and leave the round/slots/votes
// completely untouched — regardless of whether a voting round happens to be
// open on that text.
func TestUpdateTextIgnoresVotingState(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	roundBefore, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateText(text.ID, "Constitution", "Un tout autre contenu."); err != nil {
		t.Fatal(err)
	}

	roundAfter, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundAfter.Slots) != len(roundBefore.Slots) || roundAfter.ID != roundBefore.ID {
		t.Fatalf("UpdateText disturbed the open round: before=%+v after=%+v", roundBefore, roundAfter)
	}

	fetched, err := repo.Text(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Content != "Un tout autre contenu." {
		t.Fatalf("Content = %q", fetched.Content)
	}
}

func TestProposeEditCreatesSlotWithSeedFragment(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(round.Slots) != 1 || round.Slots[0].Start != start || round.Slots[0].End != end {
		t.Fatalf("unexpected round slots: %+v", round.Slots)
	}

	fragments, err := repo.Fragments(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 2 {
		t.Fatalf("expected seed + proposal, got %d fragments", len(fragments))
	}
	var sawSeed, sawProposal bool
	for _, f := range fragments {
		if f.AuthorID == SeedAuthorID && f.Content == "francais" {
			sawSeed = true
		}
		if f.AuthorID == "alice" && f.Content == "français" {
			sawProposal = true
		}
	}
	if !sawSeed || !sawProposal {
		t.Fatalf("expected seed and proposal fragments, got %+v", fragments)
	}
}

func TestProposeEditSameRangeReusesSlot(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")

	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, start, end, "Français", "bob"); err != nil {
		t.Fatal(err)
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(round.Slots) != 1 {
		t.Fatalf("expected a single shared slot, got %d", len(round.Slots))
	}

	fragments, err := repo.Fragments(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 3 { // seed + alice + bob
		t.Fatalf("expected 3 competing fragments, got %d", len(fragments))
	}
}

func TestProposeEditDisjointRangesCoexistInSameRound(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare la republique."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	s1, e1 := runeRange(content, "francais")
	s2, e2 := runeRange(content, "republique")

	if _, err := repo.ProposeEdit(text.ID, s1, e1, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, s2, e2, "République", "bob"); err != nil {
		t.Fatal(err)
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(round.Slots) != 2 {
		t.Fatalf("expected 2 disjoint slots, got %d", len(round.Slots))
	}
}

func TestProposeEditPartiallyOverlappingRangeRejected(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	s1, e1 := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, s1, e1, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	s2, e2 := runeRange(content, "peuple francais")
	if _, err := repo.ProposeEdit(text.ID, s2, e2, "gens français", "bob"); err == nil {
		t.Fatal("expected an error for a partially overlapping range, got none")
	}
}

func TestProposeEditRejectsInvalidRange(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	contentLen := len([]rune(content))

	cases := map[string][2]int{
		"negative start":   {-1, 3},
		"end before start": {5, 2},
		"empty range":      {3, 3},
		"end past length":  {0, contentLen + 1},
	}
	for name, r := range cases {
		if _, err := repo.ProposeEdit(text.ID, r[0], r[1], "x", "alice"); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

func TestProposeEditUnknownText(t *testing.T) {
	repo := newTestRepository(t)
	if _, err := repo.ProposeEdit("does-not-exist", 0, 1, "x", "alice"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProposeEditOnSupersededTextIsRejected is the guard against branching
// history the "cool" text in production surfaced: once a round has closed
// and forked textID into a new version, textID's content is frozen — a
// second, independent round should never be openable on it again, since
// that would silently produce a sibling fork instead of continuing the one
// version chain a reader would expect from the round numbers.
func TestProposeEditOnSupersededTextIsRejected(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	// text.ID has now been superseded by outcome.Text.ID — a second,
	// independent round on the original must be rejected...
	_, err = repo.ProposeEdit(text.ID, 0, 4, "Vous", "bob")
	var superseded *ErrTextSuperseded
	if !errors.As(err, &superseded) {
		t.Fatalf("expected *ErrTextSuperseded proposing an edit on a forked-away text, got %v", err)
	}
	if superseded.TextID != text.ID || superseded.SupersededBy != outcome.Text.ID {
		t.Fatalf("ErrTextSuperseded = %+v, want TextID=%q SupersededBy=%q", superseded, text.ID, outcome.Text.ID)
	}

	// ...while proposing an edit on the fork itself — the actually-current
	// version — must still work fine, same as any other text.
	if _, err := repo.ProposeEdit(outcome.Text.ID, 0, 4, "Vous", "bob"); err != nil {
		t.Fatalf("expected ProposeEdit on the current (forked) version to succeed, got %v", err)
	}
}

func TestProposeEditRespectsRuneBoundaries(t *testing.T) {
	repo := newTestRepository(t)
	content := "Liberté, égalité, fraternité."
	text, err := repo.CreateText("Devise", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "égalité")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "EGALITE", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Content != "EGALITE" {
		t.Fatalf("Content = %q", fragment.Content)
	}

	round, _ := repo.CurrentRound(text.ID)
	seedFragments, err := repo.Fragments(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range seedFragments {
		if f.AuthorID == SeedAuthorID && f.Content != "égalité" {
			t.Fatalf("expected clean rune-boundary seed extraction, got %q", f.Content)
		}
	}
}

func TestWinningFragmentDefaultsToSeedWithNoVotes(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	round, _ := repo.CurrentRound(text.ID)
	winner, err := repo.WinningFragment(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if winner.AuthorID != SeedAuthorID {
		t.Fatalf("expected seed to win with no votes, got %+v", winner)
	}
}

func TestWinningFragmentPicksMostVotes(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	challenger, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(challenger.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(challenger.ID, "user-2"); err != nil {
		t.Fatal(err)
	}

	round, _ := repo.CurrentRound(text.ID)
	winner, err := repo.WinningFragment(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if winner.ID != challenger.ID {
		t.Fatalf("expected the more-voted challenger to win, got %+v", winner)
	}
}

func TestCloseRoundSplicesWinnersAndPreservesGaps(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare la republique unie."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	s1, e1 := runeRange(content, "francais")
	f1, err := repo.ProposeEdit(text.ID, s1, e1, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	s2, e2 := runeRange(content, "declare")
	f2, err := repo.ProposeEdit(text.ID, s2, e2, "proclame solennellement", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f2.ID, "user-2"); err != nil {
		t.Fatal(err)
	}
	// "la republique unie" is left completely untouched: no slot there.

	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	want := "Nous le peuple français proclame solennellement la republique unie."
	if outcome.Text.Content != want {
		t.Fatalf("Content = %q, want %q", outcome.Text.Content, want)
	}
	if outcome.Text.ID == text.ID {
		t.Fatal("expected CloseRound to fork a new text, got the same ID")
	}
	if outcome.Text.PreviousTextID != text.ID {
		t.Fatalf("PreviousTextID = %q, want %q", outcome.Text.PreviousTextID, text.ID)
	}
	if !outcome.Text.Finalized {
		t.Fatal("expected the forked text to be Finalized")
	}
	if outcome.Round.Status != RoundStatusClosed || outcome.Round.ClosedAt == nil {
		t.Fatalf("expected round to be closed, got %+v", outcome.Round)
	}
	if len(outcome.Slots) != 2 {
		t.Fatalf("expected 2 slot results, got %d", len(outcome.Slots))
	}

	if _, err := repo.CurrentRound(text.ID); err != ErrNotFound {
		t.Fatalf("expected no open round after closing, got %v", err)
	}

	original, err := repo.Text(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Content != content {
		t.Fatalf("original text should be untouched: Content = %q, want %q", original.Content, content)
	}
	if original.Finalized {
		t.Fatal("original text should not be marked Finalized")
	}

	got, err := repo.Text(outcome.Text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != want {
		t.Fatalf("persisted Content = %q, want %q", got.Content, want)
	}
}

// TestCloseRoundMigratesSubscriptionsToTheFork proves subscribers of the
// pre-round text follow it to the new version CloseRound forks — replaced,
// not duplicated, so the old (now superseded) text's subscription is gone
// once the fork exists.
func TestCloseRoundMigratesSubscriptionsToTheFork(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Subscribe("alice", text.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("bob", text.ID); err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "français", "carol")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, userID := range []string{"alice", "bob"} {
		subscribedToFork, err := repo.IsSubscribed(userID, outcome.Text.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !subscribedToFork {
			t.Fatalf("%s's subscription must carry over to the fork %s", userID, outcome.Text.ID)
		}

		subscribedToOld, err := repo.IsSubscribed(userID, text.ID)
		if err != nil {
			t.Fatal(err)
		}
		if subscribedToOld {
			t.Fatalf("%s's subscription to the superseded original %s must be gone, not duplicated", userID, text.ID)
		}

		subs, err := repo.SubscriptionsForUser(userID)
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || subs[0] != outcome.Text.ID {
			t.Fatalf("SubscriptionsForUser(%s) = %v, want exactly [%q]", userID, subs, outcome.Text.ID)
		}
	}
}

// TestCloseRoundWithNoSubscribersIsUnaffected makes sure the subscription
// migration added to CloseRound is a genuine no-op — not just harmless —
// when nobody had subscribed to the text a round closes on.
func TestCloseRoundWithNoSubscribersIsUnaffected(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Text.Content == "" {
		t.Fatal("sanity check: fork should still have content")
	}
}

// TestCloseRoundEmptyRound keeps what TestCloseRoundNoOpenRound used to
// protect — a text nobody has proposed anything on cannot be closed — now
// that the reason has changed: the round exists from creation, it is simply
// empty, and closing it would fork a copy saying nothing new.
func TestCloseRoundEmptyRound(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseRound(text.ID); err != ErrEmptyRound {
		t.Fatalf("expected ErrEmptyRound, got %v", err)
	}
}

// TestCloseRoundNoOpenRound covers the case that still has no round at all:
// a text a fork has already superseded, whose currentRound was tombstoned
// by the close that produced the fork.
func TestCloseRoundNoOpenRound(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "peuple")
	if _, err := repo.ProposeEdit(text.ID, start, end, "citoyen", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseRound(text.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CloseRound(text.ID); err != ErrNoOpenRound {
		t.Fatalf("expected ErrNoOpenRound on a superseded text, got %v", err)
	}
}

func TestIsSupersededFalseForANeverClosedText(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	superseded, err := repo.IsSuperseded(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded {
		t.Fatal("a text nobody has ever closed a round on must not be reported superseded")
	}
}

func TestIsSupersededAfterCloseRound(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	superseded, err := repo.IsSuperseded(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !superseded {
		t.Fatal("expected the original text to be superseded once its round closed and forked it")
	}

	// The fork itself is the current head of the chain — not superseded by
	// anything, until a round closes on it too.
	forkSuperseded, err := repo.IsSuperseded(outcome.Text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if forkSuperseded {
		t.Fatal("the newly forked text must not itself be reported superseded")
	}
}

func TestIsSubscribedFalseUntilSubscribed(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}

	subscribed, err := repo.IsSubscribed("alice", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if subscribed {
		t.Fatal("expected alice not to be subscribed before ever calling Subscribe")
	}

	if _, err := repo.Subscribe("alice", text.ID); err != nil {
		t.Fatal(err)
	}
	subscribed, err = repo.IsSubscribed("alice", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("expected alice to be subscribed after calling Subscribe")
	}

	// A different user's subscription (or lack of one) is independent.
	bobSubscribed, err := repo.IsSubscribed("bob", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobSubscribed {
		t.Fatal("bob subscribing was never requested, must not be reported subscribed")
	}
}

func TestSubscribeUnknownText(t *testing.T) {
	repo := newTestRepository(t)
	if _, err := repo.Subscribe("alice", "does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSubscriptionsForUserListsSubscribedTexts(t *testing.T) {
	repo := newTestRepository(t)

	var ids []string
	for i := 0; i < 3; i++ {
		text, err := repo.CreateText(fmt.Sprintf("Text %d", i), "content", "creator")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, text.ID)
	}

	if _, err := repo.Subscribe("alice", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("alice", ids[2]); err != nil {
		t.Fatal(err)
	}
	// ids[1] is never subscribed to, and bob's own subscription must not
	// leak into alice's list.
	if _, err := repo.Subscribe("bob", ids[1]); err != nil {
		t.Fatal(err)
	}

	subscribed, err := repo.SubscriptionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range subscribed {
		got[id] = true
	}
	if len(got) != 2 || !got[ids[0]] || !got[ids[2]] {
		t.Fatalf("SubscriptionsForUser(alice) = %v, want exactly %v", subscribed, []string{ids[0], ids[2]})
	}
}

func TestScheduleRoundCloseSetsFieldWithoutClosing(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	closeAt := time.Now().Add(7 * 24 * time.Hour)
	round, err := repo.ScheduleRoundClose(text.ID, closeAt)
	if err != nil {
		t.Fatal(err)
	}
	if round.Status != RoundStatusOpen {
		t.Fatalf("expected the round to stay open, got status %q", round.Status)
	}
	if round.ScheduledCloseAt == nil || !round.ScheduledCloseAt.Equal(closeAt) {
		t.Fatalf("ScheduledCloseAt = %v, want %v", round.ScheduledCloseAt, closeAt)
	}

	// Still open for proposals — scheduling a future close doesn't freeze
	// anything early.
	otherStart, otherEnd := runeRange(content, "declare")
	if _, err := repo.ProposeEdit(text.ID, otherStart, otherEnd, "proclame", "bob"); err != nil {
		t.Fatalf("expected the round to still accept proposals, got %v", err)
	}

	persisted, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ScheduledCloseAt == nil || !persisted.ScheduledCloseAt.Equal(closeAt) {
		t.Fatalf("persisted ScheduledCloseAt = %v, want %v", persisted.ScheduledCloseAt, closeAt)
	}
}

// Scheduling is refused on an empty round for the same reason closing is,
// plus one of its own: the worker would otherwise inherit a round it can
// never close and retry it on every tick.
func TestScheduleRoundCloseEmptyRound(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(text.ID, time.Now().Add(24*time.Hour)); err != ErrEmptyRound {
		t.Fatalf("expected ErrEmptyRound, got %v", err)
	}
}

func TestDueScheduledRounds(t *testing.T) {
	repo := newTestRepository(t)

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(7 * 24 * time.Hour)

	// Due: an open round scheduled to close in the past.
	dueText, err := repo.CreateText("Due", "Contenu du texte du.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	dStart, dEnd := runeRange(dueText.Content, "du")
	if _, err := repo.ProposeEdit(dueText.ID, dStart, dEnd, "modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(dueText.ID, past); err != nil {
		t.Fatal(err)
	}

	// Not due yet: an open round scheduled far in the future.
	futureText, err := repo.CreateText("Future", "Contenu futur.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	fStart, fEnd := runeRange(futureText.Content, "futur")
	if _, err := repo.ProposeEdit(futureText.ID, fStart, fEnd, "modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(futureText.ID, future); err != nil {
		t.Fatal(err)
	}

	// Never scheduled: an open round with no ScheduledCloseAt at all.
	unscheduledText, err := repo.CreateText("Unscheduled", "Contenu normal.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	uStart, uEnd := runeRange(unscheduledText.Content, "normal")
	if _, err := repo.ProposeEdit(unscheduledText.ID, uStart, uEnd, "modifie", "alice"); err != nil {
		t.Fatal(err)
	}

	// Already closed: was scheduled in the past, but someone closed it by
	// hand before the worker got to it — must not show up as due again.
	closedText, err := repo.CreateText("Closed", "Contenu clos.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	cStart, cEnd := runeRange(closedText.Content, "clos")
	if _, err := repo.ProposeEdit(closedText.ID, cStart, cEnd, "modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(closedText.ID, past); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseRound(closedText.ID); err != nil {
		t.Fatal(err)
	}

	due, err := repo.DueScheduledRounds(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("expected exactly 1 due round, got %d: %+v", len(due), due)
	}
	if due[0].TextID != dueText.ID {
		t.Fatalf("due round TextID = %q, want %q", due[0].TextID, dueText.ID)
	}
}

func TestTextWithSlotsEmptyFirstRound(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.TextWithSlots(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text.ID != text.ID {
		t.Fatalf("Text.ID = %q, want %q", got.Text.ID, text.ID)
	}
	if got.RoundNumber != 1 {
		t.Fatalf("RoundNumber = %d, want 1 — creation opens the first round", got.RoundNumber)
	}
	if got.Slots == nil || len(got.Slots) != 0 {
		t.Fatalf("Slots = %v, want a non-nil empty slice", got.Slots)
	}
}

func TestTextWithSlotsOpenRound(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	got, err := repo.TextWithSlots(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoundNumber != 1 {
		t.Fatalf("RoundNumber = %d, want 1", got.RoundNumber)
	}
	if len(got.Slots) != 1 || got.Slots[0].Start != start || got.Slots[0].End != end {
		t.Fatalf("Slots = %+v, want one slot [%d,%d)", got.Slots, start, end)
	}
	if got.Slots[0].Round != 1 {
		t.Fatalf("Slots[0].Round = %d, want 1", got.Slots[0].Round)
	}
}

func TestTextWithSlotsUnknownText(t *testing.T) {
	repo := newTestRepository(t)
	if _, err := repo.TextWithSlots("does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSecondRoundBuildsOnClosedRoundContent(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	f1, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	firstOutcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	forkedText := firstOutcome.Text.ID
	newContent := firstOutcome.Text.Content // "Nous le peuple français declare."

	// A second round opens on the *forked* text (the first round's ID
	// no longer has an open round of its own), with its own fresh offsets.
	start2, end2 := runeRange(newContent, "declare")
	f2, err := repo.ProposeEdit(forkedText, start2, end2, "proclame", "carol")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f2.ID, "user-2"); err != nil {
		t.Fatal(err)
	}

	round, err := repo.CurrentRound(forkedText)
	if err != nil {
		t.Fatal(err)
	}
	if round.ID == firstOutcome.Round.ID {
		t.Fatalf("expected a distinct new round, got the same one")
	}
	if round.Number != firstOutcome.Round.Number+1 {
		t.Fatalf("Number = %d, want round numbering to continue across the fork at %d", round.Number, firstOutcome.Round.Number+1)
	}

	secondOutcome, err := repo.CloseRound(forkedText)
	if err != nil {
		t.Fatal(err)
	}
	want := "Nous le peuple français proclame."
	if secondOutcome.Text.Content != want {
		t.Fatalf("Content = %q, want %q", secondOutcome.Text.Content, want)
	}
	if secondOutcome.Text.PreviousTextID != forkedText {
		t.Fatalf("PreviousTextID = %q, want %q", secondOutcome.Text.PreviousTextID, forkedText)
	}
}

// A fresh text is at 1, not 0: creation opens its first round. Zero is now
// reachable only for an id no text was ever created under — which is what
// the second half checks, since that is the only reading of "no round ever
// opened" left.
func TestRoundCountOnAFreshTextIsOne(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple francais declare.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	count, err := repo.RoundCount(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("RoundCount on a fresh text = %d, want 1 — creation opens round 1", count)
	}

	count, err = repo.RoundCount("no-such-text")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("RoundCount for an unknown text = %d, want 0", count)
	}
}

// TestRoundCountSurvivesAfterTheRoundCloses is the behavior CurrentRound
// can't give search results: once a round closes, CurrentRound goes back to
// ErrNotFound, but the forked text should still report which round produced
// it rather than looking like it never had one.
func TestRoundCountSurvivesAfterTheRoundCloses(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}

	// While the round is open, RoundCount and CurrentRound.Number agree.
	count, err := repo.RoundCount(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("RoundCount while round 1 is open = %d, want 1", count)
	}

	if err := repo.CastVote(fragment.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The original text's own round is gone (it forked) and the fork opens
	// the next one straight away, so the count continues across the chain
	// rather than restarting — a closed round must never look
	// indistinguishable from "never had a round".
	next, err := repo.CurrentRound(outcome.Text.ID)
	if err != nil {
		t.Fatalf("expected the fork to have its own open round, got err=%v", err)
	}
	if next.Number != 2 {
		t.Fatalf("the fork's round Number = %d, want 2 — round 1 is what produced it", next.Number)
	}
	if len(next.Slots) != 0 {
		t.Fatalf("the fork's round Slots = %v, want none until somebody proposes again", next.Slots)
	}

	// And the old text keeps no open round of its own: it is frozen history.
	if _, err := repo.CurrentRound(text.ID); err != ErrNotFound {
		t.Fatalf("expected no open round left on the superseded text, got err=%v", err)
	}

	count, err = repo.RoundCount(outcome.Text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("RoundCount on the forked text after closing = %d, want 2 (the closed round plus the one just opened)", count)
	}
}

func TestWinningFragmentTieBreaksOnEarliestProposal(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	if _, err := repo.ProposeEdit(text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := repo.ProposeEdit(text.ID, start, end, "Francais", "bob"); err != nil {
		t.Fatal(err)
	}
	// All three (seed, alice, bob) sit at 0 votes: the seed, being earliest, wins.

	round, _ := repo.CurrentRound(text.ID)
	winner, err := repo.WinningFragment(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if winner.AuthorID != SeedAuthorID {
		t.Fatalf("expected seed to win a 0-vote tie, got %+v", winner)
	}
}

func TestCastVoteUnknownFragment(t *testing.T) {
	repo := newTestRepository(t)
	if err := repo.CastVote("does-not-exist", "user-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteUserVotesRemovesOnlyThatUser(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")
	f1, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f1.ID, "user-2"); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteUserVotes("user-1"); err != nil {
		t.Fatal(err)
	}

	votes, err := repo.VoteCount(f1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 1 {
		t.Fatalf("VoteCount after deleting user-1's vote = %d, want 1 (user-2's should remain)", votes)
	}

	// user-1 casting again should behave as a first-time vote (no leftover
	// choice pointer confusing the withdraw-previous-vote logic).
	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	votes, err = repo.VoteCount(f1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 2 {
		t.Fatalf("VoteCount after user-1 re-votes = %d, want 2", votes)
	}
}

func TestDeleteUserVotesNoVotesIsNoop(t *testing.T) {
	repo := newTestRepository(t)
	if err := repo.DeleteUserVotes("nobody"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserFragmentsRemovesOnlyThatAuthorsFragments(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}
	start, end := runeRange(content, "francais")

	alicesFragment, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bobsFragment, err := repo.addFragment(text.ID, alicesFragment.SlotID, "Francais", "bob")
	if err != nil {
		t.Fatal(err)
	}

	// A third user votes for alice's fragment — that vote and the voter's
	// current-choice pointer should both disappear once alice's fragment
	// (and only alice's) is deleted.
	if err := repo.CastVote(alicesFragment.ID, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(bobsFragment.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteUserFragments("alice"); err != nil {
		t.Fatal(err)
	}

	fragments, err := repo.Fragments(text.ID, alicesFragment.SlotID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range fragments {
		ids = append(ids, f.ID)
		if f.AuthorID == "alice" {
			t.Fatalf("alice's fragment %s should have been deleted, still present: %+v", f.ID, ids)
		}
	}
	// Seed + bob's fragment should remain.
	if len(fragments) != 2 {
		t.Fatalf("expected 2 remaining fragments (seed + bob), got %d: %+v", len(fragments), ids)
	}

	if _, err := repo.Fragment(alicesFragment.ID); err != ErrNotFound {
		t.Fatalf("expected alice's fragment to be gone, got err=%v", err)
	}

	// carol's vote for the now-deleted fragment must be gone too.
	votes, err := repo.VoteCount(alicesFragment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 0 {
		t.Fatalf("VoteCount for deleted fragment = %d, want 0", votes)
	}

	// bob's fragment and dave's vote for it are untouched.
	votes, err = repo.VoteCount(bobsFragment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 1 {
		t.Fatalf("VoteCount for bob's fragment = %d, want 1 (dave's vote should remain)", votes)
	}

	// carol voting again for whatever's left of the slot should behave as a
	// first-time vote — no stale choice pointer left over from the deleted
	// fragment confusing the withdraw-previous-vote logic.
	if err := repo.CastVote(bobsFragment.ID, "carol"); err != nil {
		t.Fatal(err)
	}
	votes, err = repo.VoteCount(bobsFragment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 2 {
		t.Fatalf("VoteCount for bob's fragment after carol votes = %d, want 2", votes)
	}
}

func TestDeleteUserFragmentsNoFragmentsIsNoop(t *testing.T) {
	repo := newTestRepository(t)
	if err := repo.DeleteUserFragments("nobody"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserTextsRemovesEntireVersionChain(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	original, err := repo.CreateText("Constitution", content, "alice")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	challenger, err := repo.ProposeEdit(original.ID, start, end, "français", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(challenger.ID, "carol"); err != nil {
		t.Fatal(err)
	}

	outcome, err := repo.CloseRound(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	forked := outcome.Text
	if forked.CreatedBy != "alice" {
		t.Fatalf("forked text CreatedBy = %q, want %q (should propagate through CloseRound)", forked.CreatedBy, "alice")
	}

	// Open a second round on the forked text, so its own version chain (and
	// bob's fragment/carol's vote on it) also has to be torn down.
	forkedStart, forkedEnd := runeRange(forked.Content, "français")
	secondChallenger, err := repo.ProposeEdit(forked.ID, forkedStart, forkedEnd, "FRANÇAIS", "dave")
	if err != nil {
		t.Fatal(err)
	}

	// A text alice did NOT create must survive untouched.
	unrelated, err := repo.CreateText("Autre texte", "Contenu neutre.", "eve")
	if err != nil {
		t.Fatal(err)
	}

	deletedIDs, err := repo.DeleteUserTexts("alice")
	if err != nil {
		t.Fatal(err)
	}
	wantDeleted := map[string]bool{original.ID: true, forked.ID: true}
	if len(deletedIDs) != len(wantDeleted) {
		t.Fatalf("DeleteUserTexts returned %v, want exactly %v", deletedIDs, wantDeleted)
	}
	for _, id := range deletedIDs {
		if !wantDeleted[id] {
			t.Fatalf("DeleteUserTexts returned unexpected id %q, want only %v", id, wantDeleted)
		}
	}

	if _, err := repo.Text(original.ID); err != ErrNotFound {
		t.Fatalf("expected original text to be gone, got err=%v", err)
	}
	if _, err := repo.Text(forked.ID); err != ErrNotFound {
		t.Fatalf("expected forked text to be gone, got err=%v", err)
	}
	if _, err := repo.Fragment(challenger.ID); err != ErrNotFound {
		t.Fatalf("expected bob's fragment on the original text to be gone, got err=%v", err)
	}
	if _, err := repo.Fragment(secondChallenger.ID); err != ErrNotFound {
		t.Fatalf("expected dave's fragment on the forked text to be gone, got err=%v", err)
	}
	votes, err := repo.VoteCount(challenger.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 0 {
		t.Fatalf("VoteCount for deleted fragment = %d, want 0", votes)
	}

	still, err := repo.Text(unrelated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.ID != unrelated.ID {
		t.Fatalf("unrelated text should be untouched, got %+v", still)
	}
}

func TestDeleteUserTextsNoTextsIsNoop(t *testing.T) {
	repo := newTestRepository(t)
	deletedIDs, err := repo.DeleteUserTexts("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedIDs) != 0 {
		t.Fatalf("expected no deleted ids, got %v", deletedIDs)
	}
}

func TestDeleteTextRemovesItsRoundsFragmentsAndVotesButNotForksOrOtherTexts(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	start, end := runeRange(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	unrelated, err := repo.CreateText("Autre texte", "Contenu neutre.", "bob")
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteText(text.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Text(text.ID); err != ErrNotFound {
		t.Fatalf("expected the deleted text to be gone, got err=%v", err)
	}
	if _, err := repo.Fragment(fragment.ID); err != ErrNotFound {
		t.Fatalf("expected its fragment to be gone, got err=%v", err)
	}
	votes, err := repo.VoteCount(fragment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 0 {
		t.Fatalf("VoteCount for deleted fragment = %d, want 0", votes)
	}

	// DeleteText only removes the exact ID given — the fork CloseRound
	// produced is its own independent text, untouched.
	if _, err := repo.Text(outcome.Text.ID); err != nil {
		t.Fatalf("expected the fork to survive deleting its predecessor, got err=%v", err)
	}

	// A completely unrelated text must never be touched.
	if _, err := repo.Text(unrelated.ID); err != nil {
		t.Fatalf("expected an unrelated text to be untouched, got err=%v", err)
	}
}

func TestDeleteTextUnknownTextIsNotFound(t *testing.T) {
	repo := newTestRepository(t)
	if err := repo.DeleteText("does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestDeleteTextRemovesOtherUsersSubscriptionsToIt proves DeleteText cleans
// up subscriptions made by users other than the text's own author — the
// case that matters when a user is deleted: DeleteUserTexts removes their
// texts via this same method, and anyone else who'd subscribed to one of
// those texts must not end up with a subscription pointing at nothing.
func TestDeleteTextRemovesOtherUsersSubscriptionsToIt(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateText("Autre texte", "Contenu neutre.", "bob")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Subscribe("alice", text.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("bob", text.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("alice", other.ID); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteText(text.ID); err != nil {
		t.Fatal(err)
	}

	for _, userID := range []string{"alice", "bob"} {
		subscribed, err := repo.IsSubscribed(userID, text.ID)
		if err != nil {
			t.Fatal(err)
		}
		if subscribed {
			t.Fatalf("%s's subscription to the deleted text should be gone", userID)
		}
	}

	// alice's unrelated subscription to a different text must survive.
	stillSubscribed, err := repo.IsSubscribed("alice", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stillSubscribed {
		t.Fatal("alice's subscription to an unrelated text must not be touched")
	}

	// And her per-user index must agree — only the unrelated text remains.
	aliceSubs, err := repo.SubscriptionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceSubs) != 1 || aliceSubs[0] != other.ID {
		t.Fatalf("SubscriptionsForUser(alice) = %v, want exactly [%q]", aliceSubs, other.ID)
	}
}

func TestDeleteUserSubscriptionsRemovesOnlyThatUsers(t *testing.T) {
	repo := newTestRepository(t)
	textA, err := repo.CreateText("Text A", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}
	textB, err := repo.CreateText("Text B", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Subscribe("alice", textA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("alice", textB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("bob", textA.ID); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteUserSubscriptions("alice"); err != nil {
		t.Fatal(err)
	}

	for _, textID := range []string{textA.ID, textB.ID} {
		subscribed, err := repo.IsSubscribed("alice", textID)
		if err != nil {
			t.Fatal(err)
		}
		if subscribed {
			t.Fatalf("alice's subscription to %s should be gone", textID)
		}
	}
	aliceSubs, err := repo.SubscriptionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceSubs) != 0 {
		t.Fatalf("SubscriptionsForUser(alice) = %v, want none left", aliceSubs)
	}

	// bob's subscription is untouched.
	bobSubscribed, err := repo.IsSubscribed("bob", textA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bobSubscribed {
		t.Fatal("bob's subscription must survive deleting alice's")
	}
}

func TestDeleteUserSubscriptionsNoSubscriptionsIsNoop(t *testing.T) {
	repo := newTestRepository(t)
	if err := repo.DeleteUserSubscriptions("nobody"); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribersForTextListsFollowers(t *testing.T) {
	repo := newTestRepository(t)

	watched, err := repo.CreateText("Suivi", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateText("Autre", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}

	for _, user := range []string{"alice", "bob"} {
		if _, err := repo.Subscribe(user, watched.ID); err != nil {
			t.Fatal(err)
		}
	}
	// carol follows a different text: her subscription must not leak into
	// the followers of the one being asked about.
	if _, err := repo.Subscribe("carol", other.ID); err != nil {
		t.Fatal(err)
	}

	subscribers, err := repo.SubscribersForText(watched.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range subscribers {
		got[id] = true
	}
	// creator included: CreateText subscribes the author to their own text.
	if len(got) != 3 || !got["alice"] || !got["bob"] || !got["creator"] {
		t.Fatalf("expected creator, alice and bob, got %v", subscribers)
	}
}

// A text nobody has explicitly followed still has exactly one subscriber:
// its author, subscribed by CreateText. Worth pinning down, because it is
// what makes notification fan-out need to exclude whoever caused the
// change — otherwise an author editing their own text notifies themselves.
func TestSubscribersForTextAlwaysIncludesItsCreator(t *testing.T) {
	repo := newTestRepository(t)

	text, err := repo.CreateText("Personne", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}

	subscribers, err := repo.SubscribersForText(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscribers) != 1 || subscribers[0] != "creator" {
		t.Fatalf("expected only the creator, got %v", subscribers)
	}
}
