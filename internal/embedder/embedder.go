package embedder

import (
	"context"
	"fmt"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
)

// Embedder orchestrates chunk embedding via an LLM provider.
type Embedder struct {
	provider llm.Provider
	store    *vectorstore.VectorStore
	batchSize int
}

// New creates an Embedder.
func New(provider llm.Provider, store *vectorstore.VectorStore) *Embedder {
	return &Embedder{
		provider:  provider,
		store:     store,
		batchSize: 32,
	}
}

// SetBatchSize configures how many texts are sent per Embed call.
func (e *Embedder) SetBatchSize(n int) {
	if n > 0 {
		e.batchSize = n
	}
}

// EmbedChunks generates embeddings for all chunks and stores them.
func (e *Embedder) EmbedChunks(ctx context.Context, chunks []*chunker.Chunk) error {
	if e.provider == nil {
		return fmt.Errorf("no embedding provider configured")
	}

	// Filter chunks that are not yet embedded
	var toEmbed []*chunker.Chunk
	for _, ch := range chunks {
		if _, ok := e.store.Get(ch.ID); !ok {
			toEmbed = append(toEmbed, ch)
		}
	}

	if len(toEmbed) == 0 {
		return nil
	}

	total := len(toEmbed)
	for i := 0; i < total; i += e.batchSize {
		end := i + e.batchSize
		if end > total {
			end = total
		}
		batch := toEmbed[i:end]
		texts := make([]string, len(batch))
		for j, ch := range batch {
			texts[j] = ch.Content
		}

		vectors, err := e.provider.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch %d-%d: %w", i, end-1, err)
		}
		if len(vectors) != len(batch) {
			return fmt.Errorf("embed batch %d-%d: expected %d vectors, got %d", i, end-1, len(batch), len(vectors))
		}

		for j, ch := range batch {
			e.store.Upsert(ch.ID, vectors[j], ch)
		}
	}

	return nil
}

// Count returns the number of records already in the store.
func (e *Embedder) Count() int {
	return e.store.Count()
}
