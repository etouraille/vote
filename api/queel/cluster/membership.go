package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"
)

// MemberStatus is what a Membership believes about one node.
type MemberStatus int

const (
	StatusAlive MemberStatus = iota
	StatusDead
)

func (s MemberStatus) String() string {
	if s == StatusAlive {
		return "alive"
	}
	return "dead"
}

// Member is one entry in a Membership's view of the cluster. Incarnation
// only ever goes up for a given node: a node seeds its own incarnation from
// the current time at startup, so a restarted node's "I'm alive" always
// outranks whatever "dead" verdict its peers held about it before.
type Member struct {
	Node        Node         `json:"node"`
	Status      MemberStatus `json:"status"`
	Incarnation int64        `json:"incarnation"`
}

// Membership tracks the live set of cluster nodes via periodic gossip: each
// round, a node picks a random known peer and exchanges its membership view
// with it (push-pull, one request/response), merging in whichever side has
// newer information. New nodes join by exchanging views with a single seed
// instead of needing a complete, static node list up front.
//
// Failure detection here is intentionally simple: a node that fails to
// answer gossip is marked dead directly by whoever noticed, rather than
// first asking other nodes to double-check via indirect probes the way SWIM
// does — a deployment on an unreliable network would want that extra step
// so one node's flaky link to a single peer doesn't mark a healthy node dead
// cluster-wide.
type Membership struct {
	mu      sync.RWMutex
	self    Node
	members map[Node]Member
	http    *http.Client
}

// NewMembership creates a membership view containing only self, alive, with
// an incarnation seeded from the current time.
func NewMembership(self Node) *Membership {
	return &Membership{
		self: self,
		members: map[Node]Member{
			self: {Node: self, Status: StatusAlive, Incarnation: time.Now().UnixNano()},
		},
		http: http.DefaultClient,
	}
}

// Join contacts seed to learn the rest of the cluster and announce itself.
func (m *Membership) Join(ctx context.Context, seed Node) error {
	return m.gossipWith(ctx, seed)
}

// Gossip performs one round: pick a random known-alive peer other than self
// and exchange membership views with it. A no-op if no peer is known yet.
func (m *Membership) Gossip(ctx context.Context) error {
	peer := m.randomPeer()
	if peer == "" {
		return nil
	}
	return m.gossipWith(ctx, peer)
}

func (m *Membership) gossipWith(ctx context.Context, peer Node) error {
	view := m.snapshot()

	body, err := json.Marshal(view)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, string(peer)+"/internal/gossip", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(req)
	if err != nil {
		m.markDead(peer)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		m.markDead(peer)
		return fmt.Errorf("gossip with %s failed with status %d", peer, resp.StatusCode)
	}

	var theirView []Member
	if err := json.NewDecoder(resp.Body).Decode(&theirView); err != nil {
		return err
	}
	m.merge(theirView)
	return nil
}

// HandleGossip merges an incoming view from a peer and returns this node's
// own (now-merged) view, so one request/response round trip exchanges
// knowledge both ways.
func (m *Membership) HandleGossip(theirView []Member) []Member {
	m.merge(theirView)
	return m.snapshot()
}

func (m *Membership) snapshot() []Member {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view := make([]Member, 0, len(m.members))
	for _, mem := range m.members {
		view = append(view, mem)
	}
	return view
}

func (m *Membership) merge(view []Member) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, incoming := range view {
		current, known := m.members[incoming.Node]
		if !known || isNewer(incoming, current) {
			m.members[incoming.Node] = incoming
		}
	}
}

// isNewer reports whether candidate should replace current: a strictly
// higher incarnation always wins; at an equal incarnation, Dead beats Alive
// so a failure verdict can't be quietly overwritten by stale good news.
func isNewer(candidate, current Member) bool {
	if candidate.Incarnation != current.Incarnation {
		return candidate.Incarnation > current.Incarnation
	}
	return candidate.Status == StatusDead && current.Status == StatusAlive
}

func (m *Membership) markDead(node Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, known := m.members[node]
	incarnation := current.Incarnation + 1
	if !known {
		incarnation = time.Now().UnixNano()
	}
	m.members[node] = Member{Node: node, Status: StatusDead, Incarnation: incarnation}
}

func (m *Membership) randomPeer() Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var candidates []Node
	for node, mem := range m.members {
		if node != m.self && mem.Status == StatusAlive {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[rand.Intn(len(candidates))]
}

// AliveNodes returns every node currently believed alive, including self, in
// a stable sorted order.
func (m *Membership) AliveNodes() []Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes := make([]Node, 0, len(m.members))
	for _, mem := range m.members {
		if mem.Status == StatusAlive {
			nodes = append(nodes, mem.Node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	return nodes
}

// Start begins periodic gossiping in the background until ctx is canceled.
func (m *Membership) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.Gossip(ctx)
			}
		}
	}()
}
