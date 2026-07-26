// Package bootstrap wires up gossip-based cluster membership and keeps a
// Coordinator in sync with it — the sequence every queel binary that wants
// to run as a cluster node needs, whether that's queeld (queel/cmd/queeld)
// or a host application embedding queel directly (this repo's api). Before
// this package existed, both carried their own copy of it.
//
// It deliberately stops at membership + coordination: exposing the
// resulting Membership and internal replication endpoints over HTTP (see
// queel/server.NewInternalHandler and server.GossipHandler) is left to the
// caller, since where those live — same mux as the public API, a dedicated
// internal-only port, a Unix socket — is a deployment decision this
// package has no business making for its caller.
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/etouraille/queel/cluster"
)

// Environment variable names ConfigFromEnv/JoinFromEnv read — both queeld
// and api set these identically, so the names live here once rather than
// as string literals duplicated in each main package.
const (
	EnvNodeAddress       = "QUEEL_NODE_ADDRESS"
	EnvSeedNode          = "QUEEL_SEED_NODE"
	EnvReplicationFactor = "QUEEL_REPLICATION_FACTOR"
	EnvGossipInterval    = "QUEEL_GOSSIP_INTERVAL"

	// EnvAntiEntropyInterval is read by AntiEntropyIntervalFromEnv, not by
	// ConfigFromEnv/Join — starting the background anti-entropy job itself
	// (see queel/cluster.RunAntiEntropy) needs a *queel.Engine, which this
	// package deliberately has no notion of (same reasoning as not mounting
	// HTTP routes itself — see the package doc). Callers read this value
	// with AntiEntropyIntervalFromEnv and start that job on their own.
	EnvAntiEntropyInterval = "QUEEL_ANTI_ENTROPY_INTERVAL"
)

// DefaultReplicationFactor and DefaultGossipInterval apply whenever a
// Config leaves the corresponding field at its zero value.
const (
	DefaultReplicationFactor = 3
	DefaultGossipInterval    = 2 * time.Second
)

// Config holds what a node needs to join (or found) a queel cluster. It has
// no notion of environment variables or flags itself — callers translate
// whatever configuration surface they expose into a Config.
type Config struct {
	// Self is this node's own reachable address for peer-to-peer traffic —
	// what every other node's PeerClient will use to reach it. Required.
	Self cluster.Node

	// Seed is an existing cluster member's address to join through. Leave
	// empty to found a brand new cluster containing only Self.
	Seed cluster.Node

	// ReplicationFactor is how many distinct nodes each key is replicated
	// to. Zero means DefaultReplicationFactor.
	ReplicationFactor int

	// GossipInterval is how often membership gossips with a random peer and
	// the Coordinator's replica set is refreshed from the result. Zero
	// means DefaultGossipInterval.
	GossipInterval time.Duration
}

// Join starts gossip-based membership for cfg.Self — optionally announcing
// itself to an existing cluster through cfg.Seed first — and returns a
// Coordinator kept continuously in sync with membership as it changes,
// alongside the Membership itself so the caller can expose it to peers
// (typically via server.GossipHandler) however it sees fit.
//
// ctx bounds only the initial join handshake against cfg.Seed, not the
// ongoing background gossip/refresh loops Join starts — those run for the
// life of the process, same as Membership.Start already documents.
func Join(ctx context.Context, cfg Config) (*cluster.Coordinator, *cluster.Membership, error) {
	if cfg.Self == "" {
		return nil, nil, fmt.Errorf("bootstrap: Config.Self is required")
	}

	rf := cfg.ReplicationFactor
	if rf == 0 {
		rf = DefaultReplicationFactor
	}
	interval := cfg.GossipInterval
	if interval == 0 {
		interval = DefaultGossipInterval
	}

	membership := cluster.NewMembership(cfg.Self)

	if cfg.Seed != "" {
		joinCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := membership.Join(joinCtx, cfg.Seed); err != nil {
			return nil, nil, fmt.Errorf("joining cluster via seed %s: %w", cfg.Seed, err)
		}
	}

	coordinator := cluster.NewCoordinator(cluster.NewRing([]cluster.Node{cfg.Self}, 1), map[cluster.Node]*cluster.PeerClient{})
	coordinator.SetMembers(membership.AliveNodes(), rf)

	backgroundCtx := context.Background()
	membership.Start(backgroundCtx, interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			coordinator.SetMembers(membership.AliveNodes(), rf)
		}
	}()

	return coordinator, membership, nil
}

// ConfigFromEnv reads EnvNodeAddress/EnvSeedNode/EnvReplicationFactor/
// EnvGossipInterval into a Config. enabled reports whether EnvNodeAddress
// was set at all: clustering is opt-in per the doc comments on those env
// vars in queeld and api, so a caller should fall back to running
// unclustered when enabled is false rather than treat that as an error —
// only a malformed value for one of the other three is an error.
func ConfigFromEnv() (cfg Config, enabled bool, err error) {
	selfAddr := os.Getenv(EnvNodeAddress)
	if selfAddr == "" {
		return Config{}, false, nil
	}
	cfg.Self = cluster.Node(selfAddr)
	cfg.Seed = cluster.Node(os.Getenv(EnvSeedNode))

	if v := os.Getenv(EnvReplicationFactor); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, false, fmt.Errorf("invalid %s: %w", EnvReplicationFactor, err)
		}
		cfg.ReplicationFactor = parsed
	}
	if v := os.Getenv(EnvGossipInterval); v != "" {
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, false, fmt.Errorf("invalid %s: %w", EnvGossipInterval, err)
		}
		cfg.GossipInterval = parsed
	}
	return cfg, true, nil
}

// JoinFromEnv composes ConfigFromEnv and Join: the entire cluster-mode
// opt-in a main package needs, in one call. enabled is false — with a nil
// coordinator and membership, and a nil error — whenever EnvNodeAddress
// isn't set; the caller should keep using its local, unclustered Store in
// that case. Only a genuine failure (a malformed env var, or Join itself
// failing to reach cfg.Seed) is returned as err.
func JoinFromEnv(ctx context.Context) (coordinator *cluster.Coordinator, membership *cluster.Membership, enabled bool, err error) {
	cfg, enabled, err := ConfigFromEnv()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	coordinator, membership, err = Join(ctx, cfg)
	return coordinator, membership, enabled, err
}

// AntiEntropyIntervalFromEnv reads EnvAntiEntropyInterval, defaulting to
// cluster.DefaultAntiEntropyInterval if unset. Only meaningful once a
// caller has already started cluster.RunAntiEntropy — see this constant's
// own doc comment for why that's the caller's job, not this package's.
func AntiEntropyIntervalFromEnv() (time.Duration, error) {
	v := os.Getenv(EnvAntiEntropyInterval)
	if v == "" {
		return cluster.DefaultAntiEntropyInterval, nil
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", EnvAntiEntropyInterval, err)
	}
	return parsed, nil
}
