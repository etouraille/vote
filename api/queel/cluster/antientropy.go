package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"log"
	"math/rand"
	"sort"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/merkle"
)

// DefaultAntiEntropyBuckets is how many leaves the Merkle tree partitions a
// node's keyspace into when no explicit count is given — how finely a
// divergence can be localized before falling back to comparing every key
// in the affected bucket. Must be a power of two (see merkle.Build). 4096
// keeps a full tree transfer small (4096 * 32 bytes = 128 KiB) while still
// giving each bucket a small enough share of a realistic keyspace that a
// divergence rarely means re-syncing more than a handful of keys.
const DefaultAntiEntropyBuckets = 4096

// DefaultAntiEntropyInterval is how often RunAntiEntropy reconciles against
// a random peer when no explicit interval is given.
const DefaultAntiEntropyInterval = 5 * time.Minute

// BucketFor deterministically assigns key to one of numBuckets leaves —
// the same key always lands in the same bucket on every node, which is
// what lets two independently-built trees be compared leaf-for-leaf.
func BucketFor(key string, numBuckets int) int {
	sum := sha256.Sum256([]byte(key))
	h := binary.BigEndian.Uint64(sum[:8])
	return int(h % uint64(numBuckets))
}

// BuildTree scans engine's entire local keyspace — every key it currently
// holds, stored as cluster.Entry-wrapped values (see
// queel/server.NewInternalHandler, which is what puts them there) — and
// summarizes it as a Merkle tree with numBuckets leaves, one per key-hash
// bucket. It also returns each bucket's actual contents, so a caller that
// just built its own tree doesn't need a second scan to reconcile whatever
// buckets a Diff against a peer's tree flags as divergent.
func BuildTree(engine *queel.Engine, numBuckets int) (*merkle.Tree, map[int][]KeyEntry, error) {
	kvs, err := engine.Scan([]byte(""))
	if err != nil {
		return nil, nil, err
	}

	buckets := make(map[int][]KeyEntry, numBuckets)
	for _, kv := range kvs {
		var entry Entry
		if err := json.Unmarshal(kv.Value, &entry); err != nil {
			return nil, nil, err
		}
		b := BucketFor(string(kv.Key), numBuckets)
		buckets[b] = append(buckets[b], KeyEntry{Key: string(kv.Key), Entry: entry})
	}

	leaves := make([]merkle.Hash, numBuckets)
	for b := 0; b < numBuckets; b++ {
		leaves[b] = leafHash(buckets[b])
	}

	tree, err := merkle.Build(leaves)
	if err != nil {
		return nil, nil, err
	}
	return tree, buckets, nil
}

