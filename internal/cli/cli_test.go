package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/rag"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	completeFunc func(ctx context.Context, prompt string) (string, error)
	embedFunc    func(ctx context.Context, texts []string) ([][]float32, error)
}

func (m *mockProvider) Complete(ctx context.Context, prompt string) (string, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, prompt)
	}
	return "mock answer", nil
}

func (m *mockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, texts)
	}
	return make([][]float32, len(texts)), nil
}

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
	assert.FileExists(t, filepath.Join(outDir, "sequence-diagram.mmd"))

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
	assert.Contains(t, string(overview), "未在项目中找到模块")
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
	assert.Contains(t, rr.Body.String(), "<h1>Test</h1>")
	assert.Contains(t, rr.Body.String(), `<nav class="sidebar">`)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
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
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), `<div class="mermaid">`)
	assert.Contains(t, rr.Body.String(), "graph TD")
}

func TestWikiHandlerMermaidDiagramPage(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "architecture.mmd"), []byte("graph TD\nA-->B\n"), 0644))

	handler := newWikiHandler(tmpDir)
	req := httptest.NewRequest(http.MethodGet, "/architecture.mmd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, `<div class="mermaid">`)
	assert.Contains(t, body, "A-->B")
	assert.Contains(t, body, `<nav class="sidebar">`)
}

func TestWikiHandlerNavigationItems(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "overview.md"), []byte("# Overview\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "api.md"), []byte("# API\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "diagram.mmd"), []byte("graph TD\n"), 0644))

	handler := newWikiHandler(tmpDir)
	req := httptest.NewRequest(http.MethodGet, "/overview.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	assert.Contains(t, body, `href="api.md"`)
	assert.Contains(t, body, `href="diagram.mmd"`)
	assert.Contains(t, body, `href="overview.md" class="active"`)
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
	assert.Contains(t, err.Error(), "Wiki 目录未找到")
}

func TestRunConfigInteractive(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user input: provider=openai, api_key=sk-test, base_url=https://api.openai.com/v1, model=gpt-4o, use_same=y
	input := strings.NewReader("openai\nsk-test\nhttps://api.openai.com/v1\ngpt-4o\ny\n")

	cfg := &Config{ConfigPath: cfgPath}
	err := runConfigInteractive(cfg, input)
	require.NoError(t, err)

	// Verify saved config
	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", saved.Generation.Provider)
	assert.Equal(t, "sk-test", saved.Generation.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", saved.Generation.BaseURL)
	assert.Equal(t, "gpt-4o", saved.Generation.Model)
	// Embedding should be a copy of generation
	assert.Equal(t, saved.Generation, saved.Embedding)
}

func TestRunConfigInteractiveDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Write an existing config (old format, backward compat)
	existing := &llm.Config{
		Provider: "ollama",
		BaseURL:  "http://localhost:11434",
		Model:    "qwen:14b",
	}
	require.NoError(t, llm.SaveConfig(existing, cfgPath))

	// Simulate user pressing Enter for all prompts (accepting defaults) + use_same=y
	input := strings.NewReader("\n\n\n\ny\n")

	cfg := &Config{ConfigPath: cfgPath}
	err := runConfigInteractive(cfg, input)
	require.NoError(t, err)

	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "ollama", saved.Generation.Provider)
	assert.Equal(t, "http://localhost:11434", saved.Generation.BaseURL)
	assert.Equal(t, "qwen:14b", saved.Generation.Model)
}

func TestRunConfigInteractiveOllamaNoKey(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user selecting ollama (no API key prompt) + use_same=y
	input := strings.NewReader("ollama\nhttp://localhost:11434\nqwen:32b\ny\n")

	cfg := &Config{ConfigPath: cfgPath}
	err := runConfigInteractive(cfg, input)
	require.NoError(t, err)

	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "ollama", saved.Generation.Provider)
	assert.Equal(t, "", saved.Generation.APIKey)
	assert.Equal(t, "qwen:32b", saved.Generation.Model)
}

func TestMaskKey(t *testing.T) {
	assert.Equal(t, "(none)", maskKey(""))
	assert.Equal(t, "****", maskKey("short"))
	assert.Equal(t, "abcd****wxyz", maskKey("abcdefghijklwxyz"))
}

func TestReadLine(t *testing.T) {
	scanner := bufio.NewScanner(bytes.NewReader([]byte("hello\n")))
	assert.Equal(t, "hello", readLine(scanner))
}

func TestMarkdownToHTMLHeaders(t *testing.T) {
	src := []byte("# H1\n## H2\n### H3\n#### H4\n##### H5\n###### H6\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<h1>H1</h1>")
	assert.Contains(t, string(html), "<h2>H2</h2>")
	assert.Contains(t, string(html), "<h3>H3</h3>")
	assert.Contains(t, string(html), "<h4>H4</h4>")
	assert.Contains(t, string(html), "<h5>H5</h5>")
	assert.Contains(t, string(html), "<h6>H6</h6>")
}

