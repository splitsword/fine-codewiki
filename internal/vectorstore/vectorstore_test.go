package vectorstore

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndGet(t *testing.T) {
	vs := New()
	vs.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{ID: "chunk-1", Content: "hello"})

	assert.Equal(t, 1, vs.Count())

	rec, ok := vs.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, "chunk-1", rec.ID)
	assert.Equal(t, []float32{1, 0, 0}, rec.Vector)
	assert.Equal(t, "hello", rec.Chunk.Content)
}

func TestSQLiteUpsertAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs.Close()

	vs.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{ID: "chunk-1", Content: "hello"})
	assert.Equal(t, 1, vs.Count())

	rec, ok := vs.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, "chunk-1", rec.ID)
	assert.Equal(t, []float32{1, 0, 0}, rec.Vector)
	assert.Equal(t, "hello", rec.Chunk.Content)
}

func TestSQLiteDelete(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs.Close()

	vs.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{Content: "hello"})
	assert.True(t, vs.Delete("chunk-1"))
	assert.Equal(t, 0, vs.Count())
	assert.False(t, vs.Delete("chunk-1"))
}

func TestSQLiteSearchTopK(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs.Close()

	vs.Upsert("x", []float32{1, 0, 0}, &chunker.Chunk{ID: "x", Content: "x-axis"})
	vs.Upsert("y", []float32{0, 1, 0}, &chunker.Chunk{ID: "y", Content: "y-axis"})
	vs.Upsert("z", []float32{0, 0, 1}, &chunker.Chunk{ID: "z", Content: "z-axis"})

	results := vs.Search([]float32{1, 0.1, 0}, 2, 0.0)
	require.Len(t, results, 2)
	assert.Equal(t, "x", results[0].Record.ID)
	assert.InDelta(t, 1.0, results[0].Similarity, 0.01)

	results = vs.Search([]float32{0.1, 1, 0}, 1, 0.0)
	require.Len(t, results, 1)
	assert.Equal(t, "y", results[0].Record.ID)
}

func TestSQLitePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs1, err := NewSQLite(path)
	require.NoError(t, err)
	vs1.Upsert("chunk-1", []float32{0.1, 0.2, 0.3}, &chunker.Chunk{ID: "chunk-1", Type: chunker.TypeFunction, Content: "func add(a, b)", Filename: "math.py", Name: "add"})
	require.NoError(t, vs1.Close())

	vs2, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs2.Close()

	assert.Equal(t, 1, vs2.Count())
	rec, ok := vs2.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, rec.Vector)
	assert.Equal(t, "func add(a, b)", rec.Chunk.Content)
	assert.Equal(t, "math.py", rec.Chunk.Filename)
	assert.Equal(t, chunker.TypeFunction, rec.Chunk.Type)
}

func TestUpsertOverwrite(t *testing.T) {
	vs := New()
	vs.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{Content: "old"})
	vs.Upsert("chunk-1", []float32{0, 1, 0}, &chunker.Chunk{Content: "new"})

	rec, ok := vs.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, []float32{0, 1, 0}, rec.Vector)
	assert.Equal(t, "new", rec.Chunk.Content)
}

func TestDelete(t *testing.T) {
	vs := New()
	vs.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{Content: "hello"})

	assert.True(t, vs.Delete("chunk-1"))
	assert.Equal(t, 0, vs.Count())
	assert.False(t, vs.Delete("chunk-1"))
}

func TestSearchEmptyStore(t *testing.T) {
	vs := New()
	results := vs.Search([]float32{1, 0, 0}, 3, 0.0)
	assert.Nil(t, results)
}

func TestSearchTopK(t *testing.T) {
	vs := New()
	// Three orthogonal unit vectors in 3D space
	vs.Upsert("x", []float32{1, 0, 0}, &chunker.Chunk{ID: "x", Content: "x-axis"})
	vs.Upsert("y", []float32{0, 1, 0}, &chunker.Chunk{ID: "y", Content: "y-axis"})
	vs.Upsert("z", []float32{0, 0, 1}, &chunker.Chunk{ID: "z", Content: "z-axis"})

	// Query closest to x
	results := vs.Search([]float32{1, 0.1, 0}, 2, 0.0)
	require.Len(t, results, 2)
	assert.Equal(t, "x", results[0].Record.ID)
	assert.InDelta(t, 1.0, results[0].Similarity, 0.01)

	// Query closest to y
	results = vs.Search([]float32{0.1, 1, 0}, 1, 0.0)
	require.Len(t, results, 1)
	assert.Equal(t, "y", results[0].Record.ID)
}

func TestSearchZeroVector(t *testing.T) {
	vs := New()
	vs.Upsert("a", []float32{1, 0, 0}, &chunker.Chunk{Content: "a"})
	results := vs.Search([]float32{0, 0, 0}, 3, 0.0)
	assert.Nil(t, results)
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "store.json")

	vs := New()
	vs.Upsert("chunk-1", []float32{0.1, 0.2, 0.3}, &chunker.Chunk{ID: "chunk-1", Type: chunker.TypeFunction, Content: "func add(a, b)", Filename: "math.py", Name: "add"})
	vs.Upsert("chunk-2", []float32{0.4, 0.5, 0.6}, &chunker.Chunk{ID: "chunk-2", Type: chunker.TypeClass, Content: "class User", Filename: "user.py", Name: "User"})

	err := vs.Save(path)
	require.NoError(t, err)
	assert.FileExists(t, path)

	vs2 := New()
	err = vs2.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 2, vs2.Count())

	rec1, ok := vs2.Get("chunk-1")
	require.True(t, ok)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, rec1.Vector)
	assert.Equal(t, "func add(a, b)", rec1.Chunk.Content)
	assert.Equal(t, "math.py", rec1.Chunk.Filename)
	assert.Equal(t, "add", rec1.Chunk.Name)

	rec2, ok := vs2.Get("chunk-2")
	require.True(t, ok)
	assert.Equal(t, chunker.TypeClass, rec2.Chunk.Type)
}

