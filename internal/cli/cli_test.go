package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCommand(t *testing.T) {
	repoPath := filepath.Join("..", "..", "testdata", "repos", "python-basic")
	_, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		t.Skip("testdata not found, skipping integration test")
	}

	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, ".codewiki", "wiki")

	cfg := &Config{
		SourceDir:   repoPath,
		OutputDir:   outDir,
		Language:    "python",
		ProjectName: "python-basic",
	}

	err = RunGenerate(cfg)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(outDir, "overview.md"))
	assert.FileExists(t, filepath.Join(outDir, "api-reference.md"))
	assert.FileExists(t, filepath.Join(outDir, "architecture.md"))
	assert.FileExists(t, filepath.Join(outDir, "architecture.mmd"))
	assert.FileExists(t, filepath.Join(outDir, "class-diagram.mmd"))

	overview, err := os.ReadFile(filepath.Join(outDir, "overview.md"))
	require.NoError(t, err)
	assert.Contains(t, string(overview), "python-basic")
	assert.Contains(t, string(overview), "models/user")

	arch, err := os.ReadFile(filepath.Join(outDir, "architecture.mmd"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(arch), "graph TD"))
}

func TestGenerateCommandEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "wiki")

	cfg := &Config{
		SourceDir:   tmpDir,
		OutputDir:   outDir,
		Language:    "python",
		ProjectName: "empty",
	}

	err := RunGenerate(cfg)
	require.NoError(t, err)

	overview, err := os.ReadFile(filepath.Join(outDir, "overview.md"))
	require.NoError(t, err)
	assert.Contains(t, string(overview), "No modules found")
}

func TestGenerateCommandInvalidSource(t *testing.T) {
	cfg := &Config{
		SourceDir:   "/nonexistent/path",
		OutputDir:   "/tmp/out",
		ProjectName: "test",
	}

	err := RunGenerate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse directory")
}

func TestWikiHandler(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "overview.md"), []byte("# Test\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "architecture.mmd"), []byte("graph TD\n"), 0644))

	indexContent := `<html><body><a href="overview.md">Overview</a></body></html>`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644))

	handler := newWikiHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/overview.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Body.String(), "# Test")
	assert.Equal(t, "text/markdown; charset=utf-8", rr.Header().Get("Content-Type"))
}

func TestWikiHandlerNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	handler := newWikiHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestWikiHandlerMermaidContentType(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "diagram.mmd"), []byte("graph TD\n"), 0644))

	handler := newWikiHandler(tmpDir)
	req := httptest.NewRequest(http.MethodGet, "/diagram.mmd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rr.Header().Get("Content-Type"))
}

func TestWikiHandlerDirectoryRequest(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))

	handler := newWikiHandler(tmpDir)
	req := httptest.NewRequest(http.MethodGet, "/subdir", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestWikiHandlerDirectoryTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := newWikiHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = "../../../etc/passwd"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 403, rr.Code)
}

func TestContentTypeFor(t *testing.T) {
	assert.Equal(t, "text/html; charset=utf-8", contentTypeFor("index.html"))
	assert.Equal(t, "text/css; charset=utf-8", contentTypeFor("style.css"))
	assert.Equal(t, "application/javascript; charset=utf-8", contentTypeFor("app.js"))
	assert.Equal(t, "application/json; charset=utf-8", contentTypeFor("data.json"))
	assert.Equal(t, "image/png", contentTypeFor("icon.png"))
	assert.Equal(t, "image/jpeg", contentTypeFor("photo.jpg"))
	assert.Equal(t, "image/svg+xml", contentTypeFor("logo.svg"))
	assert.Equal(t, "text/plain; charset=utf-8", contentTypeFor("readme.txt"))
}

func TestRunServeMissingWikiDir(t *testing.T) {
	cfg := &Config{
		OutputDir: "/nonexistent/wiki",
		Port:      18080,
	}

	err := RunServe(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wiki directory not found")
}
