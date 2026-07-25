package cluster_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

// newCountingTestCluster is like newTestCluster but counts every HTTP
// request each node receives, so tests can verify batching actually reduces
// round trips instead of just checking the end result is correct.
func newCountingTestCluster(t *testing.T, n int) (ring *cluster.Ring, peers map[cluster.Node]*cluster.PeerClient, counts map[cluster.Node]*int64) {
	t.Helper()

	var names []cluster.Node
	peers = make(map[cluster.Node]*cluster.PeerClient)
	counts = make(map[cluster.Node]*int64)

	for i := 0; i < n; i++ {
		engine, err := queel.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = engine.Close() })

		inner := server.NewInternalHandler(engine)
		count := new(int64)
		counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(count, 1)
			inner.ServeHTTP(w, r)
		})

		ts := httptest.NewServer(counting)
		t.Cleanup(ts.Close)

		name := cluster.Node(ts.URL)
		names = append(names, name)
		peers[name] = cluster.NewPeerClient(ts.URL)
		counts[name] = count
	}

	ring = cluster.NewRing(names, n)
	return ring, peers, counts
}

func totalRequests(counts map[cluster.Node]*int64) int64 {
	var total int64
	for _, c := range counts {
		total += atomic.LoadInt64(c)
	}
	return total
}

func TestWriteBatchGroupsRequestsByNodeNotByKey(t *testing.T) {
	ring, peers, counts := newCountingTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)

	const numKeys = 20
	ops := make([]queel.WriteOp, numKeys)
	for i := range ops {
		ops[i] = queel.WriteOp{Key: []byte(fmt.Sprintf("key-%d", i)), Value: []byte("v")}
	}

	if err := coord.WriteBatch(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	total := totalRequests(counts)
	// Without batching, this would be numKeys * replicationFactor(3) = 60
	// separate requests. Batched by node, it should be at most one request
	// per node (3), regardless of how many keys were written.
	if total > 3 {
		t.Fatalf("expected at most 3 requests (one per node) for a %d-key batch, got %d", numKeys, total)
	}
	if total == 0 {
		t.Fatal("expected at least some requests to have been made")
	}
}

// TestCastVoteSwitchUsesOneRoundTripPerNodeNotPerKey exercises the real
// Repository.CastVote path (which writes up to 3 independent keys when a
// user switches their vote: the new vote, the choice pointer, and a
// tombstone for the old vote) and checks the batched write stays bounded by
// cluster size rather than growing with the number of keys touched.
func TestCastVoteSwitchUsesOneRoundTripPerNodeNotPerKey(t *testing.T) {
	ring, peers, counts := newCountingTestCluster(t, 3)
	coord := cluster.NewCoordinator(ring, peers)
	repo := queel.NewRepository(cluster.NewDistributedStore(coord))

	text, err := repo.CreateText("Constitution", "Nous le peuple.")
	if err != nil {
		t.Fatal(err)
	}
	f1, err := repo.ProposeEdit(text.ID, 0, 4, "Vous", "alice")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := repo.ProposeEdit(text.ID, 0, 4, "Ils", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(f1.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	for node := range counts {
		atomic.StoreInt64(counts[node], 0)
	}

	// Switching an existing vote writes 3 independent keys: the new vote,
	// the updated choice pointer, and a tombstone for the old vote.
	if err := repo.CastVote(f2.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	total := totalRequests(counts)
	// 2 reads (fragment, choice) at up to 3 requests each, plus one batched
	// write of 3 keys at up to 3 requests (one per node) — 9 total, not the
	// 2*3 + 3*3 = 15 it would take with one request per key per replica.
	if total > 9 {
		t.Fatalf("expected at most 9 requests for a vote switch, got %d", total)
	}
}
