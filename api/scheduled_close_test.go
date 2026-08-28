package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

// noopEmbedder always fails — runScheduledCloseWorker must still close a
// due round even though indexing the fork it produces can't succeed
// against it, exactly as IndexFinalizedText's own doc comment promises.
type noopEmbedder struct{}

func (noopEmbedder) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedding unavailable in test")
}

func TestRunScheduledCloseWorkerClosesDueRounds(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := queel.NewRepository(engine)

	dueText, err := repo.CreateText("Due", "Contenu a modifier.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(dueText.ID, 0, len("Contenu"), "Contenu modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(dueText.ID, time.Now().Add(-time.Hour), "creator"); err != nil {
		t.Fatal(err)
	}

	// A second text scheduled well into the future must be left alone —
	// the worker shouldn't close everything it sees, only what's due.
	futureText, err := repo.CreateText("Future", "Autre contenu ici.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(futureText.ID, 0, len("Autre"), "Un autre", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(futureText.ID, time.Now().Add(7*24*time.Hour), "creator"); err != nil {
		t.Fatal(err)
	}

	// Unreachable on purpose: proves a worker that can't reach
	// Qdrant/Ollama still closes the round instead of getting stuck on the
	// best-effort indexing step.
	index := newSearchIndexer(noopEmbedder{}, newQdrantClient("http://127.0.0.1:1", "test"), false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// nil notifier: textNotifier.notify returns early on a nil receiver by
	// design (notifying is a side effect of closing, never a precondition
	// of it), so these tests exercise the closing itself without standing
	// up a dispatcher.
	go runScheduledCloseWorker(ctx, repo, index, nil, 20*time.Millisecond, nil)

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := repo.CurrentRound(dueText.ID); errors.Is(err, queel.ErrNotFound) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := repo.CurrentRound(dueText.ID); !errors.Is(err, queel.ErrNotFound) {
		t.Fatalf("expected the due round to be closed (no current round left), got err=%v", err)
	}

	if _, err := repo.CurrentRound(futureText.ID); err != nil {
		t.Fatalf("expected the future-scheduled round to still be open, got err=%v", err)
	}
}

// TestRunScheduledCloseWorkerRespectsIsLeader is the cluster-mode half:
// a node that isn't the elected leader (see main.go's isScheduledCloseLeader)
// must never close anything, no matter how overdue — otherwise every node
// in the cluster would redundantly race to close the same rounds.
func TestRunScheduledCloseWorkerRespectsIsLeader(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := queel.NewRepository(engine)

	text, err := repo.CreateText("Due", "Contenu a modifier.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, 0, len("Contenu"), "Contenu modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(text.ID, time.Now().Add(-time.Hour), "creator"); err != nil {
		t.Fatal(err)
	}

	index := newSearchIndexer(noopEmbedder{}, newQdrantClient("http://127.0.0.1:1", "test"), false)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	notLeader := func() bool { return false }
	runScheduledCloseWorker(ctx, repo, index, nil, 20*time.Millisecond, notLeader)

	if _, err := repo.CurrentRound(text.ID); err != nil {
		t.Fatalf("a non-leader must never close a round, even an overdue one, got err=%v", err)
	}

	// Now let it win the "election" and confirm the exact same overdue
	// round it just ignored gets closed as soon as it is the leader.
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	isLeader := func() bool { return true }
	go runScheduledCloseWorker(ctx2, repo, index, nil, 20*time.Millisecond, isLeader)

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := repo.CurrentRound(text.ID); errors.Is(err, queel.ErrNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected the round to close once this worker became the leader")
}

// clusterMembershipNode pairs a real cluster.Membership with the
// httptest.Server serving its /internal/gossip endpoint — the same wiring
// queel/cluster's own membership_test.go uses, reproduced here since that
// helper is unexported and this package needs its own real, gossip-driven
// Membership instances to test isScheduledCloseLeader against (not just
// hand-crafted true/false stubs, which TestRunScheduledCloseWorkerRespectsIsLeader
// already covers).
type clusterMembershipNode struct {
	membership *cluster.Membership
	self       cluster.Node
	ts         *httptest.Server
}

func newClusterMembershipNodes(t *testing.T, n int) []clusterMembershipNode {
	t.Helper()

	nodes := make([]clusterMembershipNode, n)
	for i := 0; i < n; i++ {
		mux := http.NewServeMux()
		ts := httptest.NewServer(mux)
		t.Cleanup(ts.Close)

		self := cluster.Node(ts.URL)
		membership := cluster.NewMembership(self)
		mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))

		nodes[i] = clusterMembershipNode{membership: membership, self: self, ts: ts}
	}
	return nodes
}

// convergeGossip joins every node but seed to it, then runs enough gossip
// rounds for full membership knowledge to spread to all of them — the same
// pattern (and round count) queel/cluster's own convergence tests rely on.
func convergeGossip(ctx context.Context, t *testing.T, nodes []clusterMembershipNode, seed cluster.Node) {
	t.Helper()
	for _, n := range nodes {
		if n.self == seed {
			continue
		}
		if err := n.membership.Join(ctx, seed); err != nil {
			t.Fatal(err)
		}
	}
	for round := 0; round < 10; round++ {
		for _, n := range nodes {
			_ = n.membership.Gossip(ctx)
		}
	}
}

// electedLeaders returns the indices of every node in nodes that currently
// considers itself the scheduled-close leader.
func electedLeaders(nodes []clusterMembershipNode) []int {
	var leaders []int
	for i, n := range nodes {
		if isScheduledCloseLeader(n.membership, n.self) {
			leaders = append(leaders, i)
		}
	}
	return leaders
}

func TestIsScheduledCloseLeaderPicksTheLowestAliveNodeConsistently(t *testing.T) {
	nodes := newClusterMembershipNodes(t, 3)
	ctx := context.Background()
	convergeGossip(ctx, t, nodes, nodes[0].self)

	leaders := electedLeaders(nodes)
	if len(leaders) != 1 {
		t.Fatalf("expected exactly one leader among %d converged nodes, got %v", len(nodes), leaders)
	}

	// Every node's own AliveNodes() should agree on who that is — that's
	// the whole point of a rule based on a value gossip has already
	// converged on, rather than something each node could compute
	// differently.
	elected := nodes[leaders[0]].self
	for i, n := range nodes {
		alive := n.membership.AliveNodes()
		if len(alive) == 0 || alive[0] != elected {
			t.Fatalf("node %d disagrees on the leader: its own AliveNodes() = %v, elected = %s", i, alive, elected)
		}
	}
}

func TestIsScheduledCloseLeaderFailsOverWhenLeaderDies(t *testing.T) {
	nodes := newClusterMembershipNodes(t, 3)
	ctx := context.Background()
	convergeGossip(ctx, t, nodes, nodes[0].self)

	before := electedLeaders(nodes)
	if len(before) != 1 {
		t.Fatalf("expected exactly one leader before the failure, got %v", before)
	}
	leaderIdx := before[0]

	// The elected leader is gone for good; nobody will ever reach it again.
	nodes[leaderIdx].ts.Close()
	survivors := make([]clusterMembershipNode, 0, len(nodes)-1)
	for i, n := range nodes {
		if i != leaderIdx {
			survivors = append(survivors, n)
		}
	}
	for round := 0; round < 15; round++ {
		for _, n := range survivors {
			_ = n.membership.Gossip(ctx)
		}
	}

	after := electedLeaders(survivors)
	if len(after) != 1 {
		t.Fatalf("expected exactly one new leader among the %d survivors after failover, got %v", len(survivors), after)
	}
}

// TestRunScheduledCloseWorkerInClusterModeOnlyLeaderCloses is the full
// integration: two nodes with a real, gossip-converged cluster.Membership
// each run runScheduledCloseWorker wired exactly as main.go wires it
// (isLeader = isScheduledCloseLeader(membership, self)), against the same
// underlying repository — standing in for the replicated store every node
// in a real cluster would share. Only the elected leader may close the due
// round; run the follower first, on its own, to prove it leaves an overdue
// round alone rather than just happening not to have won a race.
func TestRunScheduledCloseWorkerInClusterModeOnlyLeaderCloses(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := queel.NewRepository(engine)

	text, err := repo.CreateText("Due", "Contenu a modifier.", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, 0, len("Contenu"), "Contenu modifie", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ScheduleRoundClose(text.ID, time.Now().Add(-time.Hour), "creator"); err != nil {
		t.Fatal(err)
	}

	nodes := newClusterMembershipNodes(t, 2)
	ctx := context.Background()
	convergeGossip(ctx, t, nodes, nodes[0].self)

	leaders := electedLeaders(nodes)
	if len(leaders) != 1 {
		t.Fatalf("expected exactly one leader among 2 nodes, got %v", leaders)
	}
	leader := nodes[leaders[0]]
	var follower clusterMembershipNode
	for i, n := range nodes {
		if i != leaders[0] {
			follower = n
		}
	}

	index := newSearchIndexer(noopEmbedder{}, newQdrantClient("http://127.0.0.1:1", "test"), false)

	followerCtx, cancelFollower := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelFollower()
	runScheduledCloseWorker(followerCtx, repo, index, nil, 20*time.Millisecond, func() bool {
		return isScheduledCloseLeader(follower.membership, follower.self)
	})
	if _, err := repo.CurrentRound(text.ID); err != nil {
		t.Fatalf("the follower node must not have closed the round, got err=%v", err)
	}

	leaderCtx, cancelLeader := context.WithTimeout(context.Background(), time.Second)
	defer cancelLeader()
	go runScheduledCloseWorker(leaderCtx, repo, index, nil, 20*time.Millisecond, func() bool {
		return isScheduledCloseLeader(leader.membership, leader.self)
	})

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := repo.CurrentRound(text.ID); errors.Is(err, queel.ErrNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected the leader node to close the overdue round")
}
