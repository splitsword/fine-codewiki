package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Mock Server Tests ----------

func TestOpenAIProviderComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var reqBody map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "gpt-4o", reqBody["model"])

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from mock"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := p.Complete(context.Background(), "Say hello")
	require.NoError(t, err)
	assert.Equal(t, "Hello from mock", resp)
}

func TestOpenAIProviderEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/embeddings", r.URL.Path)

		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}},
				{"embedding": []float32{0.4, 0.5, 0.6}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "text-embedding-3-small",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	vectors, err := p.Embed(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	require.Len(t, vectors, 2)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, vectors[0])
	assert.Equal(t, []float32{0.4, 0.5, 0.6}, vectors[1])
}

func TestOpenAIProviderRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "0")
			fmt.Fprint(w, `{"error": {"message": "rate limited"}}`)
			return
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Success after retry"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 3,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := p.Complete(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "Success after retry", resp)
	assert.Equal(t, 3, attempts)
}

func TestOpenAIProviderNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"choices": []map[string]any{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 0,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices in response")
}

func TestOpenAIProviderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 0,
		HTTPClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

func TestOllamaProviderComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/generate", r.URL.Path)

		var reqBody map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "qwen:14b", reqBody["model"])
		assert.Equal(t, "Say hello", reqBody["prompt"])

		resp := map[string]any{"response": "Hello from Ollama"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "qwen:14b",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := p.Complete(context.Background(), "Say hello")
	require.NoError(t, err)
	assert.Equal(t, "Hello from Ollama", resp)
}

func TestOllamaProviderEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/embed", r.URL.Path)

		resp := map[string]any{
			"embeddings": [][]float32{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "nomic-embed-text",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	vectors, err := p.Embed(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	require.Len(t, vectors, 2)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, vectors[0])
}

// ---------- Config Tests ----------

func TestLoadConfigFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `
provider: openai
api_key: sk-test123
base_url: https://api.openai.com/v1
model: gpt-4o
max_retries: 5
timeout: 30
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "sk-test123", cfg.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", cfg.BaseURL)
	assert.Equal(t, "gpt-4o", cfg.Model)
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, 30, cfg.Timeout)
}

func TestLoadConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: ollama\nmodel: qwen:14b\n"), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.Provider)
	assert.Equal(t, "qwen:14b", cfg.Model)
	assert.Equal(t, "http://localhost:11434", cfg.BaseURL) // default
	assert.Equal(t, 3, cfg.MaxRetries)                     // default
	assert.Equal(t, 120, cfg.Timeout)                      // default
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.Provider) // default
	assert.Equal(t, "http://localhost:11434", cfg.BaseURL)
}

func TestNewProviderFromConfig(t *testing.T) {
	openaiCfg := &Config{Provider: "openai", APIKey: "key", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"}
	p, err := NewProvider(openaiCfg)
	require.NoError(t, err)
	_, ok := p.(*OpenAIProvider)
	assert.True(t, ok)

	ollamaCfg := &Config{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "qwen"}
	p, err = NewProvider(ollamaCfg)
	require.NoError(t, err)
	_, ok = p.(*OllamaProvider)
	assert.True(t, ok)

	_, err = NewProvider(&Config{Provider: "unknown"})
	require.Error(t, err)
}

func TestConfigEnvOverride(t *testing.T) {
	os.Setenv("CODEWIKI_API_KEY", "env-key")
	os.Setenv("CODEWIKI_MODEL", "env-model")
	defer func() {
		os.Unsetenv("CODEWIKI_API_KEY")
		os.Unsetenv("CODEWIKI_MODEL")
	}()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: openai\napi_key: file-key\nmodel: file-model\n"), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "env-key", cfg.APIKey)
	assert.Equal(t, "env-model", cfg.Model)
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		Provider:   "openai",
		APIKey:     "sk-test",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "gpt-4o",
		MaxRetries: 5,
		Timeout:    30,
	}

	err := SaveConfig(cfg, cfgPath)
	require.NoError(t, err)

	loaded, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", loaded.Provider)
	assert.Equal(t, "sk-test", loaded.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", loaded.BaseURL)
	assert.Equal(t, "gpt-4o", loaded.Model)
	assert.Equal(t, 5, loaded.MaxRetries)
	assert.Equal(t, 30, loaded.Timeout)
}

func TestSaveConfigCreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nested", "dir", "config.yaml")

	cfg := &Config{Provider: "ollama", Model: "qwen"}
	err := SaveConfig(cfg, cfgPath)
	require.NoError(t, err)

	_, err = os.Stat(cfgPath)
	assert.NoError(t, err)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.Contains(t, path, ".codewiki")
	assert.Contains(t, path, "config.yaml")
}

func TestLoadConfigParseError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "bad.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("{invalid yaml: ["), 0644))

	_, err := LoadConfig(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse config")
}

func TestLoadConfigBaseURLEnv(t *testing.T) {
	os.Setenv("CODEWIKI_BASE_URL", "https://custom.example.com/v1")
	defer os.Unsetenv("CODEWIKI_BASE_URL")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: openai\n"), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.example.com/v1", cfg.BaseURL)
}

func TestSaveConfigEmptyPath(t *testing.T) {
	// This will try to write to the default path (home dir).
	// We can't easily test this without side effects, so we test
	// that passing a valid path works instead.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "sub", "config.yaml")
	cfg := &Config{Provider: "ollama", Model: "qwen"}
	err := SaveConfig(cfg, cfgPath)
	require.NoError(t, err)

	loaded, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "qwen", loaded.Model)
}

func TestOpenAIProviderAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error": {"message": "bad request"}}`)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 0,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error 400")
}

func TestOllamaProviderEmbedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": "load failed"}`)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "nomic-embed-text",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Embed(context.Background(), []string{"hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ollama error 500")
}

func TestIsTimeout(t *testing.T) {
	assert.False(t, isTimeout(nil))
	assert.False(t, isTimeout(fmt.Errorf("some random error")))
	assert.True(t, isTimeout(fmt.Errorf("request timeout")))
	assert.True(t, isTimeout(fmt.Errorf("context deadline exceeded")))
}

func TestOllamaProviderCompleteInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{invalid json`)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "qwen:14b",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
}

func TestOllamaProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": "model not found"}`)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "qwen:14b",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ollama error 500")
}

// ---------- AppConfig Tests ----------

func TestLoadAppConfigNewFormat(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `
generation:
  provider: openai
  api_key: sk-gen
  base_url: https://api.openai.com/v1
  model: gpt-4o
  max_retries: 3
  timeout: 60
embedding:
  provider: ollama
  api_key: ""
  base_url: http://localhost:11434
  model: nomic-embed-text
  max_retries: 3
  timeout: 60
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	cfg, err := LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.Generation.Provider)
	assert.Equal(t, "sk-gen", cfg.Generation.APIKey)
	assert.Equal(t, "gpt-4o", cfg.Generation.Model)
	assert.Equal(t, "ollama", cfg.Embedding.Provider)
	assert.Equal(t, "nomic-embed-text", cfg.Embedding.Model)
}

func TestLoadAppConfigBackwardCompat(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `
provider: openai
api_key: sk-old
base_url: https://api.openai.com/v1
model: gpt-4o
max_retries: 5
timeout: 30
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	cfg, err := LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.Generation.Provider)
	assert.Equal(t, "sk-old", cfg.Generation.APIKey)
	assert.Equal(t, "gpt-4o", cfg.Generation.Model)
	assert.Equal(t, 5, cfg.Generation.MaxRetries)
	assert.Equal(t, 30, cfg.Generation.Timeout)
	// Embedding should be a copy of generation
	assert.Equal(t, cfg.Generation, cfg.Embedding)
}

func TestLoadAppConfigEnvOverrideSeparate(t *testing.T) {
	os.Setenv("CODEWIKI_GEN_MODEL", "gpt-4o")
	os.Setenv("CODEWIKI_EMB_MODEL", "nomic-embed-text")
	defer func() {
		os.Unsetenv("CODEWIKI_GEN_MODEL")
		os.Unsetenv("CODEWIKI_EMB_MODEL")
	}()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: ollama\nmodel: qwen:14b\n"), 0644))

	cfg, err := LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", cfg.Generation.Model)
	assert.Equal(t, "nomic-embed-text", cfg.Embedding.Model)
}

