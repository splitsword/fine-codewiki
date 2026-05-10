package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
)

const cacheVersion = "2"

// FileEntry tracks mtime and size for cache invalidation.
type FileEntry struct {
	MTime int64 `json:"mtime"`
	Size  int64 `json:"size"`
}

// CacheData is the on-disk format for the unified cache.
type CacheData struct {
	Version  string                         `json:"version"`
	Manifest map[string]FileEntry          `json:"manifest"`
	AST      map[string]*analyzer.FileResult `json:"ast"`
	Graph    *grapher.Graph                 `json:"graph,omitempty"`
}

// Cache manages AST and graph caching between generate runs.
type Cache struct {
	path     string
	data     *CacheData
	hitCount int
	mu       sync.RWMutex
}

// New creates a Cache backed by the given file path.
func New(path string) *Cache {
	return &Cache{
		path: path,
		data: &CacheData{
			Version:  cacheVersion,
			Manifest: make(map[string]FileEntry),
			AST:      make(map[string]*analyzer.FileResult),
		},
	}
}

// Load reads the cache file from disk if it exists and is valid.
func (c *Cache) Load() error {
	b, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data CacheData
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("parse cache: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if data.Version != cacheVersion {
		// Version mismatch: discard old cache
		c.data = &CacheData{
			Version:  cacheVersion,
			Manifest: make(map[string]FileEntry),
			AST:      make(map[string]*analyzer.FileResult),
		}
		return nil
	}
	if data.Manifest == nil {
		data.Manifest = make(map[string]FileEntry)
	}
	if data.AST == nil {
		data.AST = make(map[string]*analyzer.FileResult)
	}
	c.data = &data
	return nil
}

// Save persists the current cache to disk.
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	c.mu.RLock()
	data := c.data
	c.mu.RUnlock()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	return os.WriteFile(c.path, b, 0644)
}

// GetAST returns a cached FileResult if the file has not changed.
func (c *Cache) GetAST(filename string, mtime, size int64) (*analyzer.FileResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data.Manifest[filename]
	if !ok || entry.MTime != mtime || entry.Size != size {
		return nil, false
	}
	fr, ok := c.data.AST[filename]
	if !ok || fr == nil {
		return nil, false
	}
	c.hitCount++
	return fr, true
}

// PutAST stores a FileResult in the cache.
func (c *Cache) PutAST(filename string, mtime, size int64, result *analyzer.FileResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Manifest[filename] = FileEntry{MTime: mtime, Size: size}
	c.data.AST[filename] = result
}

// GetGraph returns the cached graph if available.
func (c *Cache) GetGraph() *grapher.Graph {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.Graph
}

// PutGraph stores the graph in the cache.
func (c *Cache) PutGraph(g *grapher.Graph) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Graph = g
}

// Prune removes entries for files that no longer exist.
func (c *Cache) Prune(currentFiles []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := make(map[string]bool, len(currentFiles))
	for _, f := range currentFiles {
		keep[f] = true
	}
	for filename := range c.data.Manifest {
		if !keep[filename] {
			delete(c.data.Manifest, filename)
			delete(c.data.AST, filename)
		}
	}
}

// HitCount returns the number of AST cache hits since creation.
func (c *Cache) HitCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hitCount
}

// CachedFiles returns the list of files currently in the AST cache.
func (c *Cache) CachedFiles() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	files := make([]string, 0, len(c.data.AST))
	for f := range c.data.AST {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}
