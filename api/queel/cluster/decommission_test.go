package cluster_test

import (
	"context"
	"testing"

	"github.com/etouraille/queel/cluster"
)

// These reuse newAntiEntropyNode/putRaw/getRaw from antientropy_test.go —
// same package (cluster_test), same real-engine-behind-real-HTTP approach.

func TestDecommissionHandsOffLocalKeysToFutureReplicas(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)
	c := newAntiEntropyNode(t)

	selfC := cluster.Node(c.ts.URL)
	all := []cluster.Node{cluster.Node(a.ts.URL), cluster.Node(b.ts.URL), selfC}

	putRaw(t, c, "only-on-c", cluster.NewEntry([]byte("from c"), false))

	// With only a and b remaining and replicationFactor 2, every key's
	// future replica set is exactly {a, b} regardless of its hash — so
	// decommissioning c must hand this one key off to both.
	handedOff, err := cluster.Decommission(context.Background(), c.engine, selfC, all, 2)
	if err != nil {
		t.Fatal(err)
	}
	if handedOff != 2 {
		t.Fatalf("expected 2 handoffs, got %d", handedOff)
	}

	for _, node := range []antiEntropyNode{a, b} {
		entry, found := getRaw(t, node, "only-on-c")
		if !found || string(entry.Value) != "from c" {
			t.Fatalf("expected only-on-c handed off, got found=%v entry=%+v", found, entry)
		}
	}
}

func TestDecommissionDoesNotRegressANewerValueAlreadyOnTheTarget(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)
	c := newAntiEntropyNode(t)

	selfC := cluster.Node(c.ts.URL)
	all := []cluster.Node{cluster.Node(a.ts.URL), cluster.Node(b.ts.URL), selfC}

	putRaw(t, c, "contested-key", cluster.Entry{Value: []byte("stale from c"), Timestamp: 100})
	putRaw(t, a, "contested-key", cluster.Entry{Value: []byte("newer on a"), Timestamp: 500})

	if _, err := cluster.Decommission(context.Background(), c.engine, selfC, all, 2); err != nil {
		t.Fatal(err)
	}

	entry, _ := getRaw(t, a, "contested-key")
	if string(entry.Value) != "newer on a" {
		t.Fatalf("decommission must not regress a's newer value, got %q", entry.Value)
	}
}

func TestDecommissionSkipsATargetThatAlreadyHasTheSameEntry(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)
	c := newAntiEntropyNode(t)

	selfC := cluster.Node(c.ts.URL)
	all := []cluster.Node{cluster.Node(a.ts.URL), cluster.Node(b.ts.URL), selfC}

	entry := cluster.Entry{Value: []byte("already everywhere"), Timestamp: 42}
	putRaw(t, a, "shared-key", entry)
	putRaw(t, b, "shared-key", entry)
	putRaw(t, c, "shared-key", entry)

	handedOff, err := cluster.Decommission(context.Background(), c.engine, selfC, all, 2)
	if err != nil {
		t.Fatal(err)
	}
	if handedOff != 0 {
		t.Fatalf("expected no handoffs when targets already match, got %d", handedOff)
	}
}

func TestDecommissionRejectsLeavingNoOneBehind(t *testing.T) {
	a := newAntiEntropyNode(t)
	selfA := cluster.Node(a.ts.URL)

	if _, err := cluster.Decommission(context.Background(), a.engine, selfA, []cluster.Node{selfA}, 1); err == nil {
		t.Fatal("expected an error decommissioning the only node in the cluster")
	}
}