func TestLoadMissingFile(t *testing.T) {
	vs := New()
	err := vs.Load("/nonexistent/path/store.json")
	assert.Error(t, err)
}

func TestSearchSimilarityOrdering(t *testing.T) {
	vs := New()
	vs.Upsert("a", []float32{1, 0, 0}, &chunker.Chunk{ID: "a"})
	vs.Upsert("b", []float32{0.8, 0.6, 0}, &chunker.Chunk{ID: "b"})
	vs.Upsert("c", []float32{0.5, 0.5, 0.5}, &chunker.Chunk{ID: "c"})

	results := vs.Search([]float32{1, 0, 0}, 3, 0.0)
	require.Len(t, results, 3)
	assert.Equal(t, "a", results[0].Record.ID)
	assert.Equal(t, "b", results[1].Record.ID)
	assert.Equal(t, "c", results[2].Record.ID)
	assert.Greater(t, results[0].Similarity, results[1].Similarity)
	assert.Greater(t, results[1].Similarity, results[2].Similarity)
}

func TestSearchDifferentDimensionVectors(t *testing.T) {
	vs := New()
	vs.Upsert("a", []float32{1, 0}, &chunker.Chunk{ID: "a"})
	vs.Upsert("b", []float32{0, 1, 0}, &chunker.Chunk{ID: "b"})

	results := vs.Search([]float32{1, 0, 0}, 2, 0.0)
	require.Len(t, results, 2)
	// a matches in first 2 dims, b matches in dims 2-3
	// query [1,0,0] vs a [1,0] -> dot=1, |a|=1, sim=1
	// query [1,0,0] vs b [0,1,0] -> dot=0, sim=0
	assert.Equal(t, "a", results[0].Record.ID)
	assert.InDelta(t, 1.0, results[0].Similarity, 0.001)
	assert.Equal(t, "b", results[1].Record.ID)
	assert.InDelta(t, 0.0, results[1].Similarity, 0.001)
}

func TestSQLiteShouldIndexFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs.Close()

	// New file should need indexing
	assert.True(t, vs.ShouldIndexFile("src/main.py", 1000, 500))

	// Mark as indexed
	vs.MarkFileIndexed("src/main.py", 1000, 500)

	// Same mtime and size should not need indexing
	assert.False(t, vs.ShouldIndexFile("src/main.py", 1000, 500))

	// Changed mtime should need indexing
	assert.True(t, vs.ShouldIndexFile("src/main.py", 2000, 500))

	// Changed size should need indexing
	assert.True(t, vs.ShouldIndexFile("src/main.py", 1000, 600))
}

func TestSQLitePruneFiles(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.db")

	vs, err := NewSQLite(path)
	require.NoError(t, err)
	defer vs.Close()

	vs.Upsert("a#1", []float32{1, 0, 0}, &chunker.Chunk{ID: "a#1", Filename: "a.py"})
	vs.Upsert("b#1", []float32{0, 1, 0}, &chunker.Chunk{ID: "b#1", Filename: "b.py"})
	vs.MarkFileIndexed("a.py", 1000, 100)
	vs.MarkFileIndexed("b.py", 1000, 100)
	assert.Equal(t, 2, vs.Count())

	removed := vs.PruneFiles([]string{"a.py"})
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, vs.Count())

	_, ok := vs.Get("a#1")
	assert.True(t, ok)
	_, ok = vs.Get("b#1")
	assert.False(t, ok)

	// b.py should be removed from file_index too
	assert.True(t, vs.ShouldIndexFile("b.py", 1000, 100))
}

func BenchmarkSearch1000(b *testing.B) {
	vs := New()
	for i := 0; i < 1000; i++ {
		vec := make([]float32, 384)
		for j := range vec {
			vec[j] = float32(i+j) / 1000.0
		}
		vs.Upsert(fmt.Sprintf("chunk-%d", i), vec, &chunker.Chunk{ID: fmt.Sprintf("chunk-%d", i)})
	}
	query := make([]float32, 384)
	for j := range query {
		query[j] = float32(j) / 1000.0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vs.Search(query, 5, 0.0)
	}
}

func TestSearchMinSimilarity(t *testing.T) {
	vs := New()
	vs.Upsert("a", []float32{1, 0, 0}, &chunker.Chunk{ID: "a", Content: "high-match"})
	vs.Upsert("b", []float32{0, 0.2, 0}, &chunker.Chunk{ID: "b", Content: "low-match"})

	// With threshold 0.0 — both returned
	all := vs.Search([]float32{1, 0, 0}, 5, 0.0)
	assert.Len(t, all, 2)

	// With threshold 0.5 — only high-match returned
	filtered := vs.Search([]float32{1, 0, 0}, 5, 0.5)
	require.Len(t, filtered, 1)
	assert.Equal(t, "a", filtered[0].Record.ID)
	assert.Greater(t, filtered[0].Similarity, 0.5)
}
