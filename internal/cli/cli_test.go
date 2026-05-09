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

func (m *mockProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		if m.completeFunc != nil {
			text, err := m.completeFunc(ctx, prompt)
			if err == nil && text != "" {
				ch <- text
			}
		} else {
			ch <- "mock answer"
		}
	}()
	return ch, nil
}

func (m *mockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, texts)
	}
	return make([][]float32, len(texts)), nil
}

func clearCodewikiEnv(t *testing.T) {
	t.Helper()
	envVars := []string{
		"CODEWIKI_API_KEY", "CODEWIKI_MODEL", "CODEWIKI_BASE_URL",
		"CODEWIKI_GEN_API_KEY", "CODEWIKI_GEN_MODEL", "CODEWIKI_GEN_BASE_URL",
		"CODEWIKI_EMB_API_KEY", "CODEWIKI_EMB_MODEL", "CODEWIKI_EMB_BASE_URL",
	}
	for _, k := range envVars {
		os.Unsetenv(k)
	}
}

func TestGenerateCommand(t *testing.T) {
	repoPath := filepath.Join("..", "..", "testdata", "repos", "python-basic")
	_, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		t.Skip("testdata not found, skipping integration test")
	}

	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, ".codewiki", "wiki")

	// Isolate from user's real LLM config to avoid real API calls
	oldHome := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	defer func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	clearCodewikiEnv(t)

	cfg := &Config{
		SourceDir:   repoPath,
		OutputDir:   outDir,
		Language:    "python",
		ProjectName: "python-basic",
	}

	err = RunGenerate(cfg)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(outDir, "00-overview.md"))
	assert.FileExists(t, filepath.Join(outDir, "api-reference.md"))
	assert.FileExists(t, filepath.Join(outDir, "02-architecture.md"))
	assert.FileExists(t, filepath.Join(outDir, "architecture.mmd"))
	assert.FileExists(t, filepath.Join(outDir, "class-diagram.mmd"))
	assert.FileExists(t, filepath.Join(outDir, "sequence-diagram.mmd"))

	overview, err := os.ReadFile(filepath.Join(outDir, "00-overview.md"))
	require.NoError(t, err)
	assert.Contains(t, string(overview), "python-basic")
	assert.Contains(t, string(overview), "models/user")

	arch, err := os.ReadFile(filepath.Join(outDir, "architecture.mmd"))
	require.NoError(t, err)
	assert.Contains(t, string(arch), "graph TD")
}

func TestGenerateCommandEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "wiki")

	// Isolate from user's real LLM config to avoid real API calls
	oldHome := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	defer func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	clearCodewikiEnv(t)

	cfg := &Config{
		SourceDir:   tmpDir,
		OutputDir:   outDir,
		Language:    "python",
		ProjectName: "empty",
	}

	err := RunGenerate(cfg)
	require.NoError(t, err)

	overview, err := os.ReadFile(filepath.Join(outDir, "00-overview.md"))
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
	assert.Contains(t, err.Error(), "walk source files")
}

func TestGenerateCommandMaxFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	outDir := filepath.Join(tmpDir, "wiki")
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "app"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "app", "main.py"), []byte("def main(): pass\n"), 0644))

	// Isolate from user's real LLM config to avoid real API calls
	oldHome := os.Getenv("HOME")
	oldUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	defer func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("USERPROFILE", oldUserProfile)
	}()
	clearCodewikiEnv(t)

	cfg := &Config{
		SourceDir:       repoPath,
		OutputDir:       outDir,
		Language:        "python",
		ProjectName:     "maxfunc-test",
		MaxLLMFunctions: 0,
	}

	err := RunGenerate(cfg)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(outDir, "00-overview.md"))
}

func TestWikiHandler(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "overview.md"), []byte("# Test\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "architecture.mmd"), []byte("graph TD\n"), 0644))

	indexContent := `<html><body><a href="overview.md">Overview</a></body></html>`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644))

	handler := newServerHandler(tmpDir, nil)

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
	handler := newServerHandler(tmpDir, nil)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestWikiHandlerMermaidContentType(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "diagram.mmd"), []byte("graph TD\n"), 0644))

	handler := newServerHandler(tmpDir, nil)
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

	handler := newServerHandler(tmpDir, nil)
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

	handler := newServerHandler(tmpDir, nil)
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

	handler := newServerHandler(tmpDir, nil)
	req := httptest.NewRequest(http.MethodGet, "/subdir", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestWikiHandlerDirectoryTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := newServerHandler(tmpDir, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = "../../../etc/passwd"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 403, rr.Code)
}

func TestServeAskPageDisabled(t *testing.T) {
	handler := newServerHandler(t.TempDir(), nil)

	req := httptest.NewRequest(http.MethodGet, "/ask", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Body.String(), "问答终端")
	assert.Contains(t, rr.Body.String(), "--source")
}

func TestServeAskAPIDisabled(t *testing.T) {
	handler := newServerHandler(t.TempDir(), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 503, rr.Code)
	assert.Contains(t, rr.Body.String(), "RAG 引擎未启用")
}

func TestBuildIndexLink(t *testing.T) {
	link := buildIndexLink("overview.md", "项目概述", "了解项目整体情况")
	assert.Contains(t, link, `href="overview.md"`)
	assert.Contains(t, link, "<strong>项目概述</strong>")
	assert.Contains(t, link, "了解项目整体情况")
}

