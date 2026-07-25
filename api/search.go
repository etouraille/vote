package main

import (
	"context"
	"fmt"

	"github.com/etouraille/queel"
)

// embeddingDimension must match the output size of OLLAMA_EMBED_MODEL —
// 768 for nomic-embed-text, the default. Update this if the model changes.
const embeddingDimension = 768

// searchIndexer ties the embedder and the vector store together: index a
// text — at creation, and again whenever a round closes on it — and search
// the corpus by embedding the query the same way.
type searchIndexer struct {
	embedder embedder
	qdrant   *qdrantClient

	// pruneSuperseded controls whether IndexFinalizedText also removes a
	// text's previous version from the index once the new one is indexed —
	// see SEARCH_PRUNE_SUPERSEDED in .env.example. Since queel.CloseRound
	// forks a new Text rather than mutating the old one in place (see
	// queel.Text.PreviousTextID), every finalized version gets its own
	// point unless this prunes the ones it superseded.
	pruneSuperseded bool
}

func newSearchIndexer(e embedder, q *qdrantClient, pruneSuperseded bool) *searchIndexer {
	return &searchIndexer{embedder: e, qdrant: q, pruneSuperseded: pruneSuperseded}
}

// IndexText embeds a text's content and upserts it into Qdrant, keyed
// deterministically by text ID so re-indexing the same text (a later round
// closing again) overwrites rather than duplicates.
func (s *searchIndexer) IndexText(ctx context.Context, text *queel.Text) error {
	vector, err := s.embedder.Embed(ctx, text.Content)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	id, err := textPointID(text.ID)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"text_id": text.ID,
		"title":   text.Title,
	}
	if err := s.qdrant.Upsert(ctx, id, vector, payload); err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

// RemoveText deletes a text's point from the search index, if it has one.
func (s *searchIndexer) RemoveText(ctx context.Context, textID string) error {
	id, err := textPointID(textID)
	if err != nil {
		return err
	}
	if err := s.qdrant.Delete(ctx, id); err != nil {
		return fmt.Errorf("qdrant delete: %w", err)
	}
	return nil
}

// IndexFinalizedText indexes text and, if pruneSuperseded is enabled,
// removes the version it superseded (text.PreviousTextID) from the index
// right after — so search only ever surfaces the latest finalized version
// of a text's lineage instead of accumulating one entry per closed round.
func (s *searchIndexer) IndexFinalizedText(ctx context.Context, text *queel.Text) error {
	if err := s.IndexText(ctx, text); err != nil {
		return err
	}
	if s.pruneSuperseded && text.PreviousTextID != "" {
		if err := s.RemoveText(ctx, text.PreviousTextID); err != nil {
			return fmt.Errorf("prune superseded text %s: %w", text.PreviousTextID, err)
		}
	}
	return nil
}

type SearchResult struct {
	TextID string  `json:"textId"`
	Title  string  `json:"title"`
	Score  float64 `json:"score"`
}

// Search embeds the query and returns the closest texts by cosine
// similarity — the RAG corpus is exactly the set of texts IndexText has been
// called on: every text ever created, reindexed under a new ID each time a
// round closes on it (see queel.Text.PreviousTextID).
func (s *searchIndexer) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	vector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	hits, err := s.qdrant.Search(ctx, vector, limit)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	results := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		textID, _ := hit.Payload["text_id"].(string)
		title, _ := hit.Payload["title"].(string)
		results = append(results, SearchResult{TextID: textID, Title: title, Score: hit.Score})
	}
	return results, nil
}
