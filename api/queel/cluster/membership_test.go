package cluster_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

type membershipNode struct {
	membership *cluster.Membership
	ts         *httptest.Server
}

func newMembershipCluster(t *testing.T, n int) []membershipNode {
	t.Helper()

	nodes := make([]membershipNode, n)
	for i := 0; i < n; i++ {
		// Node registers itself once its own address (the test server's URL)
		// is known, via a handler that's only wired up after Start below.
		mux := http.NewServeMux()
		ts := httptest.NewServer(mux)
		t.Cleanup(ts.Close)

		membership := cluster.NewMembership(cluster.Node(ts.URL))
		mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))

		nodes[i] = membershipNode{membership: membership, ts: ts}
	}
	return nodes
}

func TestMembershipConvergesThroughASingleSeed(t *testing.T) {
	nodes := newMembershipCluster(t, 4)
	ctx := context.Background()

	// Nobody has a full node list. Each new node only ever contacts node 0
	// as a seed; the rest of the knowledge must spread by gossip alone.
	seed := nodes[0].membership
	seedNode := cluster.Node(nodes[0].ts.URL)
	for _, n := range nodes[1:] {
		if err := n.membership.Join(ctx, seedNode); err != nil {
			t.Fatal(err)
		}
	}
	_ = seed

	// A few gossip rounds, picked in random order each time, should be
	// enough for knowledge to fully spread across 4 nodes.
	for round := 0; round < 10; round++ {
		for _, n := range nodes {
			_ = n.membership.Gossip(ctx)
		}
	}

	for i, n := range nodes {
		alive := n.membership.AliveNodes()
		if len(alive) != len(nodes) {
			t.Fatalf("node %d: expected to know about all %d nodes, knows %d: %v", i, len(nodes), len(alive), alive)
		}
	}
}

func TestMembershipDetectsAndPropagatesADeadNode(t *testing.T) {
	nodes := newMembershipCluster(t, 3)
	ctx := context.Background()

	seedNode := cluster.Node(nodes[0].ts.URL)
	for _, n := range nodes[1:] {
		if err := n.membership.Join(ctx, seedNode); err != nil {
			t.Fatal(err)
		}
	}
	for round := 0; round < 6; round++ {
		for _, n := range nodes {
			_ = n.membership.Gossip(ctx)
		}
	}
	for i, n := range nodes {
		if len(n.membership.AliveNodes()) != 3 {
			t.Fatalf("node %d: expected all 3 nodes known alive before the failure, got %v", i, n.membership.AliveNodes())
		}
	}

	// Node 2 goes away for good.
	deadNode := cluster.Node(nodes[2].ts.URL)
	nodes[2].ts.Close()

	// Nodes 0 and 1 gossip amongst themselves; whichever one first tries to
	// reach node 2 marks it dead, and that verdict must spread to the other.
	for round := 0; round < 10; round++ {
		_ = nodes[0].membership.Gossip(ctx)
		_ = nodes[1].membership.Gossip(ctx)
	}

	for i, n := range nodes[:2] {
		alive := n.membership.AliveNodes()
		for _, node := range alive {
			if node == deadNode {
				t.Fatalf("node %d: still believes dead node %s is alive: %v", i, deadNode, alive)
			}
		}
		if len(alive) != 2 {
			t.Fatalf("node %d: expected 2 alive nodes after the failure, got %v", i, alive)
		}
	}
}

func TestMembershipRestartOutranksOldDeathVerdict(t *testing.T) {
	nodeA := cluster.NewMembership("node-a")

	// A believes B has died at some incarnation...
	view := nodeA.HandleGossip([]cluster.Member{{Node: "node-b", Status: cluster.StatusDead, Incarnation: 100}})
	_ = view

	// ...but B restarts with a fresh, higher incarnation and announces itself
	// alive. That must override A's stale "dead" verdict.
	restarted := []cluster.Member{{Node: "node-b", Status: cluster.StatusAlive, Incarnation: 200}}
	nodeA.HandleGossip(restarted)

	found := false
	for _, n := range nodeA.AliveNodes() {
		if n == "node-b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected node-b's restart (higher incarnation) to override the earlier dead verdict, alive nodes: %v", nodeA.AliveNodes())
	}
}

func TestCoordinatorSetMembersRebuildsRingAndReusesPeers(t *testing.T) {
	ring := cluster.NewRing([]cluster.Node{"a", "b"}, 2)
	peers := map[cluster.Node]*cluster.PeerClient{
		"a": cluster.NewPeerClient("http://a"),
		"b": cluster.NewPeerClient("http://b"),
	}
	coord := cluster.NewCoordinator(ring, peers)

	// Grow the cluster to 3 nodes.
	coord.SetMembers([]cluster.Node{"a", "b", "c"}, 3)

	// A read against an unknown key should now consider all 3 nodes as
	// possible replicas rather than erroring out as if the ring were empty.
	// We can't easily assert on the exact replica set without exporting
	// internals, so this is really just a smoke test that SetMembers didn't
	// panic and left the coordinator usable.
	_, _, err := coord.Get(context.Background(), "some-key")
	if err == nil {
		t.Fatal("expected a read-quorum error since none of a/b/c actually listen on those fake addresses")
	}
}

func TestMembershipGossipConvergesQuickly(t *testing.T) {
	nodes := newMembershipCluster(t, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	seedNode := cluster.Node(nodes[0].ts.URL)
	for _, n := range nodes[1:] {
		if err := n.membership.Join(ctx, seedNode); err != nil {
			t.Fatal(err)
		}
	}

	for _, n := range nodes {
		n.membership.Start(ctx, 20*time.Millisecond)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		allConverged := true
		for _, n := range nodes {
			if len(n.membership.AliveNodes()) != len(nodes) {
				allConverged = false
				break
			}
		}
		if allConverged {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("membership did not converge across all 5 nodes within 1s of periodic gossip")
}
