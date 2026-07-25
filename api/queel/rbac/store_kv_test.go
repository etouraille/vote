package rbac_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/bootstrap"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/rbac"
	"github.com/etouraille/queel/server"
)

func TestOpenWithStoreCreateGetListUpdateDelete(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	store := rbac.OpenWithStore(engine)
	t.Cleanup(func() { _ = store.Close() })

	user, err := store.CreateUser(false, rbac.Permissions{CanVote: true})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" {
		t.Fatal("expected a generated ID")
	}

	fetched, err := store.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.ID != user.ID || !fetched.Permissions.CanVote {
		t.Fatalf("got %+v", fetched)
	}

	if _, err := store.CreateUser(true, rbac.Permissions{}); err != nil {
		t.Fatal(err)
	}
	if got := len(store.ListUsers()); got != 2 {
		t.Fatalf("expected 2 users, got %d", got)
	}

	updated, err := store.UpdateUser(user.ID, false, rbac.Permissions{CanCreateText: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Permissions.CanVote || !updated.Permissions.CanCreateText {
		t.Fatalf("update did not replace permissions: %+v", updated.Permissions)
	}

	if err := store.DeleteUser(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUser(user.ID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestOpenWithStoreUnknownUserOperations(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	store := rbac.OpenWithStore(engine)

	if _, err := store.GetUser("does-not-exist"); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := store.UpdateUser("does-not-exist", false, rbac.Permissions{}); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := store.DeleteUser("does-not-exist"); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOpenWithStoreCreateUserWithIDIsIdempotent(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	store := rbac.OpenWithStore(engine)

	if _, err := store.CreateUserWithID("root-uuid", true, rbac.Permissions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUserWithID("root-uuid", true, rbac.Permissions{}); !errors.Is(err, rbac.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists bootstrapping the same UUID twice, got %v", err)
	}
}

func TestOpenWithStoreCloseDoesNotCloseTheUnderlyingStore(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	store := rbac.OpenWithStore(engine)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// engine must still be usable — Store.Close must not have closed the
	// caller's shared queel.Store out from under it (see OpenWithStore's
	// doc comment: the caller owns that lifecycle, not Store).
	if _, err := store.CreateUser(false, rbac.Permissions{}); err != nil {
		t.Fatalf("expected engine to still be usable after Store.Close, got %v", err)
	}
}

// bootstrapNode is one real cluster member: a real queel.Engine behind a
// real server.NewInternalHandler, exactly as bootstrap_test.go's own helper
// builds one — duplicated here (rather than exported from that package)
// since this is the one place outside queel/bootstrap itself that needs it.
type bootstrapNode struct {
	ts  *httptest.Server
	mux *http.ServeMux
}

func newBootstrapNode(t *testing.T) bootstrapNode {
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

	return bootstrapNode{ts: ts, mux: mux}
}

func joinAndServeGossip(t *testing.T, node bootstrapNode, cfg bootstrap.Config) *cluster.Coordinator {
	t.Helper()

	coordinator, membership, err := bootstrap.Join(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	node.mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))
	return coordinator
}

// TestOpenWithStoreOverDistributedStoreReplicatesAcrossNodes is the whole
// point of OpenWithStore: two independent nodes — each its own queel.Engine,
// its own HTTP server, exactly as two separate machines would be — end up
// seeing the same rbac data once wired through the same cluster protocol
// their domain repository would already use. A rbac.Store built with Open
// (local SQLite) could never do this once the two "nodes" aren't sharing a
// filesystem; a rbac.Store built with OpenWithStore over a
// cluster.DistributedStore does, because the replication happens over HTTP
// between the nodes, not over a shared disk.
func TestOpenWithStoreOverDistributedStoreReplicatesAcrossNodes(t *testing.T) {
	nodeA := newBootstrapNode(t)
	nodeB := newBootstrapNode(t)

	coordA := joinAndServeGossip(t, nodeA, bootstrap.Config{
		Self:              cluster.Node(nodeA.ts.URL),
		ReplicationFactor: 2,
		GossipInterval:    20 * time.Millisecond,
	})
	coordB := joinAndServeGossip(t, nodeB, bootstrap.Config{
		Self:              cluster.Node(nodeB.ts.URL),
		Seed:              cluster.Node(nodeA.ts.URL),
		ReplicationFactor: 2,
		GossipInterval:    20 * time.Millisecond,
	})

	storeA := rbac.OpenWithStore(cluster.NewDistributedStore(coordA))
	storeB := rbac.OpenWithStore(cluster.NewDistributedStore(coordB))

	user, err := storeA.CreateUser(true, rbac.Permissions{CanVote: true})
	if err != nil {
		t.Fatal(err)
	}

	// Poll: coordB's own background refresh loop needs a moment to learn
	// about nodeA via gossip before a quorum write from storeA is visible
	// through storeB's coordinator.
	deadline := time.Now().Add(1 * time.Second)
	var fetched *rbac.User
	for time.Now().Before(deadline) {
		fetched, err = storeB.GetUser(user.ID)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("storeB never saw the user storeA created: %v", err)
	}
	if !fetched.Root || !fetched.Permissions.CanVote {
		t.Fatalf("storeB should see exactly what storeA wrote, got %+v", fetched)
	}

	if _, err := storeB.UpdateUser(user.ID, false, rbac.Permissions{CanCreateText: true}); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(1 * time.Second)
	var reFetched *rbac.User
	for time.Now().Before(deadline) {
		reFetched, err = storeA.GetUser(user.ID)
		if err == nil && !reFetched.Root {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reFetched == nil || reFetched.Root || !reFetched.Permissions.CanCreateText {
		t.Fatalf("storeA should see storeB's update, got %+v (err=%v)", reFetched, err)
	}
}
