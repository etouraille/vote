package cluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/etouraille/queel"
)

// Decommission is a proactive, operator-triggered handoff run on self
// before it actually leaves the cluster: for every key self currently
// holds locally, it computes who will be responsible for that key once
// self is no longer part of the ring (aliveNodes minus self), and pushes
// self's copy to whichever of those future replicas doesn't already have
// an equally-or-more-recent version — the same last-write-wins comparison
// read-repair and Reconcile already use, so a target with a newer value
// from elsewhere is never regressed.
//
// This is what turns a node's departure from "wait up to
// QUEEL_ANTI_ENTROPY_INTERVAL for the cluster to notice and repair
// itself" into "the cluster never dips below its target replication
// factor in the first place" — anti-entropy is a safety net for the
// unplanned case (a crash), this is the deliberate one.
//
// It does not touch cluster membership or stop anything: self is still
// considered alive until whatever calls this also stops the process
// afterward (see server.DecommissionHandler). It returns how many
// key/target pairs were actually written.
func Decommission(ctx context.Context, engine *queel.Engine, self Node, aliveNodes []Node, replicationFactor int) (int, error) {
	remaining := make([]Node, 0, len(aliveNodes))
	for _, n := range aliveNodes {
		if n != self {
			remaining = append(remaining, n)
		}
	}
	if len(remaining) == 0 {
		return 0, fmt.Errorf("cluster: cannot decommission %s, no other node would remain", self)
	}
	futureRing := NewRing(remaining, replicationFactor)

	kvs, err := engine.Scan([]byte(""))
	if err != nil {
		return 0, err
	}

	peers := make(map[Node]*PeerClient, len(remaining))
	handedOff := 0
	for _, kv := range kvs {
		key := string(kv.Key)
		var entry Entry
		if err := json.Unmarshal(kv.Value, &entry); err != nil {
			return handedOff, err
		}

		for _, target := range futureRing.ReplicasFor(key) {
			peer, ok := peers[target]
			if !ok {
				peer = NewPeerClient(string(target))
				peers[target] = peer
			}

			existing, found, err := peer.Get(ctx, key)
			if err != nil {
				return handedOff, fmt.Errorf("checking %s on %s before handoff: %w", key, target, err)
			}
			if found && existing.Timestamp >= entry.Timestamp {
				continue // target already has this key's current-or-newer value
			}
			if err := peer.Put(ctx, key, entry); err != nil {
				return handedOff, fmt.Errorf("handing off %s to %s: %w", key, target, err)
			}
			handedOff++
		}
	}
	return handedOff, nil
}
