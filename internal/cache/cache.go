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

const cacheVersion = "3"

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
// The returned value is a deep copy so callers cannot mutate the cached object.
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
	return copyFileResult(fr), true
}

// PutAST stores a deep copy of the FileResult in the cache.
func (c *Cache) PutAST(filename string, mtime, size int64, result *analyzer.FileResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Manifest[filename] = FileEntry{MTime: mtime, Size: size}
	c.data.AST[filename] = copyFileResult(result)
}

// copyFileResult creates a deep copy of a FileResult so that mutations
// to the returned pointer do not affect the cached data.
func copyFileResult(fr *analyzer.FileResult) *analyzer.FileResult {
	if fr == nil {
		return nil
	}
	copied := &analyzer.FileResult{
		Filename:  fr.Filename,
		Classes:   make([]analyzer.ClassInfo, len(fr.Classes)),
		Functions: make([]analyzer.FunctionInfo, len(fr.Functions)),
		Imports:   make([]analyzer.ImportInfo, len(fr.Imports)),
	}
	for i, c := range fr.Classes {
		copied.Classes[i] = analyzer.ClassInfo{
			Name:       c.Name,
			Bases:      append([]string(nil), c.Bases...),
			Methods:    make([]analyzer.FunctionInfo, len(c.Methods)),
			Decorators: append([]string(nil), c.Decorators...),
			StartLine:  c.StartLine,
		}
		for j, m := range c.Methods {
			copied.Classes[i].Methods[j] = analyzer.FunctionInfo{
				Name:       m.Name,
				Params:     append([]string(nil), m.Params...),
				ReturnType: m.ReturnType,
				Decorators: append([]string(nil), m.Decorators...),
				StartLine:  m.StartLine,
			}
		}
	}
	for i, f := range fr.Functions {
		copied.Functions[i] = analyzer.FunctionInfo{
			Name:       f.Name,
			Params:     append([]string(nil), f.Params...),
			ReturnType: f.ReturnType,
			Decorators: append([]string(nil), f.Decorators...),
			StartLine:  f.StartLine,
		}
	}
	copy(copied.Imports, fr.Imports)
	return copied
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
