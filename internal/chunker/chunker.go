package chunker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
)

// ChunkType indicates the kind of code unit this chunk represents.
type ChunkType string

const (
	TypeClass    ChunkType = "class"
	TypeFunction ChunkType = "function"
	TypeModule   ChunkType = "module"
	TypeImport   ChunkType = "import"
	TypeDocument ChunkType = "document"
)

// Chunk represents a semantic unit of code ready for embedding.
type Chunk struct {
	ID         string    `json:"id"`
	Type       ChunkType `json:"type"`
	Content    string    `json:"content"`
	Filename   string    `json:"filename"`
	Name       string    `json:"name,omitempty"`
	Bases      []string  `json:"bases,omitempty"`
	Params     []string  `json:"params,omitempty"`
	ReturnType string    `json:"return_type,omitempty"`
	StartLine  int       `json:"start_line,omitempty"`
}

// Chunker creates semantic chunks from analyzer results.
type Chunker struct {
	sourceDir string
}

// New creates a new Chunker. sourceDir is the project root directory,
// used for reading source code lines to include in chunks.
func New(sourceDir string) *Chunker {
	return &Chunker{sourceDir: sourceDir}
}

// ChunkFiles splits parsed file results into semantic chunks.
func (c *Chunker) ChunkFiles(files []*analyzer.FileResult) []*Chunk {
	var chunks []*Chunk
	for _, f := range files {
		chunks = append(chunks, c.chunkFile(f)...)
	}
	return chunks
}

func (c *Chunker) chunkFile(f *analyzer.FileResult) []*Chunk {
	var chunks []*Chunk
	filename := f.Filename

	// Module-level import chunk
	if len(f.Imports) > 0 {
		chunks = append(chunks, c.buildImportChunk(filename, f.Imports))
	}

	// Class chunks (include methods summary)
	for _, cls := range f.Classes {
		chunks = append(chunks, c.buildClassChunk(filename, cls))
	}

	// Standalone function chunks
	for _, fn := range f.Functions {
		chunks = append(chunks, c.buildFunctionChunk(filename, fn))
	}

	// If file has no symbols at all, create a single module chunk
	if len(chunks) == 0 {
		chunks = append(chunks, &Chunk{
			ID:       fmt.Sprintf("%s#module", filename),
			Type:     TypeModule,
			Content:  fmt.Sprintf("File: %s", filename),
			Filename: filename,
		})
	}

	return chunks
}

func (c *Chunker) buildImportChunk(filename string, imports []analyzer.ImportInfo) *Chunk {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("File %s imports:\n", filename))
	for _, imp := range imports {
		if imp.Name != "" {
			b.WriteString(fmt.Sprintf("- %s from %s\n", imp.Name, imp.Module))
		} else {
			b.WriteString(fmt.Sprintf("- %s\n", imp.Module))
		}
	}
	return &Chunk{
		ID:       fmt.Sprintf("%s#imports", filename),
		Type:     TypeImport,
		Content:  b.String(),
		Filename: filename,
	}
}

func (c *Chunker) buildClassChunk(filename string, cls analyzer.ClassInfo) *Chunk {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Class %s", cls.Name))
	if len(cls.Bases) > 0 {
		b.WriteString(fmt.Sprintf(" extends %s", strings.Join(cls.Bases, ", ")))
	}
	if len(cls.Decorators) > 0 {
		b.WriteString(fmt.Sprintf(" [decorators: %s]", strings.Join(cls.Decorators, ", ")))
	}
	b.WriteString("\n")

	if len(cls.Methods) > 0 {
		b.WriteString("Methods:\n")
		for _, m := range cls.Methods {
			b.WriteString(fmt.Sprintf("- %s(%s)", m.Name, strings.Join(m.Params, ", ")))
			if m.ReturnType != "" {
				b.WriteString(fmt.Sprintf(" -> %s", m.ReturnType))
			}
			b.WriteString("\n")
		}
	}

	if src := c.readSourceLines(filename, cls.StartLine, cls.EndLine); src != "" {
		b.WriteString("\n```\n")
		b.WriteString(src)
		b.WriteString("\n```\n")
	}

	return &Chunk{
		ID:        fmt.Sprintf("%s#%s", filename, cls.Name),
		Type:      TypeClass,
		Content:   b.String(),
		Filename:  filename,
		Name:      cls.Name,
		Bases:     append([]string(nil), cls.Bases...),
		StartLine: cls.StartLine,
	}
}

