package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// qdrantClient is a minimal REST client for the one Qdrant collection this
// service needs — just enough to create it, upsert a text's embedding, and
// search by vector. No client library dependency, consistent with the rest
// of this codebase (net/http + encoding/json throughout).
type qdrantClient struct {
	baseURL    string
	collection string
	http       *http.Client
}

func newQdrantClient(baseURL, collection string) *qdrantClient {
	return &qdrantClient{baseURL: baseURL, collection: collection, http: http.DefaultClient}
}

func (q *qdrantClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// EnsureCollection creates the collection if it doesn't already exist. Safe
// to call on every startup.
func (q *qdrantClient) EnsureCollection(ctx context.Context, vectorSize int) error {
	var exists struct {
		Result struct {
			Exists bool `json:"exists"`
		} `json:"result"`
	}
	if err := q.do(ctx, http.MethodGet, "/collections/"+q.collection+"/exists", nil, &exists); err != nil {
		return err
	}
	if exists.Result.Exists {
		return nil
	}

	body := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	return q.do(ctx, http.MethodPut, "/collections/"+q.collection, body, nil)
}

// Upsert indexes (or re-indexes) one point. id must be a valid Qdrant point
// ID — an unsigned integer or UUID string; see textPointID.
func (q *qdrantClient) Upsert(ctx context.Context, id uint64, vector []float32, payload map[string]any) error {
	body := map[string]any{
		"points": []map[string]any{
			{"id": id, "vector": vector, "payload": payload},
		},
	}
	return q.do(ctx, http.MethodPut, "/collections/"+q.collection+"/points", body, nil)
}

// Delete removes one point by ID. Deleting an ID that was never indexed (or
// already removed) is a no-op, not an error — Qdrant's points/delete is
// idempotent.
func (q *qdrantClient) Delete(ctx context.Context, id uint64) error {
	body := map[string]any{"points": []uint64{id}}
	return q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete", body, nil)
}

type QdrantSearchResult struct {
	ID      uint64         `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func (q *qdrantClient) Search(ctx context.Context, vector []float32, limit int) ([]QdrantSearchResult, error) {
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
	}
	var resp struct {
		Result []QdrantSearchResult `json:"result"`
	}
	if err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/search", body, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// textPointID deterministically derives a Qdrant point ID from a text ID, so
// re-indexing the same text overwrites its existing point instead of
// creating a duplicate. queel text IDs are exactly 16 hex characters (8
// random bytes from newID()), which is exactly 64 bits — parses directly as
// a uint64, one of the two ID shapes Qdrant accepts (the other being UUID).
func textPointID(textID string) (uint64, error) {
	id, err := strconv.ParseUint(textID, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("text id %q is not a valid point id: %w", textID, err)
	}
	return id, nil
}
