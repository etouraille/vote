package cluster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

// antiEntropyNode is one real cluster member for these tests: a real
// queel.Engine behind a real server.NewInternalHandler, so BuildTree,
// MerkleTree/MerkleBucket, and Reconcile all exercise actual HTTP and
// actual on-disk storage, not fakes.
type antiEntropyNode struct {
	engine *queel.Engine
	ts     *httptest.Server
}

func newAntiEntropyNode(t *testing.T) antiEntropyNode {
	t.Helper()

	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	mux := http.NewServeMux()
	mux.Handle("/internal/", server.NewInternalHandler(engine))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return antiEntropyNode{engine: engine, ts: ts}
}

// putRaw stores key on node exactly as a real cluster write would leave it
// — a cluster.Entry-wrapped JSON payload — bypassing HTTP/Coordinator
// entirely so tests can construct a specific divergence directly.
func putRaw(t *testing.T, node antiEntropyNode, key string, entry cluster.Entry) {
	t.Helper()
	payload, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.engine.Put([]byte(key), payload); err != nil {
		t.Fatal(err)
	}
}

func getRaw(t *testing.T, node antiEntropyNode, key string) (cluster.Entry, bool) {
	t.Helper()
	value, found, err := node.engine.Get([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return cluster.Entry{}, false
	}
	var entry cluster.Entry
	if err := json.Unmarshal(value, &entry); err != nil {
		t.Fatal(err)
	}
	return entry, true
}

const testBuckets = 16

func TestReconcileNoOpWhenTreesAlreadyMatch(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	entry := cluster.NewEntry([]byte("same everywhere"), false)
	putRaw(t, a, "shared-key", entry)
	putRaw(t, b, "shared-key", entry)

	repaired, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 0 {
		t.Fatalf("expected 0 repairs between already-identical nodes, got %d", repaired)
	}
}

func TestReconcilePullsAKeyOnlyThePeerHas(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	putRaw(t, b, "peer-only-key", cluster.NewEntry([]byte("from b"), false))

	repaired, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("expected 1 repair, got %d", repaired)
	}

	entry, found := getRaw(t, a, "peer-only-key")
	if !found {
		t.Fatal("expected a to have pulled peer-only-key from b")
	}
	if string(entry.Value) != "from b" {
		t.Fatalf("pulled value = %q, want %q", entry.Value, "from b")
	}
}

func TestReconcilePushesAKeyOnlyTheLocalNodeHas(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	putRaw(t, a, "local-only-key", cluster.NewEntry([]byte("from a"), false))

	repaired, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("expected 1 repair, got %d", repaired)
	}

	entry, found := getRaw(t, b, "local-only-key")
	if !found {
		t.Fatal("expected b to have received local-only-key from a")
	}
	if string(entry.Value) != "from a" {
		t.Fatalf("pushed value = %q, want %q", entry.Value, "from a")
	}
}

func TestReconcileNewerEntryWinsRegardlessOfSide(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	older := cluster.Entry{Value: []byte("stale"), Timestamp: 1000}
	newer := cluster.Entry{Value: []byte("fresh"), Timestamp: 2000}

	// a has the stale value, b has the fresh one — a must pull b's.
	putRaw(t, a, "race-key", older)
	putRaw(t, b, "race-key", newer)

	if _, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets); err != nil {
		t.Fatal(err)
	}

	aEntry, _ := getRaw(t, a, "race-key")
	if string(aEntry.Value) != "fresh" {
		t.Fatalf("a's race-key = %q after reconcile, want %q (b's newer value)", aEntry.Value, "fresh")
	}

	// Reverse roles: now a has the fresher value than b — running
	// Reconcile from a's side again must push it to b.
	putRaw(t, a, "race-key-2", newer)
	putRaw(t, b, "race-key-2", older)

	if _, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets); err != nil {
		t.Fatal(err)
	}
	bEntry, _ := getRaw(t, b, "race-key-2")
	if string(bEntry.Value) != "fresh" {
		t.Fatalf("b's race-key-2 = %q after reconcile, want %q (a's newer value)", bEntry.Value, "fresh")
	}
}

