package cluster_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

// testNode is one node of a test cluster: a real Engine on disk, served over
// a real HTTP test server via server.NewInternalHandler.
type testNode struct {
	name cluster.Node
	ts   *httptest.Server
}

func newTestCluster(t *testing.T, n int) (nodes []testNode, ring *cluster.Ring, peers map[cluster.Node]*cluster.PeerClient) {
	t.Helper()

	var names []cluster.Node
	peers = make(map[cluster.Node]*cluster.PeerClient)

	for i := 0; i < n; i++ {
		engine, err := queel.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = engine.Close() })

		ts := httptest.NewServer(server.NewInternalHandler(engine))
		t.Cleanup(ts.Close)

		name := cluster.Node(ts.URL)
		nodes = append(nodes, testNode{name: name, ts: ts})
		names = append(names, name)
		peers[name] = cluster.NewPeerClient(ts.URL)
	}

	ring = cluster.NewRing(names, n)
	return nodes, ring, peers
}

func TestCoordinatorReplicatesPutToAllNodesAndReadsItBack(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	value, found, err := coord.Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(value) != "v1" {
		t.Fatalf("Get = %q, %v, want %q, true", value, found, "v1")
	}
}

func TestCoordinatorToleratesOneNodeBeingDown(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	// Write while all 3 are healthy.
	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	// Take one replica offline by pointing its peer client at a dead address —
	// with RF=3, quorum is 2, so writes and reads should still succeed.
	replicas := ring.ReplicasFor("text/1")
	downNode := replicas[0]
	original := peers[downNode]
	peers[downNode] = cluster.NewPeerClient("http://127.0.0.1:1") // nothing listens here

	if err := coord.Put(ctx, "text/1", []byte("v2")); err != nil {
		t.Fatalf("expected quorum write to survive one dead node, got: %v", err)
	}
	value, found, err := coord.Get(ctx, "text/1")
	if err != nil {
		t.Fatalf("expected quorum read to survive one dead node, got: %v", err)
	}
	if !found || string(value) != "v2" {
		t.Fatalf("Get = %q, %v, want %q, true", value, found, "v2")
	}

	// Bring it back: it must have missed v2 entirely.
	peers[downNode] = original
	entry, foundOnDownNode, err := peers[downNode].Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if !foundOnDownNode || string(entry.Value) != "v1" {
		t.Fatalf("expected the previously-down node to still have the stale value %q, got %q", "v1", entry.Value)
	}
}

func TestCoordinatorGetPicksMostRecentValueAcrossStaleReplicas(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	replicas := ring.ReplicasFor("text/1")
	staleNode := replicas[0]
	original := peers[staleNode]
	peers[staleNode] = cluster.NewPeerClient("http://127.0.0.1:1")

	if err := coord.Put(ctx, "text/1", []byte("v2")); err != nil {
		t.Fatal(err)
	}

	// staleNode never received v2; once it's reachable again, a Get across
	// all three replicas must still return v2, not fall back to the stale v1.
	peers[staleNode] = original

	value, found, err := coord.Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(value) != "v2" {
		t.Fatalf("Get = %q, %v, want the freshest value %q", value, found, "v2")
	}
}

func TestCoordinatorGetRepairsStaleReplica(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	replicas := ring.ReplicasFor("text/1")
	staleNode := replicas[0]
	original := peers[staleNode]
	peers[staleNode] = cluster.NewPeerClient("http://127.0.0.1:1")

	if err := coord.Put(ctx, "text/1", []byte("v2")); err != nil {
		t.Fatal(err)
	}

	// staleNode missed v2. Bring it back, then read once through the
	// coordinator: that single Get must repair it.
	peers[staleNode] = original

	if _, _, err := coord.Get(ctx, "text/1"); err != nil {
		t.Fatal(err)
	}

	entry, found, err := original.Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(entry.Value) != "v2" {
		t.Fatalf("expected read-repair to have fixed the stale replica to %q, got found=%v value=%q", "v2", found, entry.Value)
	}
}

func TestCoordinatorGetRepairsReplicaThatNeverSawTheKey(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	replicas := ring.ReplicasFor("text/1")
	missingNode := replicas[0]
	original := peers[missingNode]
	peers[missingNode] = cluster.NewPeerClient("http://127.0.0.1:1")

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	// missingNode was down for the very first write: it never saw this key.

	peers[missingNode] = original
	if _, _, err := coord.Get(ctx, "text/1"); err != nil {
		t.Fatal(err)
	}

	entry, found, err := original.Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(entry.Value) != "v1" {
		t.Fatalf("expected read-repair to have populated the previously-missing replica with %q, got found=%v value=%q", "v1", found, entry.Value)
	}
}

func TestCoordinatorFailsWhenQuorumUnreachable(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	// Take two of the three replicas down: only 1/3 remain, below quorum (2).
	replicas := ring.ReplicasFor("text/1")
	for _, node := range replicas[:2] {
		peers[node] = cluster.NewPeerClient("http://127.0.0.1:1")
	}

	if _, _, err := coord.Get(ctx, "text/1"); err == nil {
		t.Fatal("expected read quorum failure with only 1/3 replicas reachable, got none")
	}
	if err := coord.Put(ctx, "text/1", []byte("v2")); err == nil {
		t.Fatal("expected write quorum failure with only 1/3 replicas reachable, got none")
	}
}

func TestCoordinatorDelete(t *testing.T) {
	ctx := context.Background()
	_, ring, peers := newTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	if err := coord.Put(ctx, "text/1", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := coord.Delete(ctx, "text/1"); err != nil {
		t.Fatal(err)
	}

	_, found, err := coord.Get(ctx, "text/1")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected key to be gone after Delete")
	}
}
