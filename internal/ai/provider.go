package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider represents an AI provider interface
type Provider interface {
	Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error)
	GetProviderName() string
}

// CompletionOptions holds options for AI completion requests
type CompletionOptions struct {
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	Model       string  `json:"model,omitempty"`
}

// CompletionResponse holds the response from an AI provider
type CompletionResponse struct {
	Content      string `json:"content"`
	Usage        Usage  `json:"usage"`
	Model        string `json:"model"`
	ProviderName string `json:"provider"`
}

// Usage holds token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIProvider implements Provider for OpenAI API
type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Complete implements Provider interface for OpenAI
func (p *OpenAIProvider) Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "gpt-4-turbo-preview"
	}
	
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}
	
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.7
	}

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	return &CompletionResponse{
		Content: openAIResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     openAIResp.Usage.PromptTokens,
			CompletionTokens: openAIResp.Usage.CompletionTokens,
			TotalTokens:      openAIResp.Usage.TotalTokens,
		},
		Model:        openAIResp.Model,
		ProviderName: "openai",
	}, nil
}

// GetProviderName returns the provider name
func (p *OpenAIProvider) GetProviderName() string {
	return "openai"
}

// ClaudeProvider implements Provider for Anthropic Claude API
type ClaudeProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(apiKey string) *ClaudeProvider {
	return &ClaudeProvider{
		APIKey:  apiKey,
		BaseURL: "https://api.anthropic.com",
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Complete implements Provider interface for Claude
func (p *ClaudeProvider) Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens": maxTokens,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var claudeResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("no content returned")
	}

	return &CompletionResponse{
		Content: claudeResp.Content[0].Text,
		Usage: Usage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
		Model:        claudeResp.Model,
		ProviderName: "claude",
	}, nil
}

// GetProviderName returns the provider name
func (p *ClaudeProvider) GetProviderName() string {
	return "claude"
}

// LocalProvider implements a mock provider that doesn't require API calls
type LocalProvider struct{}

// NewLocalProvider creates a new local mock provider
func NewLocalProvider() *LocalProvider {
	return &LocalProvider{}
}

// Complete implements Provider interface with heuristic responses
func (p *LocalProvider) Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error) {
	// Simple heuristic-based responses for when no AI provider is configured
	var content string
	
	if strings.Contains(strings.ToLower(prompt), "analyze") {
		content = `{
			"category": "general",
			"summary": "User content requiring analysis",
			"keywords": ["content", "analysis", "general"],
			"importance": "medium",
			"suggested_path": "general/imported"
		}`
	} else if strings.Contains(strings.ToLower(prompt), "context") {
		content = "Based on the available information, here is the relevant context for the user's query."
	} else {
		content = "I apologize, but no AI provider is configured. This is a placeholder response. Please configure an AI provider (OpenAI, Claude, etc.) for full functionality."
	}

	return &CompletionResponse{
		Content:      content,
		Usage:        Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		Model:        "local-heuristic",
		ProviderName: "local",
	}, nil
}

// GetProviderName returns the provider name
func (p *LocalProvider) GetProviderName() string {
	return "local"
}

// Manager manages AI provider configurations
type Manager struct {
	providers map[string]Provider
	default_provider string
}

// NewManager creates a new AI provider manager
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
		default_provider: "local",
	}
}

// AddProvider adds a provider to the manager
func (m *Manager) AddProvider(name string, provider Provider) {
	m.providers[name] = provider
}

// SetDefaultProvider sets the default provider
func (m *Manager) SetDefaultProvider(name string) error {
	if _, exists := m.providers[name]; !exists {
		return fmt.Errorf("provider %s not found", name)
	}
	m.default_provider = name
	return nil
}

// GetProvider returns a provider by name
func (m *Manager) GetProvider(name string) (Provider, error) {
	if name == "" {
		name = m.default_provider
	}
	
	provider, exists := m.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	
	return provider, nil
}

// Complete performs completion using the default or specified provider
func (m *Manager) Complete(ctx context.Context, prompt string, options CompletionOptions, providerName ...string) (*CompletionResponse, error) {
	var name string
	if len(providerName) > 0 {
		name = providerName[0]
	}
	
	provider, err := m.GetProvider(name)
	if err != nil {
		return nil, err
	}
	
	return provider.Complete(ctx, prompt, options)
}

// ListProviders returns all available provider names
func (m *Manager) ListProviders() []string {
	var names []string
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}