func TestMarkdownToHTMLCodeBlock(t *testing.T) {
	src := []byte("```go\nfmt.Println(\"hello\")\n```\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<pre><code>")
	assert.Contains(t, string(html), "fmt.Println")
}

func TestMarkdownToHTMLMermaidBlock(t *testing.T) {
	src := []byte("```mermaid\ngraph TD\n```\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), `<div class="mermaid">`)
	assert.Contains(t, string(html), "graph TD")
}

func TestMarkdownToHTMLUnorderedList(t *testing.T) {
	src := []byte("- Item 1\n- Item 2\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<ul>")
	assert.Contains(t, string(html), "<li>Item 1</li>")
	assert.Contains(t, string(html), "<li>Item 2</li>")
	assert.Contains(t, string(html), "</ul>")
}

func TestMarkdownToHTMLOrderedList(t *testing.T) {
	src := []byte("1. First\n2. Second\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<ol>")
	assert.Contains(t, string(html), "<li>First</li>")
	assert.Contains(t, string(html), "<li>Second</li>")
	assert.Contains(t, string(html), "</ol>")
}

func TestMarkdownToHTMLBlockquote(t *testing.T) {
	src := []byte("> Quote text\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<blockquote>")
	assert.Contains(t, string(html), "Quote text")
}

func TestMarkdownToHTMLHorizontalRule(t *testing.T) {
	src := []byte("---\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<hr>")
}

func TestMarkdownToHTMLTable(t *testing.T) {
	src := []byte("| A | B |\n|---|---|\n| 1 | 2 |\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<table>")
	assert.Contains(t, string(html), "<th>A</th>")
	assert.Contains(t, string(html), "<th>B</th>")
	assert.Contains(t, string(html), "<td>1</td>")
	assert.Contains(t, string(html), "<td>2</td>")
}

func TestMarkdownToHTMLParagraph(t *testing.T) {
	src := []byte("Hello world\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<p>Hello world</p>")
}

func TestMarkdownToHTMLInlineBold(t *testing.T) {
	src := []byte("**bold**\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<strong>bold</strong>")
}

func TestMarkdownToHTMLInlineItalic(t *testing.T) {
	src := []byte("*italic*\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<em>italic</em>")
}

func TestMarkdownToHTMLInlineLink(t *testing.T) {
	src := []byte("[link](https://example.com)\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), `<a href="https://example.com">link</a>`)
}

func TestMarkdownToHTMLInlineCode(t *testing.T) {
	src := []byte("`code`\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<code>code</code>")
}

func TestMarkdownToHTMLEmpty(t *testing.T) {
	html := markdownToHTML([]byte(""))
	assert.Contains(t, string(html), "<body>")
}

func TestRunConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user input: provider=ollama, base_url=http://localhost:11434, model=qwen, use_same=y
	input := strings.NewReader("ollama\nhttp://localhost:11434\nqwen\ny\n")

	cfg := &Config{ConfigPath: cfgPath}
	err := runConfigInteractive(cfg, input)
	require.NoError(t, err)

	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "ollama", saved.Generation.Provider)
	assert.Equal(t, "http://localhost:11434", saved.Generation.BaseURL)
	assert.Equal(t, "qwen", saved.Generation.Model)
}

func TestMarkdownToHTMLCombined(t *testing.T) {
	src := []byte("# Title\n\nParagraph with **bold** and *italic*.\n\n```\ncode block\n```\n\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<h1>Title</h1>")
	assert.Contains(t, string(html), "<strong>bold</strong>")
	assert.Contains(t, string(html), "<em>italic</em>")
	assert.Contains(t, string(html), "<pre><code>")
}

func TestRenderInlineLinkBoldItalicCode(t *testing.T) {
	result := renderInline("[a](http://b) **c** *d* `e`")
	assert.Contains(t, result, `<a href="http://b">a</a>`)
	assert.Contains(t, result, "<strong>c</strong>")
	assert.Contains(t, result, "<em>d</em>")
	assert.Contains(t, result, "<code>e</code>")
}

func TestHTMLEscape(t *testing.T) {
	assert.Equal(t, "&lt;div&gt;", htmlEscape("<div>"))
	assert.Equal(t, "&amp;", htmlEscape("&"))
	assert.Equal(t, "&quot;", htmlEscape("\""))
}

func TestOrderedListMatch(t *testing.T) {
	assert.True(t, orderedListMatch("1. item"))
	assert.True(t, orderedListMatch("99. item"))
	assert.False(t, orderedListMatch("abc"))
	assert.False(t, orderedListMatch("1 item"))
}

func TestOrderedListItem(t *testing.T) {
	assert.Equal(t, "item", orderedListItem("1. item"))
	assert.Equal(t, "item", orderedListItem("99. item"))
}