func TestReconcileConvergesManyScatteredDifferences(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	// Shared baseline both nodes agree on.
	for i := 0; i < 50; i++ {
		key := keyFor(i)
		putRaw(t, a, key, cluster.Entry{Value: []byte("baseline"), Timestamp: 1})
		putRaw(t, b, key, cluster.Entry{Value: []byte("baseline"), Timestamp: 1})
	}

	// Scatter divergences across the keyspace: some a-only, some b-only,
	// some conflicting with different winners.
	putRaw(t, a, "only-a-1", cluster.NewEntry([]byte("a1"), false))
	putRaw(t, a, "only-a-2", cluster.NewEntry([]byte("a2"), false))
	putRaw(t, b, "only-b-1", cluster.NewEntry([]byte("b1"), false))
	putRaw(t, a, "conflict-1", cluster.Entry{Value: []byte("a-wins"), Timestamp: 500})
	putRaw(t, b, "conflict-1", cluster.Entry{Value: []byte("b-loses"), Timestamp: 100})
	putRaw(t, a, "conflict-2", cluster.Entry{Value: []byte("a-loses"), Timestamp: 100})
	putRaw(t, b, "conflict-2", cluster.Entry{Value: []byte("b-wins"), Timestamp: 500})

	repaired, err := cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets)
	if err != nil {
		t.Fatal(err)
	}
	// One repair per divergent key, not per side: 2 a-only (pushed to b) +
	// 1 b-only (pulled from b) + conflict-1 (b's stale copy overwritten) +
	// conflict-2 (a's stale copy overwritten) = 5.
	if repaired != 5 {
		t.Fatalf("expected 5 repairs, got %d", repaired)
	}

	for _, node := range []antiEntropyNode{a, b} {
		if e, ok := getRaw(t, node, "only-a-1"); !ok || string(e.Value) != "a1" {
			t.Fatalf("only-a-1 missing or wrong after convergence: %+v, ok=%v", e, ok)
		}
		if e, ok := getRaw(t, node, "only-a-2"); !ok || string(e.Value) != "a2" {
			t.Fatalf("only-a-2 missing or wrong after convergence: %+v, ok=%v", e, ok)
		}
		if e, ok := getRaw(t, node, "only-b-1"); !ok || string(e.Value) != "b1" {
			t.Fatalf("only-b-1 missing or wrong after convergence: %+v, ok=%v", e, ok)
		}
		if e, ok := getRaw(t, node, "conflict-1"); !ok || string(e.Value) != "a-wins" {
			t.Fatalf("conflict-1 = %+v, ok=%v, want a-wins (higher timestamp)", e, ok)
		}
		if e, ok := getRaw(t, node, "conflict-2"); !ok || string(e.Value) != "b-wins" {
			t.Fatalf("conflict-2 = %+v, ok=%v, want b-wins (higher timestamp)", e, ok)
		}
	}

	// The trees should now agree completely — a second Reconcile repairs
	// nothing further.
	repaired, err = cluster.Reconcile(context.Background(), a.engine, cluster.NewPeerClient(b.ts.URL), testBuckets)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 0 {
		t.Fatalf("expected 0 repairs once converged, got %d", repaired)
	}
}

func TestRunAntiEntropyConvergesInTheBackground(t *testing.T) {
	a := newAntiEntropyNode(t)
	b := newAntiEntropyNode(t)

	putRaw(t, b, "background-key", cluster.NewEntry([]byte("from background sync"), false))

	membership := cluster.NewMembership(cluster.Node(a.ts.URL))
	// HandleGossip merges in b directly — sidesteps a full gossip exchange,
	// this test only cares that RunAntiEntropy itself converges once it
	// knows about a peer, not membership discovery (already covered by
	// membership_test.go).
	membership.HandleGossip([]cluster.Member{{Node: cluster.Node(b.ts.URL), Status: cluster.StatusAlive, Incarnation: time.Now().UnixNano()}})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go cluster.RunAntiEntropy(ctx, a.engine, membership, cluster.Node(a.ts.URL), 20*time.Millisecond, testBuckets)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, found := getRaw(t, a, "background-key"); found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("RunAntiEntropy never converged background-key from b within 1s")
}

func keyFor(i int) string {
	return "baseline-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}
