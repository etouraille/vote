package main

import (
	"context"
	"log"
	"time"

	"github.com/etouraille/queel"
)

// defaultScheduledCloseInterval is how often runScheduledCloseWorker checks
// for rounds whose "close in N days" due date has arrived, when
// SCHEDULED_CLOSE_CHECK_INTERVAL isn't set. Scheduling only ever happens in
// whole-day increments, so checking every hour is far more than precise
// enough while staying cheap.
const defaultScheduledCloseInterval = time.Hour

// runScheduledCloseWorker is the other half of scheduleCloseHandler: it
// periodically asks queel for every round whose scheduled close date has
// passed (see queel.Repository.DueScheduledRounds) and closes each one the
// same way closeRoundHandler would — winning fragments spliced into a new
// Finalized text, then indexed for search. Runs for the life of the
// process; ctx is only there so a future caller could cancel it, nothing
// in main currently does.
func runScheduledCloseWorker(ctx context.Context, repo *queel.Repository, index *searchIndexer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due, err := repo.DueScheduledRounds(time.Now())
			if err != nil {
				log.Printf("scheduled close worker: listing due rounds: %v", err)
				continue
			}
			for _, round := range due {
				outcome, err := repo.CloseRound(round.TextID)
				if err != nil {
					log.Printf("scheduled close worker: closing round %d for text %s: %v", round.Number, round.TextID, err)
					continue
				}
				if err := index.IndexFinalizedText(ctx, outcome.Text); err != nil {
					log.Printf("scheduled close worker: indexing text %s (forked from %s): %v", outcome.Text.ID, round.TextID, err)
				}
				log.Printf("scheduled close worker: closed round %d for text %s (forked into %s)", round.Number, round.TextID, outcome.Text.ID)
			}
		}
	}
}
