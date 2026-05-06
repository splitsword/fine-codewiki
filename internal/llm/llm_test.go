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
	assert.Equal(t, 60, cfg.Timeout)                       // default
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
