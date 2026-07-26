// Package merkle implements a fixed-depth binary Merkle tree: a way to
// summarize a large keyspace as a small set of hashes such that comparing
// two copies of that keyspace costs time proportional to how much they
// actually diverge, not to their total size. See queel/cluster's
// anti-entropy background job for what this is for — reconciling two
// cluster nodes' data without either transferring or hashing every key on
// every comparison.
//
// This package knows nothing about queel, cluster nodes, or HTTP — it's
// pure tree math over caller-supplied leaf hashes.
package merkle

import (
	"crypto/sha256"
	"fmt"
)

// HashSize is the width of every hash in the tree, in bytes.
const HashSize = sha256.Size

// Hash is one node's value in the tree — a leaf hash (supplied by the
// caller, see Build) or an internal node's hash (computed from its
// children).
type Hash [HashSize]byte

// HashBytes hashes an arbitrary byte string into a Hash — the primitive a
// caller uses to turn a bucket's contents into a leaf hash before calling
// Build.
func HashBytes(data []byte) Hash {
	return sha256.Sum256(data)
}

func hashPair(a, b Hash) Hash {
	var buf [2 * HashSize]byte
	copy(buf[:HashSize], a[:])
	copy(buf[HashSize:], b[:])
	return sha256.Sum256(buf[:])
}

// Tree is a complete binary tree built bottom-up from a power-of-two number
// of leaves. levels[0] holds the leaves; each subsequent level holds one
// hash per pair of hashes in the level below; the last level holds exactly
// the root.
type Tree struct {
	levels [][]Hash
}

// Build constructs a Tree over leaves, which must be a power-of-two long —
// one hash per bucket the caller partitioned its keyspace into. The tree
// is immutable once built; summarizing a changed keyspace means calling
// Build again over freshly computed leaf hashes.
func Build(leaves []Hash) (*Tree, error) {
	if len(leaves) == 0 || leaves == nil {
		return nil, fmt.Errorf("merkle: at least one leaf is required")
	}
	if len(leaves)&(len(leaves)-1) != 0 {
		return nil, fmt.Errorf("merkle: leaf count must be a power of two, got %d", len(leaves))
	}

	levels := make([][]Hash, 1, 32)
	levels[0] = leaves
	for len(levels[len(levels)-1]) > 1 {
		prev := levels[len(levels)-1]
		next := make([]Hash, len(prev)/2)
		for i := range next {
			next[i] = hashPair(prev[2*i], prev[2*i+1])
		}
		levels = append(levels, next)
	}
	return &Tree{levels: levels}, nil
}

// Root is the single hash summarizing the entire tree — identical trees
// (same leaves, in the same order) always produce the same Root, and any
// change to any leaf changes it.
func (t *Tree) Root() Hash {
	return t.levels[len(t.levels)-1][0]
}

// Leaves returns the tree's leaf hashes, in bucket-index order — what a
// node sends a peer so the peer can reconstruct an equivalent Tree (via
// Build) and Diff against its own, without either side transferring its
// actual data.
func (t *Tree) Leaves() []Hash {
	return t.levels[0]
}

// NumLeaves is how many buckets this tree partitions the keyspace into.
func (t *Tree) NumLeaves() int {
	return len(t.levels[0])
}

// Diff compares a and b, which must have the same number of leaves (i.e.
// both built with the same bucket count), and returns the indices of every
// leaf whose hash differs between them. Comparison starts at the root and
// only descends into subtrees whose hash doesn't already match — a whole
// matching branch is skipped in one comparison instead of checking every
// leaf beneath it, which is the entire point of using a tree instead of a
// flat list of hashes: cost is proportional to how much the two trees
// actually diverge, not to their size.
func Diff(a, b *Tree) ([]int, error) {
	if a.NumLeaves() != b.NumLeaves() {
		return nil, fmt.Errorf("merkle: cannot diff trees with different leaf counts (%d vs %d)", a.NumLeaves(), b.NumLeaves())
	}
	if a.Root() == b.Root() {
		return nil, nil
	}

	var out []int
	diffNode(a, b, len(a.levels)-1, 0, &out)
	return out, nil
}

func diffNode(a, b *Tree, level, index int, out *[]int) {
	if a.levels[level][index] == b.levels[level][index] {
		return
	}
	if level == 0 {
		*out = append(*out, index)
		return
	}
	diffNode(a, b, level-1, 2*index, out)
	diffNode(a, b, level-1, 2*index+1, out)
}
