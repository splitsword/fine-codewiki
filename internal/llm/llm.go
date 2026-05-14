package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Provider is the unified interface for LLM operations.
type Provider interface {
	// Complete sends a prompt and returns the generated text.
	Complete(ctx context.Context, prompt string) (string, error)
	// CompleteStream sends a prompt and returns a channel of text tokens.
	CompleteStream(ctx context.Context, prompt string) (<-chan string, error)
	// Embed returns vector embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Config holds LLM provider configuration for a single model.
type Config struct {
	Provider   string `yaml:"provider"` // "openai" or "ollama"
	APIKey     string `yaml:"api_key"`
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	MaxRetries int    `yaml:"max_retries"`
	Timeout    int    `yaml:"timeout"` // seconds
	Thinking   bool   `yaml:"thinking"` // 启用深度思考模式
}

// AppConfig holds separate LLM configurations for generation and embedding.
type AppConfig struct {
	Generation Config `yaml:"generation"`
	Embedding  Config `yaml:"embedding"`
}

// defaultConfig returns a default single-model config.
func defaultConfig() Config {
	return Config{
		Provider:   "ollama",
		BaseURL:    "http://localhost:11434",
		Model:      "qwen:14b",
		MaxRetries: 3,
		Timeout:    120,
		Thinking:   true,
	}
}

// LoadConfig reads configuration from a YAML file.
// If the file does not exist, returns default config.
// Environment variables CODEWIKI_API_KEY and CODEWIKI_MODEL override file values.
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	// Environment overrides
	if v := os.Getenv("CODEWIKI_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("CODEWIKI_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("CODEWIKI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("CODEWIKI_THINKING"); v != "" {
		cfg.Thinking = v == "1" || v == "true"
	}

	return &cfg, nil
}

// LoadAppConfig reads the dual-model configuration from a YAML file.
// Supports both new format (generation + embedding blocks) and old format
// (single flat config, which is copied to both generation and embedding).
// Environment variables CODEWIKI_GEN_* / CODEWIKI_EMB_* override file values,
// with fallback to CODEWIKI_API_KEY / CODEWIKI_MODEL / CODEWIKI_BASE_URL.
func LoadAppConfig(path string) (*AppConfig, error) {
	appCfg := &AppConfig{
		Generation: defaultConfig(),
		Embedding:  defaultConfig(),
	}

	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err == nil {
		// Check if file contains new format markers
		isNewFormat := strings.Contains(string(data), "generation:") || strings.Contains(string(data), "embedding:")
		if isNewFormat {
			if err := yaml.Unmarshal(data, appCfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
		} else {
			oldCfg := defaultConfig()
			if err := yaml.Unmarshal(data, &oldCfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
			appCfg.Generation = oldCfg
			appCfg.Embedding = oldCfg
		}
	}

	// Environment overrides for generation
	if v := os.Getenv("CODEWIKI_GEN_API_KEY"); v != "" {
		appCfg.Generation.APIKey = v
	} else if v := os.Getenv("CODEWIKI_API_KEY"); v != "" {
		appCfg.Generation.APIKey = v
	}
	if v := os.Getenv("CODEWIKI_GEN_MODEL"); v != "" {
		appCfg.Generation.Model = v
	} else if v := os.Getenv("CODEWIKI_MODEL"); v != "" {
		appCfg.Generation.Model = v
	}
	if v := os.Getenv("CODEWIKI_GEN_BASE_URL"); v != "" {
		appCfg.Generation.BaseURL = v
	} else if v := os.Getenv("CODEWIKI_BASE_URL"); v != "" {
		appCfg.Generation.BaseURL = v
	}
	if v := os.Getenv("CODEWIKI_THINKING"); v != "" {
		appCfg.Generation.Thinking = v == "1" || v == "true"
	}

	// Environment overrides for embedding
	if v := os.Getenv("CODEWIKI_EMB_API_KEY"); v != "" {
		appCfg.Embedding.APIKey = v
	} else if v := os.Getenv("CODEWIKI_API_KEY"); v != "" {
		appCfg.Embedding.APIKey = v
	}
	if v := os.Getenv("CODEWIKI_EMB_MODEL"); v != "" {
		appCfg.Embedding.Model = v
	} else if v := os.Getenv("CODEWIKI_MODEL"); v != "" {
		appCfg.Embedding.Model = v
	}
	if v := os.Getenv("CODEWIKI_EMB_BASE_URL"); v != "" {
		appCfg.Embedding.BaseURL = v
	} else if v := os.Getenv("CODEWIKI_BASE_URL"); v != "" {
		appCfg.Embedding.BaseURL = v
	}

	return appCfg, nil
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codewiki/config.yaml"
	}
	return filepath.Join(home, ".codewiki", "config.yaml")
}

// SaveConfig writes configuration to a YAML file.
func SaveConfig(cfg *Config, path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// SaveAppConfig writes the dual-model configuration to a YAML file.
func SaveAppConfig(cfg *AppConfig, path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// NewProvider creates a Provider based on configuration.
func NewProvider(cfg *Config) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "openai":
		return newOpenAIProvider(cfg), nil
	case "ollama":
		return newOllamaProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

// NewGenerationProvider creates a Provider for document generation tasks.
func NewGenerationProvider(cfg *AppConfig) (Provider, error) {
	return NewProvider(&cfg.Generation)
}

// NewEmbeddingProvider creates a Provider for embedding / RAG tasks.
func NewEmbeddingProvider(cfg *AppConfig) (Provider, error) {
	return NewProvider(&cfg.Embedding)
}

// ---------- OpenAI Provider ----------

// OpenAIProvider calls OpenAI-compatible APIs.
type OpenAIProvider struct {
	BaseURL      string
	APIKey       string
	Model        string
	MaxRetries   int
	Thinking     bool
	HTTPClient   *http.Client
	StreamClient *http.Client
}

func newOpenAIProvider(cfg *Config) *OpenAIProvider {
	// HTTPClient carries no Timeout — all deadline enforcement is via context.
	// Setting http.Client.Timeout would conflict with context deadlines and
	// prematurely kill long-running requests (especially with thinking mode).
	return &OpenAIProvider{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		MaxRetries:   cfg.MaxRetries,
		Thinking:     cfg.Thinking,
		HTTPClient:   &http.Client{},
		StreamClient: &http.Client{},
	}
}

// Complete implements Provider.
func (p *OpenAIProvider) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	p.injectThinking(reqBody)

	var resp openAIChatResponse
	err := p.post(ctx, "/chat/completions", reqBody, &resp)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return resp.Choices[0].Message.Content, nil
}

// CompleteStream implements Provider.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	reqBody := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": true,
	}
	p.injectThinking(reqBody)

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := p.StreamClient
	if client == nil {
		client = p.HTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				ch <- chunk.Choices[0].Delta.Content
			}
		}
	}()

	return ch, nil
}

