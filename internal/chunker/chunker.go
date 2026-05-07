package chunker

import (
	"fmt"
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
type Chunker struct{}

// New creates a new Chunker.
func New() *Chunker {
	return &Chunker{}
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
