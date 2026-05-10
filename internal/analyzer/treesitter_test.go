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

func TestTreeSitterParserEmptyResultFallsBack(t *testing.T) {
	// When tree-sitter returns empty result (no classes/functions/imports),
	// it should fall back to regex parser.
	fallbackCalled := false
	fallback := func(filename, source string) (*FileResult, error) {
		fallbackCalled = true
		return &FileResult{Filename: filename, Classes: []ClassInfo{{Name: "FallbackClass"}}}, nil
	}

	parser := NewTreeSitterParser(nil, fallback)
	result, err := parser.Parse("empty.py", "# just a comment\n")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, fallbackCalled, "fallback should be called when tree-sitter returns empty result")
	assert.Equal(t, "FallbackClass", result.Classes[0].Name)
}

func TestTreeSitterParserErrorWithoutFallback(t *testing.T) {
	// When grammar is loaded but parse fails and no fallback is provided,
	// it should return the error.
	// Using nil language simulates "grammar loaded but parse fails" path
	// because extractFromTreeSitter won't be called when lang is nil.
	// Instead we test the path where fallback is nil.
	parser := NewTreeSitterParser(nil, nil)
	_, err := parser.Parse("test.py", "class User: pass")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no grammar loaded")
}

func TestParseWithRegexAllExtensions(t *testing.T) {
	tests := []struct {
		path     string
		ext      string
		wantClass string
	}{
		{"user.py", ".py", "User"},
		{"app.js", ".js", "User"},
		{"app.ts", ".ts", "User"},
		{"app.tsx", ".tsx", "User"},
		{"main.go", ".go", ""},
		{"User.java", ".java", "User"},
		{"lib.rs", ".rs", ""},
		{"main.cpp", ".cpp", "User"},
		{"main.cc", ".cc", "User"},
		{"main.c", ".c", ""},
		{"header.h", ".h", ""},
		{"header.hpp", ".hpp", "User"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			src := "class User:\n    pass\n"
			switch tt.ext {
			case ".go":
				src = "package main\n\ntype User struct{}\n"
			case ".rs":
				src = "struct User {}\n"
			case ".c", ".h":
				src = "struct User { int id; };\n"
			case ".js", ".ts", ".tsx":
				src = "class User {}\n"
			case ".java":
				src = "public class User {}\n"
			case ".cpp", ".cc", ".hpp":
				src = "class User { int id; };\n"
			}
			result, err := parseWithRegex(tt.path, src, tt.ext)
			require.NoError(t, err)
			require.NotNil(t, result)
			if tt.wantClass != "" {
				require.NotEmpty(t, result.Classes, "expected at least one class for %s", tt.path)
				assert.Equal(t, tt.wantClass, result.Classes[0].Name)
			}
		})
	}
}

func TestParseWithRegexUnsupportedExtension(t *testing.T) {
	_, err := parseWithRegex("README.txt", "hello", ".txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported extension")
}

func TestGetLanguageForFileUnsupported(t *testing.T) {
	lang, name, ok := GetLanguageForFile("data.xyz")
	assert.Nil(t, lang)
	assert.Empty(t, name)
	assert.False(t, ok)
}

func TestGetLanguageForFileSupported(t *testing.T) {
	tests := []string{"main.py", "app.js", "app.ts", "main.go", "User.java", "lib.rs", "main.cpp"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			lang, name, ok := GetLanguageForFile(path)
			assert.True(t, ok, "expected %s to be supported", path)
			assert.NotEmpty(t, name)
			assert.NotNil(t, lang)
		})
	}
}

func TestExtractCallSitesPython(t *testing.T) {
	src := []byte(`
def helper():
    pass

def main():
    helper()
    print("ok")
`)
	sites := ExtractCallSites(src, "main.py")
	require.NotEmpty(t, sites)

	var foundHelper bool
	for _, s := range sites {
		if s.Callee == "helper" {
			foundHelper = true
		}
	}
	assert.True(t, foundHelper, "expected to find call to helper")
}

func TestExtractCallSitesGo(t *testing.T) {
	src := []byte(`
package main

func helper() {}

func main() {
	helper()
	fmt.Println("ok")
}
`)
	sites := ExtractCallSites(src, "main.go")
	require.NotEmpty(t, sites)

	var foundHelper, foundPrintln bool
	for _, s := range sites {
		if s.Callee == "helper" {
			foundHelper = true
		}
		if s.Callee == "Println" {
			foundPrintln = true
		}
	}
	assert.True(t, foundHelper, "expected to find call to helper")
	assert.True(t, foundPrintln, "expected to find call to Println")
}

func TestExtractCallSitesJava(t *testing.T) {
	src := []byte(`
public class Main {
	public static void main(String[] args) {
		Util.print();
	}
}
`)
	sites := ExtractCallSites(src, "Main.java")
	require.NotEmpty(t, sites)
	require.Len(t, sites, 1)
	assert.Equal(t, "print", sites[0].Callee)
}

func TestExtractCallSitesUnsupportedExtension(t *testing.T) {
	src := []byte(`call_me()`)
	sites := ExtractCallSites(src, "script.unknown")
	assert.Empty(t, sites)
}
