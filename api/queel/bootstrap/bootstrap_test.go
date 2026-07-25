package bootstrap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/bootstrap"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/server"
)

// bootstrapNode is one cluster member for these tests: a real queel.Engine
// behind a real server.NewInternalHandler, exactly what a live node would
// run — so Coordinator reads/writes exercised against the bootstrap.Join
// result travel over actual HTTP, not a fake.
//
// It starts with only the put/get/scan replication routes mounted — the
// gossip route needs a *cluster.Membership to hand server.GossipHandler,
// and that only exists once bootstrap.Join returns one. Callers must mount
// it themselves afterward (see joinAndServeGossip), exactly mirroring what
// a real main() does: bootstrap.Join deliberately never touches an HTTP mux
// (see the package doc), so nothing in this test package can either, before
// Join has run.
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

// joinAndServeGossip calls bootstrap.Join for node and then mounts the
// resulting Membership's gossip endpoint on node's own mux — the two-step
// dance a real caller performs, since bootstrap.Join has no mux of its own
// to mount anything on.
func joinAndServeGossip(t *testing.T, node bootstrapNode, cfg bootstrap.Config) (*cluster.Coordinator, *cluster.Membership) {
	t.Helper()

	coordinator, membership, err := bootstrap.Join(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	node.mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))
	return coordinator, membership
}

func TestJoinRequiresSelf(t *testing.T) {
	_, _, err := bootstrap.Join(context.Background(), bootstrap.Config{})
	if err == nil {
		t.Fatal("expected an error for a Config with no Self")
	}
}

func TestJoinFoundsANewClusterUsableStandalone(t *testing.T) {
	node := newBootstrapNode(t)

	coordinator, membership := joinAndServeGossip(t, node, bootstrap.Config{
		Self: cluster.Node(node.ts.URL),
	})
	if got := membership.AliveNodes(); len(got) != 1 || got[0] != cluster.Node(node.ts.URL) {
		t.Fatalf("AliveNodes() = %v, want just self", got)
	}

	ctx := context.Background()
	if err := coordinator.Put(ctx, "greeting", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	value, found, err := coordinator.Get(ctx, "greeting")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(value) != "hello" {
		t.Fatalf("Get(\"greeting\") = %q, %v, want \"hello\", true", value, found)
	}
}

func TestJoinThroughSeedReplicatesAcrossBothNodes(t *testing.T) {
	nodeA := newBootstrapNode(t)
	nodeB := newBootstrapNode(t)

	coordA, _ := joinAndServeGossip(t, nodeA, bootstrap.Config{
		Self:              cluster.Node(nodeA.ts.URL),
		ReplicationFactor: 2,
		GossipInterval:    20 * time.Millisecond,
	})

	coordB, membershipB := joinAndServeGossip(t, nodeB, bootstrap.Config{
		Self:              cluster.Node(nodeB.ts.URL),
		Seed:              cluster.Node(nodeA.ts.URL),
		ReplicationFactor: 2,
		GossipInterval:    20 * time.Millisecond,
	})

	// nodeA's own gossip/refresh loop only started with a replication factor
	// of 1 (it founded the cluster alone) — nudge it to notice nodeB too, the
	// same way periodic gossip would once both are running for real.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && len(membershipB.AliveNodes()) < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(membershipB.AliveNodes()) != 2 {
		t.Fatalf("nodeB's membership never learned about both nodes: %v", membershipB.AliveNodes())
	}

	ctx := context.Background()
	if err := coordB.Put(ctx, "shared-key", []byte("written via B")); err != nil {
		t.Fatal(err)
	}

	// Poll: coordA's own background refresh loop (from its own Join call)
	// needs a moment to pick up nodeB via gossip before it can see the key
	// nodeB wrote — this cluster only replicates to nodes each side's
	// Coordinator currently knows about.
	deadline = time.Now().Add(1 * time.Second)
	var value []byte
	var found bool
	var err error
	for time.Now().Before(deadline) {
		value, found, err = coordA.Get(ctx, "shared-key")
		if err == nil && found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found || string(value) != "written via B" {
		t.Fatalf("Get(\"shared-key\") via nodeA = %q, %v, err=%v — want the value nodeB wrote to be visible from nodeA once membership converges", value, found, err)
	}
}

func TestConfigFromEnvDisabledWithoutNodeAddress(t *testing.T) {
	t.Setenv(bootstrap.EnvNodeAddress, "")

	cfg, enabled, err := bootstrap.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatalf("expected clustering to be disabled with %s unset, got Config %+v", bootstrap.EnvNodeAddress, cfg)
	}
}

func TestConfigFromEnvReadsAllFourVars(t *testing.T) {
	t.Setenv(bootstrap.EnvNodeAddress, "http://node-self:9090")
	t.Setenv(bootstrap.EnvSeedNode, "http://node-seed:9090")
	t.Setenv(bootstrap.EnvReplicationFactor, "5")
	t.Setenv(bootstrap.EnvGossipInterval, "500ms")

	cfg, enabled, err := bootstrap.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("expected clustering to be enabled")
	}
	want := bootstrap.Config{
		Self:              "http://node-self:9090",
		Seed:              "http://node-seed:9090",
		ReplicationFactor: 5,
		GossipInterval:    500 * time.Millisecond,
	}
	if cfg != want {
		t.Fatalf("ConfigFromEnv() = %+v, want %+v", cfg, want)
	}
}

func TestConfigFromEnvRejectsMalformedReplicationFactor(t *testing.T) {
	t.Setenv(bootstrap.EnvNodeAddress, "http://node-self:9090")
	t.Setenv(bootstrap.EnvReplicationFactor, "not-a-number")

	if _, _, err := bootstrap.ConfigFromEnv(); err == nil {
		t.Fatal("expected an error for a non-numeric QUEEL_REPLICATION_FACTOR")
	}
}

func TestConfigFromEnvRejectsMalformedGossipInterval(t *testing.T) {
	t.Setenv(bootstrap.EnvNodeAddress, "http://node-self:9090")
	t.Setenv(bootstrap.EnvGossipInterval, "not-a-duration")

	if _, _, err := bootstrap.ConfigFromEnv(); err == nil {
		t.Fatal("expected an error for a malformed QUEEL_GOSSIP_INTERVAL")
	}
}

func TestJoinFromEnvDisabledWithoutNodeAddress(t *testing.T) {
	t.Setenv(bootstrap.EnvNodeAddress, "")

	coordinator, membership, enabled, err := bootstrap.JoinFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if enabled || coordinator != nil || membership != nil {
		t.Fatalf("expected disabled with nil coordinator/membership, got enabled=%v coordinator=%v membership=%v", enabled, coordinator, membership)
	}
}