func TestLoadAppConfigEnvFallback(t *testing.T) {
	os.Setenv("CODEWIKI_API_KEY", "fallback-key")
	defer os.Unsetenv("CODEWIKI_API_KEY")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("provider: ollama\n"), 0644))

	cfg, err := LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "fallback-key", cfg.Generation.APIKey)
	assert.Equal(t, "fallback-key", cfg.Embedding.APIKey)
}

func TestSaveAppConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &AppConfig{
		Generation: Config{Provider: "openai", APIKey: "sk-gen", Model: "gpt-4o"},
		Embedding:  Config{Provider: "ollama", Model: "nomic-embed-text"},
	}

	err := SaveAppConfig(cfg, cfgPath)
	require.NoError(t, err)

	loaded, err := LoadAppConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "openai", loaded.Generation.Provider)
	assert.Equal(t, "sk-gen", loaded.Generation.APIKey)
	assert.Equal(t, "gpt-4o", loaded.Generation.Model)
	assert.Equal(t, "ollama", loaded.Embedding.Provider)
	assert.Equal(t, "nomic-embed-text", loaded.Embedding.Model)
}

func TestNewGenerationProvider(t *testing.T) {
	appCfg := &AppConfig{
		Generation: Config{Provider: "openai", APIKey: "k", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
	}
	p, err := NewGenerationProvider(appCfg)
	require.NoError(t, err)
	_, ok := p.(*OpenAIProvider)
	assert.True(t, ok)
}

func TestNewEmbeddingProvider(t *testing.T) {
	appCfg := &AppConfig{
		Embedding: Config{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "nomic-embed-text"},
	}
	p, err := NewEmbeddingProvider(appCfg)
	require.NoError(t, err)
	_, ok := p.(*OllamaProvider)
	assert.True(t, ok)
}

func TestOllamaProviderConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "qwen:14b",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无法连接到 Ollama 服务")
}

func TestOpenAIProviderInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{invalid json}")
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 0,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.Complete(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析响应失败")
	assert.Contains(t, err.Error(), "原始响应")
}

func TestOpenAIProviderCompleteStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}")
		fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}")
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	ch, err := p.CompleteStream(context.Background(), "Say hello")
	require.NoError(t, err)

	var tokens []string
	for token := range ch {
		tokens = append(tokens, token)
	}
	assert.Equal(t, []string{"Hello", " world"}, tokens)
}

// TestOpenAIProviderCompleteStream429Retry verifies A6: the streaming path
// retries on 429 with Retry-After backoff instead of failing the whole batch.
func TestOpenAIProviderCompleteStream429Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}")
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "gpt-4o",
		MaxRetries: 3,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	ch, err := p.CompleteStream(context.Background(), "test")
	require.NoError(t, err)
	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	assert.Equal(t, []string{"OK"}, tokens)
	assert.Equal(t, 3, attempts, "should retry until success")
}

func TestOllamaProviderCompleteStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/generate", r.URL.Path)

		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"response":"Hello","done":false}`)
		fmt.Fprintln(w, `{"response":" world","done":false}`)
		fmt.Fprintln(w, `{"response":"","done":true}`)
	}))
	defer server.Close()

	p := &OllamaProvider{
		BaseURL:    server.URL,
		Model:      "qwen:14b",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	ch, err := p.CompleteStream(context.Background(), "Say hello")
	require.NoError(t, err)

	var tokens []string
	for token := range ch {
		tokens = append(tokens, token)
	}
	assert.Equal(t, []string{"Hello", " world"}, tokens)
}

func TestOpenAIProviderCompleteStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid key"}`)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		BaseURL:    server.URL + "/v1",
		APIKey:     "bad-key",
		Model:      "gpt-4o",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.CompleteStream(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
