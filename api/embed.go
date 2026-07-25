package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// embedder turns text into a vector for similarity search. The question is
// embedded the same way as the finalized texts it's compared against, so
// both sides land in the same vector space.
type embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ollamaEmbedder calls a self-hosted Ollama instance — no external API key,
// matching the rest of this stack (queel, Postgres, Qdrant) being self-hosted.
type ollamaEmbedder struct {
	baseURL string
	model   string
	http    *http.Client
}

func newOllamaEmbedder(baseURL, model string) *ollamaEmbedder {
	return &ollamaEmbedder{baseURL: baseURL, model: model, http: http.DefaultClient}
}

type ollamaEmbeddingsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingsResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (o *ollamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbeddingsRequest{Model: o.model, Prompt: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings: unexpected status %d", resp.StatusCode)
	}

	var parsed ollamaEmbeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embeddings: empty embedding returned")
	}
	return parsed.Embedding, nil
}
