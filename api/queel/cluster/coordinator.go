package cluster

import (
	"context"
	"fmt"
	"sync"

	"github.com/etouraille/queel"
)

// Coordinator fans reads and writes for a key out to its replicas (as
// decided by a Ring) and applies quorum consistency: a write only succeeds
// once enough replicas have durably stored it, and a read only succeeds
// once enough replicas have answered — picking the most recent (highest
// timestamp) value among them, so a replica that missed a write doesn't
// shadow a fresher one it holds elsewhere.
//
// ring and peers can be swapped out via SetMembers as cluster membership
// changes (e.g. driven by a Membership's gossip-derived view), so a
// Coordinator doesn't need a fixed, static node list for its whole lifetime.
type Coordinator struct {
	mu    sync.RWMutex
	ring  *Ring
	peers map[Node]*PeerClient
}

// NewCoordinator builds a coordinator over ring, using peers to reach each
// node's internal replication endpoint.
func NewCoordinator(ring *Ring, peers map[Node]*PeerClient) *Coordinator {
	return &Coordinator{ring: ring, peers: peers}
}

// SetMembers rebuilds the ring and peer set from the current list of alive
// nodes. Existing PeerClients are reused where possible; new ones are
// created for newly seen nodes. Call this whenever membership changes (e.g.
// after each Membership gossip round) so ongoing operations always route
// against up-to-date membership.
func (c *Coordinator) SetMembers(nodes []Node, replicationFactor int) {
	c.mu.RLock()
	previous := c.peers
	c.mu.RUnlock()

	peers := make(map[Node]*PeerClient, len(nodes))
	for _, node := range nodes {
		if existing, ok := previous[node]; ok {
			peers[node] = existing
		} else {
			peers[node] = NewPeerClient(string(node))
		}
	}
	ring := NewRing(nodes, replicationFactor)

	c.mu.Lock()
	c.ring = ring
	c.peers = peers
	c.mu.Unlock()
}

func (c *Coordinator) snapshot() (*Ring, map[Node]*PeerClient) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ring, c.peers
}

type writeResult struct {
	node Node
	err  error
}

// Put replicates value for key to all its replicas, succeeding once at
// least a quorum of them have acknowledged it.
func (c *Coordinator) Put(ctx context.Context, key string, value []byte) error {
	return c.write(ctx, key, NewEntry(value, false))
}

// Delete replicates a tombstone for key, succeeding once at least a quorum
// of replicas have acknowledged it.
func (c *Coordinator) Delete(ctx context.Context, key string) error {
	return c.write(ctx, key, NewEntry(nil, true))
}

func (c *Coordinator) write(ctx context.Context, key string, entry Entry) error {
	ring, peers := c.snapshot()
	replicas := ring.ReplicasFor(key)
	if len(replicas) == 0 {
		return fmt.Errorf("no replicas known for key %q", key)
	}
	quorum := Quorum(len(replicas))

	results := make(chan writeResult, len(replicas))
	for _, node := range replicas {
		node := node
		go func() {
			peer, ok := peers[node]
			if !ok {
				results <- writeResult{node: node, err: fmt.Errorf("no peer client for node %q", node)}
				return
			}
			results <- writeResult{node: node, err: peer.Put(ctx, key, entry)}
		}()
	}

	acked := 0
	var errs []error
	for i := 0; i < len(replicas); i++ {
		res := <-results
		if res.err == nil {
			acked++
		} else {
			errs = append(errs, fmt.Errorf("%s: %w", res.node, res.err))
		}
	}

	if acked < quorum {
		return fmt.Errorf("write quorum not reached for key %q: %d/%d acked (need %d): %v", key, acked, len(replicas), quorum, errs)
	}
	return nil
}

// WriteBatch applies several independent writes together. Different keys
// usually land on different replica sets, so this groups by node instead of
// by key: every node that holds a replica for at least one of the keys gets
// exactly one batched request, however many of the ops it's responsible
// for — one round trip per involved node instead of one per key. Quorum is
// still verified per key afterwards, exactly as Put/Delete would.
func (c *Coordinator) WriteBatch(ctx context.Context, ops []queel.WriteOp) error {
	if len(ops) == 0 {
		return nil
	}
	ring, peers := c.snapshot()

	type plan struct {
		key      string
		replicas []Node
	}
	plans := make([]plan, 0, len(ops))
	itemsByNode := make(map[Node][]KeyEntry)

	for _, op := range ops {
		key := string(op.Key)
		entry := NewEntry(op.Value, op.Tombstone)
		replicas := ring.ReplicasFor(key)
		if len(replicas) == 0 {
			return fmt.Errorf("no replicas known for key %q", key)
		}
		plans = append(plans, plan{key: key, replicas: replicas})
		for _, node := range replicas {
			itemsByNode[node] = append(itemsByNode[node], KeyEntry{Key: key, Entry: entry})
		}
	}

	results := make(chan writeResult, len(itemsByNode))
	for node, items := range itemsByNode {
		node, items := node, items
		go func() {
			peer, ok := peers[node]
			if !ok {
				results <- writeResult{node: node, err: fmt.Errorf("no peer client for node %q", node)}
				return
			}
			results <- writeResult{node: node, err: peer.PutBatch(ctx, items)}
		}()
	}

	acked := make(map[Node]bool, len(itemsByNode))
	var errs []error
	for i := 0; i < len(itemsByNode); i++ {
		res := <-results
		if res.err == nil {
			acked[res.node] = true
		} else {
			errs = append(errs, fmt.Errorf("%s: %w", res.node, res.err))
		}
	}

	var failedKeys []string
	for _, p := range plans {
		count := 0
		for _, node := range p.replicas {
			if acked[node] {
				count++
			}
		}
		if count < Quorum(len(p.replicas)) {
			failedKeys = append(failedKeys, p.key)
		}
	}
	if len(failedKeys) > 0 {
		return fmt.Errorf("write quorum not reached for keys %v: %v", failedKeys, errs)
	}
	return nil
}

