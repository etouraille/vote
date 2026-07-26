package main

import (
	"context"
	"log"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

// defaultScheduledCloseInterval is how often runScheduledCloseWorker checks
// for rounds whose "close in N days" due date has arrived, when
// SCHEDULED_CLOSE_CHECK_INTERVAL isn't set. Scheduling only ever happens in
// whole-day increments, so checking every hour is far more than precise
// enough while staying cheap.
const defaultScheduledCloseInterval = time.Hour

// isScheduledCloseLeader reports whether self should run the scheduled
// close worker right now: the node that sorts first among those membership
// currently believes are alive (AliveNodes() is already stably sorted).
// Nodes are otherwise perfectly symmetric — no primary, no leader election
// — so this simple deterministic rule is enough to ensure exactly one node
// runs the worker at a time without any coordination between them.
// Re-evaluating this on every call (rather than deciding once at startup)
// is what makes it self-healing: the moment the current leader stops being
// reported alive, the next call promotes whichever node is now first.
func isScheduledCloseLeader(membership *cluster.Membership, self cluster.Node) bool {
	alive := membership.AliveNodes()
	return len(alive) > 0 && alive[0] == self
}

// runScheduledCloseWorker is the other half of scheduleCloseHandler: it
// periodically asks queel for every round whose scheduled close date has
// passed (see queel.Repository.DueScheduledRounds) and closes each one the
// same way closeRoundHandler would — winning fragments spliced into a new
// Finalized text, then indexed for search. Runs for the life of the
// process; ctx is only there so a future caller could cancel it, nothing
// in main currently does.
//
// isLeader is nil in single-node mode, where this is the only process that
// could possibly run it. In cluster mode every node runs this same loop —
// there's no separate scheduler process — so main passes a closure here
// that's true only on one deterministically-chosen node (see main.go's
// wiring), otherwise every node would redundantly re-scan the same
// replicated data and could race each other to close the same round.
// Re-checked every tick rather than once at startup, so leadership follows
// the node set as it changes instead of freezing whoever happened to be
// first when this process started.
func runScheduledCloseWorker(ctx context.Context, repo *queel.Repository, index *searchIndexer, interval time.Duration, isLeader func() bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if isLeader != nil && !isLeader() {
				continue
			}
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
