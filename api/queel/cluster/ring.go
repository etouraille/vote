// Package cluster implements the partitioning layer for running queel across
// several nodes: a consistent-hash ring decides, for any key (typically a
// text ID), which nodes are responsible for storing it, and how many of them
// must agree for a quorum read or write.
package cluster

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sort"
)

// Node identifies one member of the cluster — its address for inter-node
// communication (e.g. "http://10.0.0.2:9090").
type Node string

// virtualNodesPerNode is how many points each physical node gets on the
// ring. More points spread keys out more evenly across nodes and shrink how
// many keys move when the node set changes, at the cost of a bigger ring to
// search.
const virtualNodesPerNode = 100

type point struct {
	hash uint64
	node Node
}

// Ring is a consistent-hash ring over a fixed set of nodes.
type Ring struct {
	replicationFactor int
	points            []point // sorted by hash, ascending
}

// NewRing builds a ring over nodes with the given replication factor — the
// number of distinct physical nodes each key is replicated to. If
// replicationFactor exceeds the number of distinct nodes, it is clamped down
// to it.
func NewRing(nodes []Node, replicationFactor int) *Ring {
	distinct := map[Node]bool{}
	for _, n := range nodes {
		distinct[n] = true
	}
	if replicationFactor > len(distinct) {
		replicationFactor = len(distinct)
	}

	ring := &Ring{replicationFactor: replicationFactor}
	for node := range distinct {
		for v := 0; v < virtualNodesPerNode; v++ {
			ring.points = append(ring.points, point{
				hash: hashString(fmt.Sprintf("%s#%d", node, v)),
				node: node,
			})
		}
	}
	sort.Slice(ring.points, func(i, j int) bool { return ring.points[i].hash < ring.points[j].hash })

	return ring
}

// ReplicasFor returns the replicationFactor distinct physical nodes
// responsible for key, walking the ring clockwise from key's position. The
// first entry is the key's primary (coordinator) node.
func (r *Ring) ReplicasFor(key string) []Node {
	if len(r.points) == 0 || r.replicationFactor == 0 {
		return nil
	}

	h := hashString(key)
	start := sort.Search(len(r.points), func(i int) bool { return r.points[i].hash >= h })

	seen := make(map[Node]bool, r.replicationFactor)
	replicas := make([]Node, 0, r.replicationFactor)
	for i := 0; len(replicas) < r.replicationFactor && i < len(r.points); i++ {
		p := r.points[(start+i)%len(r.points)]
		if seen[p.node] {
			continue
		}
		seen[p.node] = true
		replicas = append(replicas, p.node)
	}
	return replicas
}

func hashString(s string) uint64 {
	sum := sha1.Sum([]byte(s))
	return binary.BigEndian.Uint64(sum[:8])
}

// Quorum returns the minimum number of replicas that must respond for a read
// or write to count as successful under quorum consistency: a majority of
// replicationFactor.
func Quorum(replicationFactor int) int {
	return replicationFactor/2 + 1
}
