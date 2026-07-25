package cluster

import (
	"fmt"
	"testing"
)

func fiveNodes() []Node {
	return []Node{"node-a", "node-b", "node-c", "node-d", "node-e"}
}

func TestReplicasForReturnsDistinctNodesAtReplicationFactor(t *testing.T) {
	ring := NewRing(fiveNodes(), 3)

	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("text-%d", i)
		replicas := ring.ReplicasFor(key)
		if len(replicas) != 3 {
			t.Fatalf("key %q: got %d replicas, want 3: %v", key, len(replicas), replicas)
		}
		seen := map[Node]bool{}
		for _, n := range replicas {
			if seen[n] {
				t.Fatalf("key %q: duplicate node %q in %v", key, n, replicas)
			}
			seen[n] = true
		}
	}
}

func TestReplicasForIsDeterministic(t *testing.T) {
	ring := NewRing(fiveNodes(), 3)

	first := ring.ReplicasFor("text-42")
	for i := 0; i < 10; i++ {
		again := ring.ReplicasFor("text-42")
		if fmt.Sprint(again) != fmt.Sprint(first) {
			t.Fatalf("ReplicasFor is not stable: got %v, then %v", first, again)
		}
	}
}

func TestReplicasForDistributesPrimaryOwnershipReasonablyEvenly(t *testing.T) {
	ring := NewRing(fiveNodes(), 3)

	counts := map[Node]int{}
	const sampleSize = 5000
	for i := 0; i < sampleSize; i++ {
		primary := ring.ReplicasFor(fmt.Sprintf("text-%d", i))[0]
		counts[primary]++
	}

	// With 5 nodes, a perfectly even split would be 1000 each; allow a
	// generous margin since this is a hash-based approximation, not exact.
	want := sampleSize / len(fiveNodes())
	for node, count := range counts {
		if count < want/2 || count > want*2 {
			t.Errorf("node %q got %d/%d keys as primary, want roughly %d", node, count, sampleSize, want)
		}
	}
	if len(counts) != len(fiveNodes()) {
		t.Errorf("expected all %d nodes to own at least one key as primary, got %d distinct owners", len(fiveNodes()), len(counts))
	}
}

func TestReplicationFactorClampedToNodeCount(t *testing.T) {
	ring := NewRing([]Node{"only-node"}, 3)

	replicas := ring.ReplicasFor("text-1")
	if len(replicas) != 1 {
		t.Fatalf("expected replication factor clamped to 1 node, got %v", replicas)
	}
}

func TestAddingANodeOnlyReshufflesAMinorityOfKeys(t *testing.T) {
	before := NewRing(fiveNodes(), 3)
	after := NewRing(append(fiveNodes(), "node-f"), 3)

	const sampleSize = 2000
	moved := 0
	for i := 0; i < sampleSize; i++ {
		key := fmt.Sprintf("text-%d", i)
		if before.ReplicasFor(key)[0] != after.ReplicasFor(key)[0] {
			moved++
		}
	}

	// Consistent hashing's whole point: adding one node out of six should
	// only reassign roughly 1/6th of keys, not (as naive hash % N sharding
	// would) nearly all of them.
	if moved > sampleSize/2 {
		t.Fatalf("expected adding a node to reshuffle a minority of keys, but %d/%d changed primary", moved, sampleSize)
	}
}

func TestQuorum(t *testing.T) {
	cases := map[int]int{1: 1, 2: 2, 3: 2, 4: 3, 5: 3, 6: 4, 7: 4}
	for rf, want := range cases {
		if got := Quorum(rf); got != want {
			t.Errorf("Quorum(%d) = %d, want %d", rf, got, want)
		}
	}
}
