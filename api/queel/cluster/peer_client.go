package cluster

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/etouraille/queel/merkle"
)

// PeerClient talks to one node's internal replication endpoints (see
// queel/server.NewInternalHandler). A Coordinator uses one of these per
// replica to fan a key's writes and reads out — end users never see this;
// they go through queel/client against the public API instead.
type PeerClient struct {
	baseURL string
	http    *http.Client
}

func NewPeerClient(baseURL string) *PeerClient {
	return &PeerClient{baseURL: baseURL, http: http.DefaultClient}
}

// Put stores entry for key on this one peer.
func (p *PeerClient) Put(ctx context.Context, key string, entry Entry) error {
	body, err := json.Marshal(struct {
		Key   string `json:"key"`
		Entry Entry  `json:"entry"`
	}{Key: key, Entry: entry})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/put", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("peer %s: put failed with status %d", p.baseURL, resp.StatusCode)
	}
	return nil
}

// PutBatch stores several key/entry pairs on this one peer in a single
// request — used to send everything a Coordinator.WriteBatch call has
// destined for this node in one round trip instead of one per key.
func (p *PeerClient) PutBatch(ctx context.Context, items []KeyEntry) error {
	body, err := json.Marshal(struct {
		Items []KeyEntry `json:"items"`
	}{Items: items})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/put-batch", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("peer %s: put-batch failed with status %d", p.baseURL, resp.StatusCode)
	}
	return nil
}

// Get fetches key from this one peer. found is false only if the peer has
// never stored this key at all — a tombstoned key is still "found" (with
// Entry.Tombstone set), so the coordinator can tell "deleted" apart from
// "never written" when picking the most recent entry across replicas.
func (p *PeerClient) Get(ctx context.Context, key string) (entry Entry, found bool, err error) {
	body, err := json.Marshal(struct {
		Key string `json:"key"`
	}{Key: key})
	if err != nil {
		return Entry{}, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/get", bytes.NewReader(body))
	if err != nil {
		return Entry{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return Entry{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Entry{}, false, nil
	}
	if resp.StatusCode >= 300 {
		return Entry{}, false, fmt.Errorf("peer %s: get failed with status %d", p.baseURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return Entry{}, false, err
	}
	return entry, true, nil
}

// KeyEntry pairs a raw key with the Entry stored for it, as returned by Scan.
type KeyEntry struct {
	Key   string `json:"key"`
	Entry Entry  `json:"entry"`
}

// Scan returns every key/entry this one peer has stored locally whose key
// starts with prefix.
func (p *PeerClient) Scan(ctx context.Context, prefix string) ([]KeyEntry, error) {
	body, err := json.Marshal(struct {
		Prefix string `json:"prefix"`
	}{Prefix: prefix})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/scan", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("peer %s: scan failed with status %d", p.baseURL, resp.StatusCode)
	}
	var entries []KeyEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// MerkleTree fetches this one peer's current Merkle tree summary of its
// local keyspace, partitioned into numBuckets leaves — see
// queel/cluster.BuildTree, which is what computes it server-side. Used by
// Reconcile to find which buckets diverge from the local tree without
// transferring the peer's actual data first.
func (p *PeerClient) MerkleTree(ctx context.Context, numBuckets int) (*merkle.Tree, error) {
	body, err := json.Marshal(struct {
		NumBuckets int `json:"numBuckets"`
	}{NumBuckets: numBuckets})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/merkle-tree", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("peer %s: merkle-tree failed with status %d", p.baseURL, resp.StatusCode)
	}
	var payload struct {
		Leaves []string `json:"leaves"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	leaves := make([]merkle.Hash, len(payload.Leaves))
	for i, hexLeaf := range payload.Leaves {
		decoded, err := hex.DecodeString(hexLeaf)
		if err != nil {
			return nil, fmt.Errorf("peer %s: decoding leaf %d: %w", p.baseURL, i, err)
		}
		if len(decoded) != merkle.HashSize {
			return nil, fmt.Errorf("peer %s: leaf %d has %d bytes, want %d", p.baseURL, i, len(decoded), merkle.HashSize)
		}
		copy(leaves[i][:], decoded)
	}
	return merkle.Build(leaves)
}

// MerkleBucket fetches every key/entry this one peer currently has whose
// key hashes into bucket under the same numBuckets partitioning MerkleTree
// used — the reconciliation step Reconcile takes once a Diff against the
// peer's tree has flagged bucket as divergent.
func (p *PeerClient) MerkleBucket(ctx context.Context, numBuckets, bucket int) ([]KeyEntry, error) {
	body, err := json.Marshal(struct {
		NumBuckets int `json:"numBuckets"`
		Bucket     int `json:"bucket"`
	}{NumBuckets: numBuckets, Bucket: bucket})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/internal/merkle-bucket", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("peer %s: merkle-bucket failed with status %d", p.baseURL, resp.StatusCode)
	}
	var entries []KeyEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}
