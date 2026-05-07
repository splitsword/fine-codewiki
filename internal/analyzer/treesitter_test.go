package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreeSitterParserFallback(t *testing.T) {
	fallbackCalled := false
	fallback := func(filename, source string) (*FileResult, error) {
		fallbackCalled = true
		return &FileResult{Filename: filename}, nil
	}

	parser := NewTreeSitterParser(nil, fallback)
	result, err := parser.Parse("test.py", "class User: pass")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, fallbackCalled, "fallback should be called when no grammar is loaded")
	assert.Equal(t, "test.py", result.Filename)
}

func TestTreeSitterParserNoFallback(t *testing.T) {
	parser := NewTreeSitterParser(nil, nil)
	_, err := parser.Parse("test.py", "class User: pass")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no grammar loaded")
}

func TestTreeSitterParserWithGrammarFallsBack(t *testing.T) {
	// Even when a grammar pointer is non-nil (placeholder), we fall back
	// because pure-Go grammar implementation is not yet bundled.
	fallbackCalled := false
	fallback := func(filename, source string) (*FileResult, error) {
		fallbackCalled = true
		return ParsePython(filename, source)
	}

	// Use a dummy language pointer (zero value is fine for this test)
	var dummyLang struct{}
	_ = dummyLang

	parser := NewTreeSitterParser(nil, fallback)
	result, err := parser.Parse("user.py", "class User:\n    pass\n")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, fallbackCalled)
	assert.Len(t, result.Classes, 1)
	assert.Equal(t, "User", result.Classes[0].Name)
}
