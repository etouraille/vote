package server

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

// NewInternalHandler exposes a node's local engine for replication by its
// peers: raw put/get on cluster.Entry-wrapped values, keyed by arbitrary
// string keys. It is meant to be reached only by other cluster nodes acting
// as coordinators (queel/cluster.Coordinator) — never by end-user clients,
// which should only ever talk to NewHandler's public API. In production,
// mount this on a different address/port than the public one, e.g. only
// reachable on a private cluster network.
func NewInternalHandler(engine *queel.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/put", internalPutHandler(engine))
	mux.HandleFunc("POST /internal/put-batch", internalPutBatchHandler(engine))
	mux.HandleFunc("POST /internal/get", internalGetHandler(engine))
	mux.HandleFunc("POST /internal/scan", internalScanHandler(engine))
	mux.HandleFunc("POST /internal/merkle-tree", internalMerkleTreeHandler(engine))
	mux.HandleFunc("POST /internal/merkle-bucket", internalMerkleBucketHandler(engine))
	return mux
}

type internalPutRequest struct {
	Key   string        `json:"key"`
	Entry cluster.Entry `json:"entry"`
}

func internalPutHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req internalPutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := json.Marshal(req.Entry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := engine.Put([]byte(req.Key), payload); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type internalPutBatchRequest struct {
	Items []cluster.KeyEntry `json:"items"`
}

func internalPutBatchHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req internalPutBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		for _, item := range req.Items {
			payload, err := json.Marshal(item.Entry)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := engine.Put([]byte(item.Key), payload); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type internalGetRequest struct {
	Key string `json:"key"`
}

func internalGetHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req internalGetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		value, found, err := engine.Get([]byte(req.Key))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "key not found")
			return
		}
		var entry cluster.Entry
		if err := json.Unmarshal(value, &entry); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

type internalScanRequest struct {
	Prefix string `json:"prefix"`
}

func internalScanHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req internalScanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		kvs, err := engine.Scan([]byte(req.Prefix))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		results := make([]cluster.KeyEntry, 0, len(kvs))
		for _, kv := range kvs {
			var entry cluster.Entry
			if err := json.Unmarshal(kv.Value, &entry); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			results = append(results, cluster.KeyEntry{Key: string(kv.Key), Entry: entry})
		}
		writeJSON(w, http.StatusOK, results)
	}
}

type merkleTreeRequest struct {
	NumBuckets int `json:"numBuckets"`
}

type merkleTreeResponse struct {
	Leaves []string `json:"leaves"` // hex-encoded merkle.Hash, one per bucket
}

// internalMerkleTreeHandler answers a peer's cluster.PeerClient.MerkleTree
// call: a fresh Merkle-tree summary of this node's entire local keyspace
// (see cluster.BuildTree), computed from a full scan on every request
// rather than cached — simple and correct, and cheap enough at the scale
// queel's anti-entropy job targets (see cluster.RunAntiEntropy).
func internalMerkleTreeHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req merkleTreeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		tree, _, err := cluster.BuildTree(engine, req.NumBuckets)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		leaves := tree.Leaves()
		hexLeaves := make([]string, len(leaves))
		for i, l := range leaves {
			hexLeaves[i] = hex.EncodeToString(l[:])
		}
		writeJSON(w, http.StatusOK, merkleTreeResponse{Leaves: hexLeaves})
	}
}

type merkleBucketRequest struct {
	NumBuckets int `json:"numBuckets"`
	Bucket     int `json:"bucket"`
}

// internalMerkleBucketHandler answers a peer's
// cluster.PeerClient.MerkleBucket call: every key/entry this node has whose
// key hashes into the requested bucket under the same numBuckets
// partitioning MerkleTree used — the reconciliation step
// cluster.Reconcile takes once it knows (via Diff against this node's
// tree) that a bucket diverges.
func internalMerkleBucketHandler(engine *queel.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req merkleBucketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		_, buckets, err := cluster.BuildTree(engine, req.NumBuckets)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		entries := buckets[req.Bucket]
		if entries == nil {
			entries = []cluster.KeyEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}