type readResult struct {
	node  Node
	entry Entry
	found bool
	err   error
}

// Get reads key from all its replicas and returns the most recent value
// among those that responded (last-write-wins), succeeding once at least a
// quorum of replicas answered. found is false if the winning entry is
// absent everywhere reachable, or tombstoned.
//
// Before returning, it read-repairs: any replica that answered with an older
// entry than the winner — or never saw the key at all — gets the winning
// entry pushed to it synchronously, so a node that missed a write (or is
// catching up after downtime) converges right away instead of staying stale
// until another quorum read happens to notice the gap again.
//
// This waits for every replica to answer or fail rather than returning as
// soon as a quorum responds, so it always picks up the freshest value any
// reachable replica holds, and so read-repair has a complete picture of who
// needs fixing — simpler and more accurate at the cost of sometimes waiting
// on a slow node instead of racing ahead.
func (c *Coordinator) Get(ctx context.Context, key string) (value []byte, found bool, err error) {
	ring, peers := c.snapshot()
	replicas := ring.ReplicasFor(key)
	if len(replicas) == 0 {
		return nil, false, fmt.Errorf("no replicas known for key %q", key)
	}
	quorum := Quorum(len(replicas))

	results := make(chan readResult, len(replicas))
	for _, node := range replicas {
		node := node
		go func() {
			peer, ok := peers[node]
			if !ok {
				results <- readResult{node: node, err: fmt.Errorf("no peer client for node %q", node)}
				return
			}
			entry, found, err := peer.Get(ctx, key)
			results <- readResult{node: node, entry: entry, found: found, err: err}
		}()
	}

	responded := 0
	var best Entry
	haveBest := false
	responses := make([]readResult, 0, len(replicas))
	for i := 0; i < len(replicas); i++ {
		res := <-results
		responses = append(responses, res)
		if res.err != nil {
			continue
		}
		responded++
		if !res.found {
			continue
		}
		if !haveBest || res.entry.Timestamp > best.Timestamp {
			best = res.entry
			haveBest = true
		}
	}

	if responded < quorum {
		return nil, false, fmt.Errorf("read quorum not reached for key %q: %d/%d responded (need %d)", key, responded, len(replicas), quorum)
	}

	if haveBest {
		c.readRepair(ctx, peers, key, best, responses)
	}

	if !haveBest || best.Tombstone {
		return nil, false, nil
	}
	return best.Value, true, nil
}

// readRepair pushes the winning entry to every replica that responded with
// something older, or with nothing at all, so it converges without waiting
// on another read to notice. Unreachable replicas are simply skipped —
// there's nothing to repair right now if a node can't be reached at all.
func (c *Coordinator) readRepair(ctx context.Context, peers map[Node]*PeerClient, key string, best Entry, responses []readResult) {
	for _, res := range responses {
		if res.err != nil {
			continue
		}
		if res.found && res.entry.Timestamp >= best.Timestamp {
			continue
		}
		if peer, ok := peers[res.node]; ok {
			_ = peer.Put(ctx, key, best) // best-effort: a failed repair just leaves the node stale until next time
		}
	}
}

type scanResult struct {
	entries []KeyEntry
	err     error
}

// Scan broadcasts prefix to every known peer and merges their results,
// picking the most recent entry per key across whichever peers respond
// (same last-write-wins rule as Get).
//
// Unlike Put/Get/Delete, this does not go through the Ring: keys sharing a
// prefix (e.g. every fragment proposed for one slot) are not guaranteed to
// share a replica set, since Put/Get/Delete shard each individual key on its
// own. Broadcasting to every peer sidesteps that entirely, at the cost of
// O(cluster size) fan-out instead of O(replication factor) — acceptable
// since queel's scans are always small, bounded lookups within one text, not
// full-dataset scans. There is no strict quorum here either: it returns
// whatever the reachable peers have, which only gets more complete as more
// of them respond; it fails only if none respond at all.
func (c *Coordinator) Scan(ctx context.Context, prefix string) ([]queel.KV, error) {
	_, peers := c.snapshot()

	results := make(chan scanResult, len(peers))
	for _, peer := range peers {
		peer := peer
		go func() {
			entries, err := peer.Scan(ctx, prefix)
			results <- scanResult{entries: entries, err: err}
		}()
	}

	best := make(map[string]Entry)
	responded := 0
	for i := 0; i < len(peers); i++ {
		res := <-results
		if res.err != nil {
			continue
		}
		responded++
		for _, ke := range res.entries {
			if existing, ok := best[ke.Key]; !ok || ke.Entry.Timestamp > existing.Timestamp {
				best[ke.Key] = ke.Entry
			}
		}
	}
	if responded == 0 {
		return nil, fmt.Errorf("scan failed for prefix %q: no peers reachable", prefix)
	}

	kvs := make([]queel.KV, 0, len(best))
	for key, entry := range best {
		if entry.Tombstone {
			continue
		}
		kvs = append(kvs, queel.KV{Key: []byte(key), Value: entry.Value})
	}
	return kvs, nil
}
