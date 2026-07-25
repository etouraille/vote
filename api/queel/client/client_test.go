package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/client"
	"github.com/etouraille/queel/server"
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

func newTestClient(t *testing.T) *client.Client {
	t.Helper()
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	repo := queel.NewRepository(engine)
	ts := httptest.NewServer(server.NewHandler(repo, nil))
	t.Cleanup(ts.Close)

	return client.New(ts.URL)
}

func TestClientFullRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	content := "Nous le peuple francais declare."
	text, err := c.CreateText(ctx, "Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := c.Text(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Content != content {
		t.Fatalf("Content = %q, want %q", fetched.Content, content)
	}

	start, end := runeRange(content, "francais")
	challenger, err := c.ProposeEdit(ctx, text.ID, start, end, "français", "alice")
	if err != nil {
		t.Fatal(err)
	}

	round, err := c.CurrentRound(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(round.Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(round.Slots))
	}

	fragments, err := c.Fragments(ctx, text.ID, round.Slots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 2 { // seed + alice's proposal
		t.Fatalf("expected 2 fragments, got %d", len(fragments))
	}

	if err := c.CastVote(ctx, challenger.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.CastVote(ctx, challenger.ID, "user-2"); err != nil {
		t.Fatal(err)
	}

	votes, err := c.VoteCount(ctx, challenger.ID)
	if err != nil {
		t.Fatal(err)
	}
	if votes != 2 {
		t.Fatalf("expected 2 votes, got %d", votes)
	}

	outcome, err := c.CloseRound(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Nous le peuple français declare."
	if outcome.Text.Content != want {
		t.Fatalf("Content = %q, want %q", outcome.Text.Content, want)
	}

	if _, err := c.CurrentRound(ctx, text.ID); err == nil {
		t.Fatal("expected an error (no open round) after closing")
	}
}

func TestClientUpdateText(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	text, err := c.CreateText(ctx, "Draft", "v1", "creator")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := c.UpdateText(ctx, text.ID, "Final", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Final" || updated.Content != "v2" {
		t.Fatalf("UpdateText = %+v, want title %q content %q", updated, "Final", "v2")
	}

	fetched, err := c.Text(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Title != "Final" || fetched.Content != "v2" {
		t.Fatalf("Text after update = %+v, want title %q content %q", fetched, "Final", "v2")
	}
}

func TestClientTextWithSlots(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	content := "Nous le peuple francais declare."
	text, err := c.CreateText(ctx, "Constitution", content, "creator")
	if err != nil {
		t.Fatal(err)
	}

	empty, err := c.TextWithSlots(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.RoundNumber != 0 || len(empty.Slots) != 0 {
		t.Fatalf("TextWithSlots with no round = %+v, want RoundNumber 0 and no slots", empty)
	}

	start, end := runeRange(content, "francais")
	if _, err := c.ProposeEdit(ctx, text.ID, start, end, "français", "alice"); err != nil {
		t.Fatal(err)
	}

	withSlot, err := c.TextWithSlots(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withSlot.RoundNumber != 1 || len(withSlot.Slots) != 1 {
		t.Fatalf("TextWithSlots after ProposeEdit = %+v, want RoundNumber 1 and 1 slot", withSlot)
	}
}

func TestClientErrorsSurfaceAsAPIError(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	_, err := c.Text(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestClientCloseRoundWithNoOpenRoundIsAConflict(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	text, err := c.CreateText(ctx, "Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.CloseRound(ctx, text.ID)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, want 409", apiErr.StatusCode)
	}
}
