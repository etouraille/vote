package server

import (
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
