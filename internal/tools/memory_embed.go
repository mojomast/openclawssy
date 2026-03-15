package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/memory"
)

type openAICompatibleEmbedder struct {
	endpoint config.ProviderEndpointConfig
	apiKey   string
	model    string
}

func (e *openAICompatibleEmbedder) ModelID() string {
	return e.model
}

func (e *openAICompatibleEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("embedding input text is required")
	}
	body := map[string]any{
		"model": e.model,
		"input": text,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(strings.TrimSpace(e.endpoint.BaseURL), "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.endpoint.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 || len(payload.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding response missing vectors")
	}
	vec := make([]float32, 0, len(payload.Data[0].Embedding))
	for _, v := range payload.Data[0].Embedding {
		vec = append(vec, float32(v))
	}
	return vec, nil
}

func memoryEmbedderFromConfig(cfg config.Config) (memory.Embedder, error) {
	if !cfg.Memory.EmbeddingsEnabled {
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Memory.EmbeddingProvider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.Model.Provider))
	}
	model := strings.TrimSpace(cfg.Memory.EmbeddingModel)
	if model == "" {
		model = "text-embedding-3-small"
	}
	endpoint, err := resolveProviderEndpoint(cfg, provider)
	if err != nil {
		return nil, err
	}
	apiKey, err := resolveProviderAPIKey(endpoint)
	if err != nil {
		return nil, err
	}
	return &openAICompatibleEmbedder{endpoint: endpoint, apiKey: apiKey, model: model}, nil
}

func resolveProviderEndpoint(cfg config.Config, provider string) (config.ProviderEndpointConfig, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return cfg.Providers.OpenAI, nil
	case "openrouter":
		return cfg.Providers.OpenRouter, nil
	case "requesty":
		return cfg.Providers.Requesty, nil
	case "zai":
		return cfg.Providers.ZAI, nil
	case "openai_compat":
		return cfg.Providers.OpenAICompat, nil
	default:
		return config.ProviderEndpointConfig{}, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func resolveProviderAPIKey(endpoint config.ProviderEndpointConfig) (string, error) {
	apiKey := strings.TrimSpace(endpoint.APIKey)
	if apiKey == "" && strings.TrimSpace(endpoint.APIKeyEnv) != "" {
		apiKey = strings.TrimSpace(os.Getenv(endpoint.APIKeyEnv))
	}
	if apiKey == "" {
		return "", errors.New("provider api key is required")
	}
	return apiKey, nil
}
