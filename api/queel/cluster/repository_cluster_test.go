package cluster_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

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

// TestRepositoryOverDistributedStore proves the exact same domain logic
// (Repository) that runs over a single local Engine also runs, completely
// unchanged, over a 3-node replicated cluster reached through
// cluster.DistributedStore — the whole point of Repository depending on the
// queel.Store interface instead of a concrete *Engine.
func TestRepositoryOverDistributedStore(t *testing.T) {
	_, ring, peers := newTestCluster(t, 3)
	coordinator := cluster.NewCoordinator(ring, peers)
	store := cluster.NewDistributedStore(coordinator)
	repo := queel.NewRepository(store)

	content := "Nous le peuple francais declare la republique."
	text, err := repo.CreateText("Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := repo.Text(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Content != content {
		t.Fatalf("Content = %q, want %q", fetched.Content, content)
	}

	s1, e1 := runeRange(content, "francais")
	f1, err := repo.ProposeEdit(text.ID, s1, e1, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	s2, e2 := runeRange(content, "republique")
	f2, err := repo.ProposeEdit(text.ID, s2, e2, "République", "bob")
	if err != nil {
		t.Fatal(err)
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(round.Slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(round.Slots))
	}

	fragments, err := repo.Fragments(text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 2 { // seed + one proposal
		t.Fatalf("expected 2 fragments for the first slot, got %d", len(fragments))
	}

	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f1.ID, "user-2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f2.ID, "user-3"); err != nil {
		t.Fatal(err)
	}

	votes, err := repo.VoteCount(f1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 2 {
		t.Fatalf("VoteCount(f1) = %d, want 2", votes)
	}

	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Nous le peuple français declare la République."
	if outcome.Text.Content != want {
		t.Fatalf("Content = %q, want %q", outcome.Text.Content, want)
	}
	if outcome.Round.Status != queel.RoundStatusClosed {
		t.Fatalf("expected round to be closed, got %+v", outcome.Round)
	}

	if _, err := repo.CurrentRound(text.ID); err != queel.ErrNotFound {
		t.Fatalf("expected no open round after closing, got %v", err)
	}
}