func (c *Chunker) buildFunctionChunk(filename string, fn analyzer.FunctionInfo) *Chunk {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Function %s(%s)", fn.Name, strings.Join(fn.Params, ", ")))
	if fn.ReturnType != "" {
		b.WriteString(fmt.Sprintf(" -> %s", fn.ReturnType))
	}
	if len(fn.Decorators) > 0 {
		b.WriteString(fmt.Sprintf(" [decorators: %s]", strings.Join(fn.Decorators, ", ")))
	}
	b.WriteString("\n")

	if src := c.readSourceLines(filename, fn.StartLine, fn.EndLine); src != "" {
		b.WriteString("\n```\n")
		b.WriteString(src)
		b.WriteString("\n```\n")
	}

	return &Chunk{
		ID:         fmt.Sprintf("%s#%s", filename, fn.Name),
		Type:       TypeFunction,
		Content:    b.String(),
		Filename:   filename,
		Name:       fn.Name,
		Params:     append([]string(nil), fn.Params...),
		ReturnType: fn.ReturnType,
		StartLine:  fn.StartLine,
	}
}

// readSourceLines reads source code lines for a symbol from its source file.
// Returns empty string if the file cannot be read or line range is invalid.
// Caps output at 60 lines to prevent oversized chunks.
func (c *Chunker) readSourceLines(filename string, startLine, endLine int) string {
	if c.sourceDir == "" || startLine <= 0 || endLine <= startLine {
		return ""
	}

	fullPath := filepath.Join(c.sourceDir, filename)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	const maxLines = 60
	if endLine-startLine+1 > maxLines {
		endLine = startLine + maxLines - 1
	}

	return strings.Join(lines[startLine-1:endLine], "\n")
}

// ChunkWikiDocs splits wiki-generated documents into semantic chunks for RAG indexing.
// Keys are document names (e.g. "architecture"), values are markdown content.
// Splits by h2 headings with a per-section cap of ~800 characters.
func (c *Chunker) ChunkWikiDocs(docs map[string]string) []*Chunk {
	var chunks []*Chunk

	for docName, content := range docs {
		if strings.TrimSpace(content) == "" {
			continue
		}
		sections := splitByH2(content)
		for _, sec := range sections {
			if strings.TrimSpace(sec.body) == "" {
				continue
			}
			slug := slugify(sec.heading)
			id := fmt.Sprintf("wiki/%s#%s", docName, slug)
			filename := fmt.Sprintf("wiki/%s.md", docName)

			// Cap section body at ~800 characters to keep chunks manageable
			body := sec.body
			if len([]rune(body)) > 800 {
				body = string([]rune(body)[:800]) + "..."
			}

			chunks = append(chunks, &Chunk{
				ID:       id,
				Type:     TypeDocument,
				Content:  fmt.Sprintf("文档: %s\n章节: %s\n\n%s", docName, sec.heading, body),
				Filename: filename,
				Name:     sec.heading,
			})
		}
	}

	return chunks
}

// wikiSection holds a parsed h2 heading section.
type wikiSection struct {
	heading string
	body    string
}

// splitByH2 splits markdown content by "## " headings.
// Content before the first heading is assigned heading "" (preamble).
func splitByH2(content string) []wikiSection {
	lines := strings.Split(content, "\n")
	var sections []wikiSection
	var current *wikiSection

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &wikiSection{
				heading: strings.TrimSpace(strings.TrimPrefix(line, "## ")),
			}
		} else if current != nil {
			if current.body != "" {
				current.body += "\n"
			}
			current.body += line
		}
	}

	if current != nil {
		sections = append(sections, *current)
	}

	return sections
}

// slugify converts a heading string to a URL-friendly slug.
func slugify(s string) string {
	if s == "" {
		return "top"
	}
	slug := strings.TrimSpace(s)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "/", "-")
	// Remove non-ASCII for clean anchor IDs
	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return "section"
	}
	return strings.ToLower(result.String())
}