func TestHandleAskAPIBadRequest(t *testing.T) {
	// Engine available but bad JSON
	mock := &mockAskProvider{}
	store := vectorstore.New()
	store.Upsert("c1", []float32{1, 0, 0}, &chunker.Chunk{ID: "c1", Filename: "test.go", Name: "Test", Type: chunker.TypeFunction, Content: "func Test() {}"})
	engine := rag.NewEngine(mock, store)
	handler := newServerHandler(t.TempDir(), engine)

	// Invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, 400, rr.Code)
	assert.Contains(t, rr.Body.String(), "请求格式错误")

	// Empty question
	req2 := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":""}`))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	assert.Equal(t, 400, rr2.Code)
	assert.Contains(t, rr2.Body.String(), "问题不能为空")
}

func TestHandleAskAPISuccess(t *testing.T) {
	mock := &mockAskProvider{answer: "This is the answer."}
	store := vectorstore.New()
	store.Upsert("c1", []float32{1, 0, 0}, &chunker.Chunk{ID: "c1", Filename: "test.go", Name: "Test", Type: chunker.TypeFunction, Content: "func Test() {}"})
	engine := rag.NewEngine(mock, store)
	handler := newServerHandler(t.TempDir(), engine)

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"What is test?"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Body.String(), "This is the answer.")
	assert.Contains(t, rr.Body.String(), "test.go")
}

func TestHandleAskAPIWithHistory(t *testing.T) {
	mock := &mockAskProvider{answer: "Answer with history."}
	store := vectorstore.New()
	store.Upsert("c1", []float32{1, 0, 0}, &chunker.Chunk{ID: "c1", Filename: "test.go", Name: "Test", Type: chunker.TypeFunction, Content: "func Test() {}"})
	engine := rag.NewEngine(mock, store)
	handler := newServerHandler(t.TempDir(), engine)

	body := `{"question":"Follow-up?","history":[{"question":"First?","answer":"Yes."}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Body.String(), "Answer with history.")
	assert.Contains(t, mock.lastPrompt, "Conversation History", "prompt should include conversation history when history is provided")
}

// mockAskProvider implements llm.Provider for ask API tests.
type mockAskProvider struct {
	answer     string
	answerErr  error
	lastPrompt string
}

func (m *mockAskProvider) Complete(ctx context.Context, prompt string) (string, error) {
	m.lastPrompt = prompt
	if m.answerErr != nil {
		return "", m.answerErr
	}
	return m.answer, nil
}

func (m *mockAskProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	m.lastPrompt = prompt
	ch := make(chan string)
	go func() {
		defer close(ch)
		if m.answerErr != nil {
			return
		}
		ch <- m.answer
	}()
	return ch, nil
}

func (m *mockAskProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{1, 0, 0}
	}
	return vecs, nil
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

func TestServeIndexPageNoOverview(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "00-overview.md"), []byte("# Test\n\nSome preview text.\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "01-what-it-does.md"), []byte("# What\n"), 0644))

	rr := httptest.NewRecorder()
	serveIndexPage(rr, tmpDir)

	assert.Equal(t, 200, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Some preview text")
	assert.Contains(t, body, "项目概述")
	assert.Contains(t, body, "项目能做什么")
}

func TestServeIndexPageMinimal(t *testing.T) {
	tmpDir := t.TempDir()
	// No files at all
	rr := httptest.NewRecorder()
	serveIndexPage(rr, tmpDir)

	assert.Equal(t, 200, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "代码百科")
	assert.NotContains(t, body, `class="index-preview"><p>`)
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

func TestRunConfigInteractiveSeparateEmbedding(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user selecting openai for generation + separate embedding config
	input := strings.NewReader("openai\nsk-test\nhttps://api.openai.com/v1\ngpt-4o\nn\nollama\nhttp://localhost:11434\nnomic-embed-text\n")

	cfg := &Config{ConfigPath: cfgPath}
	err := runConfigInteractive(cfg, input)
	require.NoError(t, err)

	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", saved.Generation.Provider)
	assert.Equal(t, "sk-test", saved.Generation.APIKey)
	assert.Equal(t, "gpt-4o", saved.Generation.Model)
	assert.Equal(t, "ollama", saved.Embedding.Provider)
	assert.Equal(t, "nomic-embed-text", saved.Embedding.Model)
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

func TestRunConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user input: provider=ollama, base_url=http://localhost:11434, model=qwen, use_same=y
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		w.WriteString("ollama\nhttp://localhost:11434\nqwen\ny\n")
		w.Close()
	}()
	defer func() { os.Stdin = oldStdin }()

	cfg := &Config{ConfigPath: cfgPath}
	err := RunConfig(cfg)
	require.NoError(t, err)

	saved, err := llm.LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "ollama", saved.Generation.Provider)
	assert.Equal(t, "http://localhost:11434", saved.Generation.BaseURL)
	assert.Equal(t, "qwen", saved.Generation.Model)
}

func TestRunAskInvalidProvider(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: invalid\n"), 0644))

	cfg := &Config{
		SourceDir:  tmpDir,
		ConfigPath: cfgPath,
	}
	err := RunAsk(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create LLM provider")
}

func TestInitRAGEngine(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.py"), []byte("def main(): pass\n"), 0644))

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: ollama\nbase_url: http://localhost:11434\nmodel: qwen:14b\n"), 0644))

	_, err := initRAGEngine(repoDir, "python", cfgPath)
	// May fail at various stages (Ollama not reachable, SQLite path issues).
	// We just want to exercise the code path.
	if err != nil {
		assert.True(t,
			strings.Contains(err.Error(), "create provider") ||
				strings.Contains(err.Error(), "open vector store") ||
				strings.Contains(err.Error(), "parse directory"),
			"unexpected error: %v", err)
	}
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
