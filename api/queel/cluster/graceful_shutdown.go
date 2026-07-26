package cluster

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/etouraille/queel"
)

// DefaultDecommissionTimeout bounds how long DecommissionOnShutdown waits
// for the handoff before giving up and exiting anyway — comfortably under
// Kubernetes' default terminationGracePeriodSeconds (30s), so a slow
// handoff loses at worst the keys it hadn't reached yet (left to
// anti-entropy, same as an unplanned crash) rather than getting SIGKILLed
// mid-write.
const DefaultDecommissionTimeout = 25 * time.Second

// DecommissionOnShutdown makes self decommission itself automatically on a
// graceful stop, instead of depending on an operator to POST
// /internal/decommission by hand first. It registers a background signal
// handler for SIGTERM (docker stop, kubectl delete pod, systemctl stop —
// what an orchestrator sends before escalating to SIGKILL) and SIGINT
// (Ctrl-C, for local use), and the moment either arrives, runs the same
// handoff Decommission always does — self's local keys pushed to whichever
// nodes will be responsible for them once self is gone — before exiting
// the process via os.Exit.
//
// SIGKILL and crashes can't be caught this way; anti-entropy remains the
// only safety net for those, exactly as it was before this existed. The
// manual /internal/decommission endpoint is unaffected — this only adds an
// automatic trigger for the graceful case, it doesn't replace the other.
//
// Call this once, after a node has joined the cluster. It returns
// immediately; the signal handling runs in the background for the life of
// the process.
func DecommissionOnShutdown(engine *queel.Engine, membership *Membership, self Node, replicationFactor int, timeout time.Duration) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.Printf("received %s: decommissioning %s before exit (up to %s)...", sig, self, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		handedOff, err := Decommission(ctx, engine, self, membership.AliveNodes(), replicationFactor)
		if err != nil {
			log.Printf("decommission on shutdown failed, exiting anyway (anti-entropy will repair what wasn't handed off): %v", err)
		} else {
			log.Printf("decommissioned %s: handed off %d key/target pairs, exiting", self, handedOff)
		}
		os.Exit(0)
	}()
}