// Embed implements Provider.
func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"model": p.Model,
		"input": texts,
	}

	var resp openAIEmbedResponse
	err := p.post(ctx, "/embeddings", reqBody, &resp)
	if err != nil {
		return nil, err
	}

	vectors := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

func (p *OpenAIProvider) post(ctx context.Context, path string, reqBody, respBody any) error {
	url := strings.TrimSuffix(p.BaseURL, "/") + path

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	var lastErr error
	maxRetries := p.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*3) * time.Second
			if isTimeout(lastErr) {
				backoff = time.Duration(attempt*5) * time.Second
			}
			time.Sleep(backoff)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.APIKey)

		resp, err := p.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			if isTimeout(err) {
				continue
			}
			return err
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("rate limited")
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				if sec, _ := strconv.Atoi(retryAfter); sec > 0 {
					time.Sleep(time.Duration(sec) * time.Second)
				}
			}
			continue
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
		}

		if err := json.Unmarshal(data, respBody); err != nil {
			return fmt.Errorf("解析响应失败: %w (原始响应: %s)", err, string(data))
		}
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("max retries exceeded: %w", lastErr)
	}
	return fmt.Errorf("max retries exceeded")
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
}

// injectThinking adds thinking/reasoning parameters to the request body.
func (p *OpenAIProvider) injectThinking(reqBody map[string]any) {
	if !p.Thinking {
		return
	}
	// DeepSeek-native thinking format
	reqBody["thinking"] = map[string]string{"type": "enabled"}
	// OpenAI-style reasoning effort (some gateways/proxies prefer this)
	reqBody["reasoning_effort"] = "high"
}

// ---------- Ollama Provider ----------

// OllamaProvider calls local Ollama API.
type OllamaProvider struct {
	BaseURL      string
	Model        string
	HTTPClient   *http.Client
	StreamClient *http.Client
}

func newOllamaProvider(cfg *Config) *OllamaProvider {
	return &OllamaProvider{
		BaseURL:      cfg.BaseURL,
		Model:        cfg.Model,
		HTTPClient:   &http.Client{},
		StreamClient: &http.Client{},
	}
}

// Complete implements Provider.
func (p *OllamaProvider) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]any{
		"model":  p.Model,
		"prompt": prompt,
		"stream": false,
	}

	var resp ollamaGenerateResponse
	if err := p.post(ctx, "/api/generate", reqBody, &resp); err != nil {
		return "", err
	}
	return resp.Response, nil
}

// CompleteStream implements Provider.
func (p *OllamaProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	reqBody := map[string]any{
		"model":  p.Model,
		"prompt": prompt,
		"stream": true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(p.BaseURL, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.StreamClient
	if client == nil {
		client = p.HTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if isConnectionError(err) {
			return nil, fmt.Errorf("无法连接到 Ollama 服务（%s）。请确认 Ollama 已启动，或检查 base_url 配置是否正确", p.BaseURL)
		}
		return nil, err
	}

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(data))
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var chunk ollamaStreamChunk
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				continue
			}
			if chunk.Response != "" {
				ch <- chunk.Response
			}
			if chunk.Done {
				break
			}
		}
	}()

	return ch, nil
}

// Embed implements Provider.
func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"model": p.Model,
		"input": texts,
	}

	var resp ollamaEmbedResponse
	if err := p.post(ctx, "/api/embed", reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Embeddings, nil
}

func (p *OllamaProvider) post(ctx context.Context, path string, reqBody, respBody any) error {
	url := strings.TrimSuffix(p.BaseURL, "/") + path

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		if isConnectionError(err) {
			return fmt.Errorf("无法连接到 Ollama 服务（%s）。请确认 Ollama 已启动，或检查 base_url 配置是否正确", p.BaseURL)
		}
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(data))
	}

	return json.Unmarshal(data, respBody)
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type ollamaStreamChunk struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// ---------- Utilities ----------

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no connection could be made") ||
		strings.Contains(msg, "connectex") && strings.Contains(msg, "dial tcp")
}
