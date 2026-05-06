package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/diagram"
	"github.com/splitsword/fine-codewiki/internal/docgen"
	"github.com/splitsword/fine-codewiki/internal/grapher"
)

// Config holds CLI configuration.
type Config struct {
	SourceDir   string
	OutputDir   string
	Language    string
	ProjectName string
	Port        int
}

// RunGenerate executes the full generate pipeline: parse → graph → diagram → doc.
func RunGenerate(cfg *Config) error {
	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(cfg.SourceDir)
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	}

	fmt.Printf("Parsing source files in %s...\n", cfg.SourceDir)
	files, err := analyzer.ParseDirectory(cfg.SourceDir, cfg.Language)
	if err != nil {
		return fmt.Errorf("parse directory: %w", err)
	}
	fmt.Printf("Found %d source files\n", len(files))

	// Normalize filenames: strip source directory prefix so module names are relative to project root
	absSource, err := filepath.Abs(cfg.SourceDir)
	if err != nil {
		absSource = cfg.SourceDir
	}
	for _, f := range files {
		absFile, err := filepath.Abs(f.Filename)
		if err == nil {
			f.Filename = absFile
		}
		if strings.HasPrefix(f.Filename, absSource) {
			rel := strings.TrimPrefix(f.Filename, absSource)
			rel = strings.TrimPrefix(rel, string(filepath.Separator))
			rel = strings.TrimPrefix(rel, "/")
			f.Filename = rel
		}
	}

	fmt.Println("Building dependency graph...")
	graph := grapher.BuildGraph(files)
	fmt.Printf("Graph: %d nodes, %d edges\n", len(graph.Nodes), len(graph.Edges))

	fmt.Println("Generating diagrams...")
	archDSL, err := diagram.GenerateArchitectureDiagram(graph)
	if err != nil {
		return fmt.Errorf("generate architecture diagram: %w", err)
	}

	classDSL, err := diagram.GenerateClassDiagram(graph)
	if err != nil {
		return fmt.Errorf("generate class diagram: %w", err)
	}

	fmt.Println("Generating documentation...")
	wiki, err := docgen.GenerateWiki(graph, cfg.ProjectName, archDSL, classDSL)
	if err != nil {
		return fmt.Errorf("generate wiki: %w", err)
	}

	fmt.Printf("Writing wiki to %s...\n", cfg.OutputDir)
	if err := docgen.WriteWikiFiles(cfg.OutputDir, wiki); err != nil {
		return fmt.Errorf("write wiki files: %w", err)
	}

	fmt.Println("Done!")
	return nil
}

// RunServe starts a local HTTP server to preview the generated wiki.
func RunServe(cfg *Config) error {
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(".", ".codewiki", "wiki")
	}

	if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
		return fmt.Errorf("wiki directory not found: %s (run 'generate' first)", cfg.OutputDir)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Printf("Serving wiki from %s at http://localhost%s\n", cfg.OutputDir, addr)
	fmt.Println("Press Ctrl+C to stop")

	handler := newWikiHandler(cfg.OutputDir)
	return http.ListenAndServe(addr, handler)
}

// wikiHandler serves wiki files with appropriate content types.
type wikiHandler struct {
	root string
}

func newWikiHandler(root string) http.Handler {
	return &wikiHandler{root: root}
}

func (h *wikiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.FromSlash(r.URL.Path)
	if path == "/" || path == "\\" {
		path = "index.html"
	} else {
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimPrefix(path, "\\")
	}

	fullPath := filepath.Join(h.root, path)
	fullPath = filepath.Clean(fullPath)

	// Security: prevent directory traversal
	absRoot, _ := filepath.Abs(h.root)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(path))
	w.Write(data)
}

func contentTypeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".mmd":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	default:
		return "text/plain; charset=utf-8"
	}
}
