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

	recent, err := repo.RecentTexts(4)
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

func TestCreateTextHasNoSlotsOrOpenRound(t *testing.T) {
	repo := newTestRepository(t)

	text, err := repo.CreateText("Constitution", "Nous, le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if text.Content != "Nous, le peuple." {
		t.Fatalf("Content = %q", text.Content)
	}
	if _, err := repo.CurrentRound(text.ID); err != ErrNotFound {
		t.Fatalf("expected no open round on a fresh text, got %v", err)
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

func TestCloseRoundNoOpenRound(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseRound(text.ID); err != ErrNoOpenRound {
		t.Fatalf("expected ErrNoOpenRound, got %v", err)
	}
}

func TestTextWithSlotsNoOpenRound(t *testing.T) {
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
	if got.RoundNumber != 0 {
		t.Fatalf("RoundNumber = %d, want 0 with no round ever opened", got.RoundNumber)
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

func TestRoundCountNeverOpenedIsZero(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple francais declare.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	count, err := repo.RoundCount(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("RoundCount for a text with no round ever opened = %d, want 0", count)
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

	// The original text's own round is gone (it forked), but RoundCount on
	// the *forked* text must still say 1 — that's the whole point: a closed
	// round shouldn't look indistinguishable from "never had a round".
	if _, err := repo.CurrentRound(outcome.Text.ID); err != ErrNotFound {
		t.Fatalf("expected no open round on the freshly forked text, got err=%v", err)
	}
	count, err = repo.RoundCount(outcome.Text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("RoundCount on the forked text after closing = %d, want 1 (the round that produced it)", count)
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

	if err := repo.DeleteUserTexts("alice"); err != nil {
		t.Fatal(err)
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
	if err := repo.DeleteUserTexts("nobody"); err != nil {
		t.Fatal(err)
	}
}
