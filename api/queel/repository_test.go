package queel

import (
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
		text, err := repo.CreateText(fmt.Sprintf("Text %d", i), "content")
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

	text, err := repo.CreateText("Constitution", "Nous, le peuple.")
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
	text, err := repo.CreateText("Brouillon", "Nous, le peuple.")
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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

func TestProposeEditRespectsRuneBoundaries(t *testing.T) {
	repo := newTestRepository(t)
	content := "Liberté, égalité, fraternité."
	text, err := repo.CreateText("Devise", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", "Nous le peuple.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseRound(text.ID); err != ErrNoOpenRound {
		t.Fatalf("expected ErrNoOpenRound, got %v", err)
	}
}

func TestTextWithSlotsNoOpenRound(t *testing.T) {
	repo := newTestRepository(t)
	text, err := repo.CreateText("Constitution", "Nous le peuple.")
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
	text, err := repo.CreateText("Constitution", content)
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
	text, err := repo.CreateText("Constitution", content)
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

func TestWinningFragmentTieBreaksOnEarliestProposal(t *testing.T) {
	repo := newTestRepository(t)
	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content)
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