func TestSplitTableCells(t *testing.T) {
	cells := splitTableCells("| a | b |")
	require.Len(t, cells, 2)
	assert.Equal(t, " a ", cells[0])
	assert.Equal(t, " b ", cells[1])
}

func TestIsTableSeparator(t *testing.T) {
	assert.True(t, isTableSeparator("|---|---|"))
	assert.True(t, isTableSeparator("| :-- | --: |"))
	assert.False(t, isTableSeparator("| a | b |"))
}

func TestMarkdownToHTMLMultipleParagraphs(t *testing.T) {
	src := []byte("First para.\n\nSecond para.\n")
	html := markdownToHTML(src)
	assert.Contains(t, string(html), "<p>First para.</p>")
	assert.Contains(t, string(html), "<p>Second para.</p>")
}

func TestRunSingleAsk(t *testing.T) {
	provider := &mockProvider{
		embedFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			vecs := make([][]float32, len(texts))
			for i := range vecs {
				vecs[i] = []float32{1, 0, 0}
			}
			return vecs, nil
		},
	}
	store := vectorstore.New()
	store.Upsert("test-id", []float32{1, 0, 0}, &chunker.Chunk{
		ID:       "test-id",
		Content:  "test content",
		Filename: "test.go",
		Name:     "TestFunc",
		Type:     chunker.TypeFunction,
	})
	engine := rag.NewEngine(provider, store)

	err := runSingleAsk(engine, "What is this?")
	require.NoError(t, err)
}

func TestRunSingleAskWithSources(t *testing.T) {
	provider := &mockProvider{
		completeFunc: func(ctx context.Context, prompt string) (string, error) {
			return "It is a test.", nil
		},
		embedFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			vecs := make([][]float32, len(texts))
			for i := range vecs {
				vecs[i] = []float32{1, 0, 0}
			}
			return vecs, nil
		},
	}
	store := vectorstore.New()
	store.Upsert("test-id", []float32{1, 0, 0}, &chunker.Chunk{
		ID:       "test-id",
		Content:  "test content",
		Filename: "test.go",
		Name:     "TestFunc",
		Type:     chunker.TypeFunction,
	})
	engine := rag.NewEngine(provider, store)

	err := runSingleAsk(engine, "What is this?")
	require.NoError(t, err)
}

func TestRunAskInvalidProvider(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: unknown\n"), 0644))

	cfg := &Config{
		SourceDir:  tmpDir,
		Question:   "test",
		ConfigPath: cfgPath,
	}
	err := RunAsk(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create LLM provider")
}

func TestRunAskWithStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/embed" {
			resp := map[string]any{"embeddings": [][]float32{{0.1, 0.2, 0.3}}}
			json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/api/generate" {
			resp := map[string]any{"response": "It is a test."}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: ollama\nbase_url: "+server.URL+"\nmodel: qwen\n"), 0644))

	// Pre-populate vector store
	vectorPath := filepath.Join(tmpDir, ".codewiki", "vectors.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(vectorPath), 0755))
	store, err := vectorstore.NewSQLite(vectorPath)
	require.NoError(t, err)
	defer store.Close()
	store.Upsert("chunk-1", []float32{0.1, 0.2, 0.3}, &chunker.Chunk{
		ID: "chunk-1", Content: "test", Filename: "test.go", Name: "Test", Type: chunker.TypeFunction,
	})

	cfg := &Config{
		SourceDir:  tmpDir,
		Question:   "What is test?",
		ConfigPath: cfgPath,
	}
	err = RunAsk(cfg)
	require.NoError(t, err)
}

func TestRunInteractiveAsk(t *testing.T) {
	provider := &mockProvider{
		embedFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			vecs := make([][]float32, len(texts))
			for i := range vecs {
				vecs[i] = []float32{1, 0, 0}
			}
			return vecs, nil
		},
	}
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Content: "test", Filename: "test.go", Name: "Test", Type: chunker.TypeFunction,
	})
	engine := rag.NewEngine(provider, store)

	// Redirect stdin
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("hello\nexit\n")
		w.Close()
	}()

	err := runInteractiveAsk(engine)
	require.NoError(t, err)
}

func TestRunServeDefaultOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".codewiki", "wiki")
	require.NoError(t, os.MkdirAll(wikiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "overview.md"), []byte("# Test\n"), 0644))

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &Config{Port: 18083}
	go func() {
		_ = RunServe(cfg)
	}()
	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://localhost:18083/")
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestRunServeStarts(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "overview.md"), []byte("# Test\n"), 0644))

	cfg := &Config{
		OutputDir: tmpDir,
		Port:      18082,
	}

	// Start server in background
	go func() {
		_ = RunServe(cfg)
	}()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://localhost:18082/overview.md")
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}