// leafHash canonically hashes one bucket's contents so the result depends
// only on what's in the bucket, never on scan order: entries are sorted by
// key first, then each contributes its key, timestamp, tombstone flag, and
// value to the hash in turn. An empty bucket (no keys hashed into it) has
// a fixed, well-defined hash too — merkle.HashBytes(nil) — so two nodes
// that both have nothing there still agree.
func leafHash(entries []KeyEntry) merkle.Hash {
	if len(entries) == 0 {
		return merkle.HashBytes(nil)
	}

	sorted := make([]KeyEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	h := sha256.New()
	var tsBuf [8]byte
	for _, e := range sorted {
		h.Write([]byte(e.Key))
		binary.BigEndian.PutUint64(tsBuf[:], uint64(e.Entry.Timestamp))
		h.Write(tsBuf[:])
		if e.Entry.Tombstone {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		h.Write(e.Entry.Value)
	}
	var out merkle.Hash
	copy(out[:], h.Sum(nil))
	return out
}

// Reconcile compares engine's local keyspace against one peer's, using a
// Merkle tree to find which buckets actually diverge instead of comparing
// every key, then repairs every key that differs within those buckets:
// whichever side holds the newer entry (by Entry.Timestamp — the same
// last-write-wins rule Coordinator.readRepair already applies at the
// single-key level) is pushed to the other side. It returns how many keys
// were repaired.
//
// This only converges keys peer and engine both currently hold, or that
// only one side has — a key one side already tombstoned (deleted) but the
// other never received the delete for won't be caught here, since a plain
// Engine.Scan never surfaces tombstoned entries in the first place (see
// Engine.Scan). That gap already exists for read-repair too; closing it
// would need a way to enumerate tombstones, which is a bigger change than
// this pass makes.
func Reconcile(ctx context.Context, engine *queel.Engine, peer *PeerClient, numBuckets int) (int, error) {
	localTree, localBuckets, err := BuildTree(engine, numBuckets)
	if err != nil {
		return 0, err
	}

	peerTree, err := peer.MerkleTree(ctx, numBuckets)
	if err != nil {
		return 0, err
	}

	if localTree.Root() == peerTree.Root() {
		return 0, nil
	}

	divergent, err := merkle.Diff(localTree, peerTree)
	if err != nil {
		return 0, err
	}

	repaired := 0
	for _, bucket := range divergent {
		peerEntries, err := peer.MerkleBucket(ctx, numBuckets, bucket)
		if err != nil {
			return repaired, err
		}

		peerByKey := make(map[string]Entry, len(peerEntries))
		for _, e := range peerEntries {
			peerByKey[e.Key] = e.Entry
		}
		localByKey := make(map[string]Entry, len(localBuckets[bucket]))
		for _, e := range localBuckets[bucket] {
			localByKey[e.Key] = e.Entry
		}

		allKeys := make(map[string]bool, len(peerByKey)+len(localByKey))
		for k := range peerByKey {
			allKeys[k] = true
		}
		for k := range localByKey {
			allKeys[k] = true
		}

		for key := range allKeys {
			local, hasLocal := localByKey[key]
			remote, hasRemote := peerByKey[key]

			switch {
			case hasLocal && !hasRemote:
				if err := peer.Put(ctx, key, local); err != nil {
					log.Printf("anti-entropy: pushing %s to %s: %v", key, peer.baseURL, err)
					continue
				}
				repaired++
			case hasRemote && !hasLocal:
				if err := putLocal(engine, key, remote); err != nil {
					log.Printf("anti-entropy: pulling %s from %s: %v", key, peer.baseURL, err)
					continue
				}
				repaired++
			case hasLocal && hasRemote && local.Timestamp < remote.Timestamp:
				if err := putLocal(engine, key, remote); err != nil {
					log.Printf("anti-entropy: pulling newer %s from %s: %v", key, peer.baseURL, err)
					continue
				}
				repaired++
			case hasLocal && hasRemote && remote.Timestamp < local.Timestamp:
				if err := peer.Put(ctx, key, local); err != nil {
					log.Printf("anti-entropy: pushing newer %s to %s: %v", key, peer.baseURL, err)
					continue
				}
				repaired++
			}
		}
	}
	return repaired, nil
}

func putLocal(engine *queel.Engine, key string, entry Entry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return engine.Put([]byte(key), payload)
}

// RunAntiEntropy periodically reconciles engine against a random other
// alive member of membership, using Reconcile — the background job that
// catches whatever read-repair alone misses: a key nobody happens to read
// again after this node lost it (a disk failure, a missed write during a
// network partition) stays silently under-replicated forever otherwise,
// since read-repair only ever fires for keys someone actually requests.
// Runs until ctx is canceled.
func RunAntiEntropy(ctx context.Context, engine *queel.Engine, membership *Membership, self Node, interval time.Duration, numBuckets int) {
	if interval <= 0 {
		interval = DefaultAntiEntropyInterval
	}
	if numBuckets <= 0 {
		numBuckets = DefaultAntiEntropyBuckets
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			target := randomPeerExcluding(membership.AliveNodes(), self)
			if target == "" {
				continue // alone in the cluster right now — nothing to reconcile against
			}

			peer := NewPeerClient(string(target))
			repaired, err := Reconcile(ctx, engine, peer, numBuckets)
			if err != nil {
				log.Printf("anti-entropy against %s failed: %v", target, err)
				continue
			}
			if repaired > 0 {
				log.Printf("anti-entropy against %s repaired %d key(s)", target, repaired)
			}
		}
	}
}

func randomPeerExcluding(nodes []Node, self Node) Node {
	candidates := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if n != self {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[rand.Intn(len(candidates))]
}
