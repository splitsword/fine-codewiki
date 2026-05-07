package embedder

import (
	"context"
	"errors"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockEmbedder struct {
	vectors [][]float32
	err     error
	calls   int
}

func (m *mockEmbedder) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		if i < len(m.vectors) {
			out[i] = m.vectors[i]
		} else {
			out[i] = []float32{float32(i)}
		}
	}
	return out, nil
}

func TestEmbedChunks(t *testing.T) {
	store := vectorstore.New()
	mock := &mockEmbedder{
		vectors: [][]float32{
			{1, 0, 0},
			{0, 1, 0},
		},
	}

	emb := New(mock, store)
	chunks := []*chunker.Chunk{
		{ID: "chunk-1", Content: "hello world"},
		{ID: "chunk-2", Content: "foo bar"},
	}

	err := emb.EmbedChunks(context.Background(), chunks)
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls)
	assert.Equal(t, 2, store.Count())

	rec1, ok := store.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, []float32{1, 0, 0}, rec1.Vector)

	rec2, ok := store.Get("chunk-2")
	require.True(t, ok)
	assert.Equal(t, []float32{0, 1, 0}, rec2.Vector)
}

func TestEmbedChunksSkipsExisting(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{9, 9, 9}, &chunker.Chunk{ID: "chunk-1", Content: "existing"})

	mock := &mockEmbedder{
		vectors: [][]float32{{0, 1, 0}},
	}

	emb := New(mock, store)
	chunks := []*chunker.Chunk{
		{ID: "chunk-1", Content: "hello world"},
		{ID: "chunk-2", Content: "foo bar"},
	}

	err := emb.EmbedChunks(context.Background(), chunks)
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls)
	assert.Equal(t, 2, store.Count())

	// chunk-1 should keep original vector (was skipped)
	rec1, _ := store.Get("chunk-1")
	assert.Equal(t, []float32{9, 9, 9}, rec1.Vector)

	// chunk-2 should have new vector
	rec2, _ := store.Get("chunk-2")
	assert.Equal(t, []float32{0, 1, 0}, rec2.Vector)
}

func TestEmbedChunksNoProvider(t *testing.T) {
	store := vectorstore.New()
	emb := New(nil, store)
	chunks := []*chunker.Chunk{{ID: "a", Content: "a"}}

	err := emb.EmbedChunks(context.Background(), chunks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no embedding provider")
}

func TestEmbedChunksProviderError(t *testing.T) {
	store := vectorstore.New()
	mock := &mockEmbedder{err: errors.New("rate limit")}

	emb := New(mock, store)
	chunks := []*chunker.Chunk{{ID: "a", Content: "a"}}

	err := emb.EmbedChunks(context.Background(), chunks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
	assert.Equal(t, 0, store.Count())
}

func TestEmbedChunksBatching(t *testing.T) {
	store := vectorstore.New()
	mock := &mockEmbedder{
		vectors: [][]float32{
			{1}, {2}, {3},
		},
	}

	emb := New(mock, store)
	emb.SetBatchSize(2)

	chunks := []*chunker.Chunk{
		{ID: "c1", Content: "a"},
		{ID: "c2", Content: "b"},
		{ID: "c3", Content: "c"},
	}

	err := emb.EmbedChunks(context.Background(), chunks)
	require.NoError(t, err)
	assert.Equal(t, 2, mock.calls) // batch 1: c1,c2 ; batch 2: c3
	assert.Equal(t, 3, store.Count())
}

type mockEmbedderMismatch struct {
	mockEmbedder
}

func (m *mockEmbedderMismatch) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	m.calls++
	return [][]float32{{1}}, nil // always return 1 vector regardless of input
}

func TestEmbedChunksMismatchedVectorCount(t *testing.T) {
	store := vectorstore.New()
	mock := &mockEmbedderMismatch{}

	emb := New(mock, store)
	chunks := []*chunker.Chunk{
		{ID: "c1", Content: "a"},
		{ID: "c2", Content: "b"},
	}

	err := emb.EmbedChunks(context.Background(), chunks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected 2 vectors, got 1")
}

func TestEmbedChunksEmpty(t *testing.T) {
	store := vectorstore.New()
	mock := &mockEmbedder{}

	emb := New(mock, store)
	err := emb.EmbedChunks(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, mock.calls)
}
