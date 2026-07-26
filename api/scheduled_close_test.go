package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/etouraille/queel"
)

// noopEmbedder always fails — runScheduledCloseWorker must still close a
// due round even though indexing the fork it produces can't succeed
// against it, exactly as IndexFinalizedText's own doc comment promises.
type noopEmbedder struct{}

func (noopEmbedder) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedding unavailable in test")
}

func TestRunScheduledCloseWorkerClosesDueRounds(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := queel.NewRepository(engine)

	dueText, err := repo.CreateText("Due", "Contenu a modifier.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(dueText.ID, 0, len("Contenu"), "Contenu modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(dueText.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	// A second text scheduled well into the future must be left alone —
	// the worker shouldn't close everything it sees, only what's due.
	futureText, err := repo.CreateText("Future", "Autre contenu ici.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(futureText.ID, 0, len("Autre"), "Un autre", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(futureText.ID, time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Unreachable on purpose: proves a worker that can't reach
	// Qdrant/Ollama still closes the round instead of getting stuck on the
	// best-effort indexing step.
	index := newSearchIndexer(noopEmbedder{}, newQdrantClient("http://127.0.0.1:1", "test"), false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go runScheduledCloseWorker(ctx, repo, index, 20*time.Millisecond)

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := repo.CurrentRound(dueText.ID); errors.Is(err, queel.ErrNotFound) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := repo.CurrentRound(dueText.ID); !errors.Is(err, queel.ErrNotFound) {
		t.Fatalf("expected the due round to be closed (no current round left), got err=%v", err)
	}

	if _, err := repo.CurrentRound(futureText.ID); err != nil {
		t.Fatalf("expected the future-scheduled round to still be open, got err=%v", err)
	}
}
