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
	// Tree-sitter grammar is now bundled via gotreesitter/grammars.
	// Attempt real tree-sitter parsing; fall back to regex on failure or empty result.
	result, err := extractFromTreeSitter(p.language, []byte(source), filename)
	if err == nil && result != nil && (len(result.Classes) > 0 || len(result.Functions) > 0 || len(result.Imports) > 0) {
		return result, nil
	}
	if p.fallback != nil {
		return p.fallback(filename, source)
	}
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed for %s: %w", filename, err)
	}
	return result, nil
}
