// Command queeld runs queel as its own standalone server: a separate
// process, with its own on-disk data — decoupled from whatever other
// project ends up talking to it through queel/client. It listens on a Unix
// domain socket if QUEEL_SOCKET is set, otherwise on TCP (PORT, default
// 9090); the wire protocol (HTTP/JSON) is identical either way.
//
// If QUEEL_NODE_ADDRESS is set (this node's own reachable address), it joins
// a cluster: it still stores data locally and serves its own internal
// replication endpoints for its peers, but the public API is now backed by
// a replicated, quorum-consistent Store spread across
// QUEEL_REPLICATION_FACTOR nodes (default 3) instead of just its own local
// engine.
//
// There is no static, complete node list to configure: a joining node only
// needs QUEEL_SEED_NODE, the address of one existing cluster member, and
// learns the rest of the membership from it by gossip (QUEEL_GOSSIP_INTERVAL,
// default 2s). The very first node of a new cluster leaves QUEEL_SEED_NODE
// unset — it simply starts a cluster containing only itself.
//
// All four QUEEL_NODE_ADDRESS/QUEEL_SEED_NODE/QUEEL_REPLICATION_FACTOR/
// QUEEL_GOSSIP_INTERVAL variables, and the join sequence itself, are handled
// by queel/bootstrap — the same package a host application embedding queel
// directly (e.g. this repo's api) uses to become a cluster node too.
//
// rbac follows the same split: single-node, it's a local SQLite database
// (QUEEL_RBAC_PATH); clustered, it rides the same replicated Store as
// everything else instead (see queel/rbac's OpenWithStore), since a local
// file can't safely be shared once nodes aren't on the same machine.
//
// Clustered nodes also run a background anti-entropy job (see
// queel/cluster.RunAntiEntropy): every QUEEL_ANTI_ENTROPY_INTERVAL (default
// 5m) this node compares its entire keyspace against a random peer's via a
// Merkle tree and repairs whatever differs — catching data a peer missed
// during a partition or disk loss that nobody happens to read again, which
// quorum reads' read-repair alone never would.
//
// Before deliberately taking a clustered node out of service, POST to its
// internal /internal/decommission first (see cluster.Decommission): it
// hands off, to whichever nodes will inherit responsibility for them, every
// key this node holds that the *other* nodes don't already have an
// equally-or-more-recent copy of. Only stop the process afterward — doing
// that first means the cluster never dips below its replication factor
// even briefly, unlike an unplanned crash, which leans on the anti-entropy
// job above to notice and repair after the fact instead.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/bootstrap"
	"github.com/etouraille/queel/cluster"
	"github.com/etouraille/queel/rbac"
	"github.com/etouraille/queel/server"
)

func main() {
	dataDir := os.Getenv("QUEEL_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	engine, err := queel.Open(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()

	mux := http.NewServeMux()
	mux.Handle("/internal/", server.NewInternalHandler(engine))

	var store queel.Store = engine

	coordinator, membership, clustered, err := bootstrap.JoinFromEnv(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if clustered {
		self := cluster.Node(os.Getenv(bootstrap.EnvNodeAddress))
		replicationFactor, err := bootstrap.ReplicationFactorFromEnv()
		if err != nil {
			log.Fatal(err)
		}

		mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))

		// A proactive, operator-triggered handoff of this node's data to
		// whichever nodes will inherit it, run before actually stopping
		// this process — see cluster.Decommission's doc comment for why
		// that's a meaningfully different (and better) guarantee than
		// waiting for the anti-entropy job below to notice on its own.
		mux.HandleFunc("POST /internal/decommission", server.DecommissionHandler(engine, membership, self, replicationFactor))

		// See queel/cluster.RunAntiEntropy's doc comment: catches whatever
		// read-repair alone misses, by periodically comparing this node's
		// entire keyspace against a random peer's via a Merkle tree instead
		// of waiting for someone to re-read a key this node fell behind on.
		antiEntropyInterval, err := bootstrap.AntiEntropyIntervalFromEnv()
		if err != nil {
			log.Fatal(err)
		}
		go cluster.RunAntiEntropy(context.Background(), engine, membership, self, antiEntropyInterval, cluster.DefaultAntiEntropyBuckets)

		store = cluster.NewDistributedStore(coordinator)
	}

	repo := queel.NewRepository(store)

	// In cluster mode, rbac rides the same replicated store as everything
	// else instead of a local SQLite file — see queel/rbac's OpenWithStore
	// doc comment. QUEEL_RBAC_PATH is only consulted, and only matters, in
	// single-node mode.
	var rbacStore *rbac.Store
	if clustered {
		rbacStore = rbac.OpenWithStore(store)
	} else {
		rbacPath := os.Getenv("QUEEL_RBAC_PATH")
		if rbacPath == "" {
			rbacPath = filepath.Join(dataDir, "rbac.db")
		}
		rbacStore, err = rbac.Open(rbacPath)
		if err != nil {
			log.Fatalf("rbac store: %v", err)
		}
	}
	defer rbacStore.Close()

	// See api/.env.example for the matching QUEEL_ROOT_UUID/JWT_SECRET
	// story on the issuer side: this bootstrap only ever needs to run once
	// per rbac directory, on whichever node created it first.
	if rootUUID := os.Getenv("QUEEL_ROOT_UUID"); rootUUID != "" {
		if _, err := rbacStore.CreateUserWithID(rootUUID, true, rbac.Permissions{}); err != nil && !errors.Is(err, rbac.ErrAlreadyExists) {
			log.Fatalf("bootstrapping root rbac user: %v", err)
		}
	}

	rbacSocket := os.Getenv("QUEEL_RBAC_SOCKET")
	if rbacSocket == "" {
		rbacSocket = filepath.Join(dataDir, "rbac.sock")
	}
	go func() {
		if err := rbac.ServeSocket(context.Background(), rbacSocket, rbacStore); err != nil {
			log.Printf("rbac socket stopped: %v", err)
		}
	}()

	// Unauthenticated, same as /internal/... — meant for orchestration
	// (a Kubernetes probe, a load balancer's own health check), not an
	// end-user client.
	mux.HandleFunc("GET /healthz", server.HealthHandler(store, membership))

	// QUEEL_JWT_SECRET must match whatever issued the caller's bearer
	// token (typically this repo's api process — see rbac.SignToken).
	// Left unset, every mutating route stays unauthenticated, exactly as
	// queeld has always behaved.
	jwtSecret := []byte(os.Getenv("QUEEL_JWT_SECRET"))
	mux.Handle("/", server.NewHandler(repo, jwtSecret))

	listener, address, err := listen()
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	log.Printf("queel listening on %s (data: %s, clustered: %v)", address, dataDir, clustered)
	if err := http.Serve(listener, mux); err != nil {
		log.Fatal(err)
	}
}

// listen opens a Unix domain socket at QUEEL_SOCKET if set, otherwise a TCP
// listener on PORT (default 9090).
func listen() (net.Listener, string, error) {
	if socketPath := os.Getenv("QUEEL_SOCKET"); socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return nil, "", err
		}
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			return nil, "", err
		}
		return listener, "unix:" + socketPath, nil
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, "", err
	}
	return listener, "tcp::" + port, nil
}
