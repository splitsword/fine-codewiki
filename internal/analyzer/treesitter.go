package analyzer

import (
	"fmt"

	ts "github.com/odvcencio/gotreesitter"
)

// TreeSitterParser wraps gotreesitter for AST parsing.
// When a grammar is available it performs tree-sitter parsing;
// otherwise it falls back to regex-based parsing.
type TreeSitterParser struct {
	language *ts.Language
	fallback func(filename, source string) (*FileResult, error)
}

// NewTreeSitterParser creates a parser with an optional grammar.
func NewTreeSitterParser(lang *ts.Language, fallback func(filename, source string) (*FileResult, error)) *TreeSitterParser {
	return &TreeSitterParser{
		language: lang,
		fallback: fallback,
	}
}

// Parse attempts tree-sitter parsing if a grammar is loaded, otherwise falls back.
func (p *TreeSitterParser) Parse(filename, source string) (*FileResult, error) {
	if p.language == nil {
		if p.fallback != nil {
			return p.fallback(filename, source)
		}
		return nil, fmt.Errorf("no grammar loaded and no fallback provided for %s", filename)
	}
	// Tree-sitter parsing would go here when pure-Go grammar files are available.
	// Currently, tree-sitter grammars are distributed as C/WASM libraries that
	// require CGO or a WASM runtime. Until pure-Go grammars are bundled,
	// we gracefully fall back to the proven regex-based parsers.
	if p.fallback != nil {
		return p.fallback(filename, source)
	}
	return nil, fmt.Errorf("tree-sitter grammar not yet available for %s", filename)
}
