package vectorstore

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	_ "modernc.org/sqlite"
)

// Record holds a single embedded chunk.
type Record struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Chunk   *chunker.Chunk `json:"chunk"`
}

// SearchResult is returned by a vector search.
type SearchResult struct {
	Record     *Record
	Similarity float64
}

// VectorStore is a vector database with optional SQLite persistence.
type VectorStore struct {
	records map[string]*Record
	db      *sql.DB
	cache   map[string]*Record // in-memory cache for SQLite-backed stores
}

// New creates an empty in-memory VectorStore.
func New() *VectorStore {
	return &VectorStore{
		records: make(map[string]*Record),
	}
}

// NewSQLite opens (or creates) a SQLite-backed vector store.
// Loads all existing records into an in-memory cache for fast search.
func NewSQLite(path string) (*VectorStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	schema := `CREATE TABLE IF NOT EXISTS vectors (
		id TEXT PRIMARY KEY,
		vector BLOB NOT NULL,
		chunk_json TEXT,
		source_file TEXT
	);
	CREATE TABLE IF NOT EXISTS file_index (
		path TEXT PRIMARY KEY,
		mtime INTEGER NOT NULL,
		size INTEGER NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	vs := &VectorStore{db: db}
	// Preload all records into cache
	records := vs.loadAllFromSQLite()
	vs.cache = make(map[string]*Record, len(records))
	for _, rec := range records {
		vs.cache[rec.ID] = rec
	}

	return vs, nil
}

// Close releases the SQLite connection if any.
func (vs *VectorStore) Close() error {
	if vs.db != nil {
		return vs.db.Close()
	}
	return nil
}

// Upsert inserts or updates a record by ID.
func (vs *VectorStore) Upsert(id string, vector []float32, chunk *chunker.Chunk) {
	rec := &Record{
		ID:     id,
		Vector: append([]float32(nil), vector...),
		Chunk:  chunk,
	}
	if vs.db != nil {
		vecBlob := vectorToBlob(vector)
		var chunkJSON []byte
		var sourceFile string
		if chunk != nil {
			chunkJSON, _ = json.Marshal(chunk)
			sourceFile = chunk.Filename
		}
		_, _ = vs.db.Exec("INSERT OR REPLACE INTO vectors (id, vector, chunk_json, source_file) VALUES (?, ?, ?, ?)", id, vecBlob, string(chunkJSON), sourceFile)
		vs.cache[id] = rec
		return
	}
	vs.records[id] = rec
}

// ShouldIndexFile returns true if the file is new or has changed since last index.
func (vs *VectorStore) ShouldIndexFile(path string, mtime, size int64) bool {
	if vs.db == nil {
		return true
	}
	var storedMtime, storedSize int64
	err := vs.db.QueryRow("SELECT mtime, size FROM file_index WHERE path = ?", path).Scan(&storedMtime, &storedSize)
	if err != nil {
		return true
	}
	return storedMtime != mtime || storedSize != size
}

// MarkFileIndexed records that a file has been indexed at the given mtime and size.
func (vs *VectorStore) MarkFileIndexed(path string, mtime, size int64) {
	if vs.db == nil {
		return
	}
	_, _ = vs.db.Exec("INSERT OR REPLACE INTO file_index (path, mtime, size) VALUES (?, ?, ?)", path, mtime, size)
}

// PruneFiles removes vectors and metadata for files not in keepPaths.
// Returns the number of records removed.
func (vs *VectorStore) PruneFiles(keepPaths []string) int {
	if vs.db == nil {
		return 0
	}
	keepSet := make(map[string]bool, len(keepPaths))
	for _, p := range keepPaths {
		keepSet[p] = true
	}

	// Find files to remove from file_index
	rows, err := vs.db.Query("SELECT path FROM file_index")
	if err != nil {
		return 0
	}
	var toRemove []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if !keepSet[path] {
			toRemove = append(toRemove, path)
		}
	}
	rows.Close()

	removed := 0
	for _, path := range toRemove {
		// Remove from cache first (need to know IDs)
		for id, rec := range vs.cache {
			if rec.Chunk != nil && rec.Chunk.Filename == path {
				delete(vs.cache, id)
				removed++
			}
		}
		res, _ := vs.db.Exec("DELETE FROM vectors WHERE source_file = ?", path)
		n, _ := res.RowsAffected()
		if removed == 0 {
			removed = int(n)
		}
		_, _ = vs.db.Exec("DELETE FROM file_index WHERE path = ?", path)
	}
	return removed
}

// Delete removes a record by ID. Returns true if the record existed.
func (vs *VectorStore) Delete(id string) bool {
	if vs.db != nil {
		res, err := vs.db.Exec("DELETE FROM vectors WHERE id = ?", id)
		if err != nil {
			return false
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			delete(vs.cache, id)
		}
		return n > 0
	}
	if _, ok := vs.records[id]; ok {
		delete(vs.records, id)
		return true
	}
	return false
}

// Get retrieves a single record by ID.
func (vs *VectorStore) Get(id string) (*Record, bool) {
	if vs.db != nil {
		if rec, ok := vs.cache[id]; ok {
			return rec, true
		}
		return nil, false
	}
	r, ok := vs.records[id]
	return r, ok
}

// Count returns the number of stored records.
func (vs *VectorStore) Count() int {
	if vs.db != nil {
		var n int
		_ = vs.db.QueryRow("SELECT COUNT(*) FROM vectors").Scan(&n)
		return n
	}
	return len(vs.records)
}

// Search performs a cosine-similarity Top-K search with an optional minimum
// similarity threshold. Results below minSimilarity are filtered out.
// Returns results sorted by similarity descending.
func (vs *VectorStore) Search(query []float32, topK int, minSimilarity float64) []SearchResult {
	if len(query) == 0 {
		return nil
	}

	qNorm := l2Norm(query)
	if qNorm == 0 {
		return nil
	}

	var records []*Record
	if vs.db != nil {
		records = make([]*Record, 0, len(vs.cache))
		for _, rec := range vs.cache {
			records = append(records, rec)
		}
	} else {
		records = make([]*Record, 0, len(vs.records))
		for _, rec := range vs.records {
			records = append(records, rec)
		}
	}

	if len(records) == 0 {
		return nil
	}

	results := make([]SearchResult, 0, len(records))
	for _, rec := range records {
		if len(rec.Vector) == 0 {
			continue
		}
		sim := cosineSimilarity(query, rec.Vector, qNorm)
		if sim >= minSimilarity {
			results = append(results, SearchResult{
				Record:     rec,
				Similarity: sim,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}
	return results
}

func (vs *VectorStore) loadAllFromSQLite() []*Record {
	rows, err := vs.db.Query("SELECT id, vector, chunk_json FROM vectors")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var records []*Record
	for rows.Next() {
		var id string
		var vecBlob []byte
		var chunkJSON string
		if err := rows.Scan(&id, &vecBlob, &chunkJSON); err != nil {
			continue
		}
		rec := &Record{
			ID:     id,
			Vector: blobToVector(vecBlob),
		}
		if chunkJSON != "" {
			_ = json.Unmarshal([]byte(chunkJSON), &rec.Chunk)
		}
		records = append(records, rec)
	}
	return records
}

// Save persists the store to a JSON file (in-memory only).
func (vs *VectorStore) Save(path string) error {
	if vs.db != nil {
		return nil // SQLite persists automatically
	}
	data := make([]*Record, 0, len(vs.records))
	for _, rec := range vs.records {
		data = append(data, rec)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vector store: %w", err)
	}
	return os.WriteFile(path, b, 0644)
}

// Load restores the store from a JSON file (in-memory only).
func (vs *VectorStore) Load(path string) error {
	if vs.db != nil {
		return nil // SQLite loads automatically on open
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read vector store: %w", err)
	}
	var data []*Record
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("unmarshal vector store: %w", err)
	}
	vs.records = make(map[string]*Record, len(data))
	for _, rec := range data {
		vs.records[rec.ID] = rec
	}
	return nil
}

func vectorToBlob(v []float32) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, v)
	return buf.Bytes()
}

func blobToVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(b)/4)
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, vec)
	return vec
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32, aNorm float64) float64 {
	dot := float64(0)
	bNorm := float64(0)
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		bNorm += bv * bv
	}
	if bNorm == 0 {
		return 0
	}
	return dot / (aNorm * math.Sqrt(bNorm))
}

func l2Norm(v []float32) float64 {
	sum := float64(0)
	for _, x := range v {
		f := float64(x)
		sum += f * f
	}
	return math.Sqrt(sum)
}
