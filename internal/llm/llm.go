package llm

import (
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
	// Embed returns vector embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Config holds LLM provider configuration.
type Config struct {
	Provider  string `yaml:"provider"`  // "openai" or "ollama"
	APIKey    string `yaml:"api_key"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	MaxRetries int   `yaml:"max_retries"`
	Timeout   int    `yaml:"timeout"` // seconds
}

// LoadConfig reads configuration from a YAML file.
// If the file does not exist, returns default config.
// Environment variables CODEWIKI_API_KEY and CODEWIKI_MODEL override file values.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Provider:   "ollama",
		BaseURL:    "http://localhost:11434",
		Model:      "qwen:14b",
		MaxRetries: 3,
		Timeout:    60,
	}

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
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

	return cfg, nil
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codewiki/config.yaml"
	}
	return filepath.Join(home, ".codewiki", "config.yaml")
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

// ---------- OpenAI Provider ----------

// OpenAIProvider calls OpenAI-compatible APIs.
type OpenAIProvider struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxRetries int
	HTTPClient *http.Client
}

func newOpenAIProvider(cfg *Config) *OpenAIProvider {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &OpenAIProvider{
		BaseURL:    cfg.BaseURL,
		APIKey:     cfg.APIKey,
		Model:      cfg.Model,
		MaxRetries: cfg.MaxRetries,
		HTTPClient: &http.Client{Timeout: timeout},
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
			time.Sleep(time.Duration(attempt) * time.Second)
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

		return json.Unmarshal(data, respBody)
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

// ---------- Ollama Provider ----------

// OllamaProvider calls local Ollama API.
type OllamaProvider struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func newOllamaProvider(cfg *Config) *OllamaProvider {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &OllamaProvider{
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		HTTPClient: &http.Client{Timeout: timeout},
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

// ---------- Utilities ----------

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
