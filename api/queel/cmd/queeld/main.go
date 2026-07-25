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
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/etouraille/queel"
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
	clustered := false

	if selfAddr := os.Getenv("QUEEL_NODE_ADDRESS"); selfAddr != "" {
		coordinator, err := joinCluster(mux, cluster.Node(selfAddr))
		if err != nil {
			log.Fatal(err)
		}
		store = cluster.NewDistributedStore(coordinator)
		clustered = true
	}

	repo := queel.NewRepository(store)

	rbacPath := os.Getenv("QUEEL_RBAC_PATH")
	if rbacPath == "" {
		rbacPath = filepath.Join(dataDir, "rbac.json")
	}
	rbacStore, err := rbac.Open(rbacPath)
	if err != nil {
		log.Fatalf("rbac store: %v", err)
	}

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

// joinCluster wires up gossip-based membership for self, optionally joining
// through QUEEL_SEED_NODE, and returns a Coordinator that stays up to date
// with the cluster's membership as it changes.
func joinCluster(mux *http.ServeMux, self cluster.Node) (*cluster.Coordinator, error) {
	rf := 3
	if rfEnv := os.Getenv("QUEEL_REPLICATION_FACTOR"); rfEnv != "" {
		parsed, err := strconv.Atoi(rfEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid QUEEL_REPLICATION_FACTOR: %w", err)
		}
		rf = parsed
	}

	interval := 2 * time.Second
	if intervalEnv := os.Getenv("QUEEL_GOSSIP_INTERVAL"); intervalEnv != "" {
		parsed, err := time.ParseDuration(intervalEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid QUEEL_GOSSIP_INTERVAL: %w", err)
		}
		interval = parsed
	}

	membership := cluster.NewMembership(self)
	mux.HandleFunc("POST /internal/gossip", server.GossipHandler(membership))

	if seed := os.Getenv("QUEEL_SEED_NODE"); seed != "" {
		joinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := membership.Join(joinCtx, cluster.Node(seed)); err != nil {
			return nil, fmt.Errorf("joining cluster via seed %s: %w", seed, err)
		}
	}

	coordinator := cluster.NewCoordinator(cluster.NewRing([]cluster.Node{self}, 1), map[cluster.Node]*cluster.PeerClient{})
	coordinator.SetMembers(membership.AliveNodes(), rf)

	ctx := context.Background()
	membership.Start(ctx, interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			coordinator.SetMembers(membership.AliveNodes(), rf)
		}
	}()

	return coordinator, nil
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
