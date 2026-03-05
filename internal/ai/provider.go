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

// GeminiProvider implements Provider for Google Gemini API
type GeminiProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		APIKey:  apiKey,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Complete implements Provider interface for Gemini
func (p *GeminiProvider) Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "gemini-1.5-flash"
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
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{
						"text": prompt,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": maxTokens,
			"temperature":     temperature,
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.BaseURL, model, p.APIKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no content returned from Gemini")
	}

	return &CompletionResponse{
		Content: geminiResp.Candidates[0].Content.Parts[0].Text,
		Usage: Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
		Model:        model,
		ProviderName: "gemini",
	}, nil
}

// GetProviderName returns the provider name
func (p *GeminiProvider) GetProviderName() string {
	return "gemini"
}

// GitHubCopilotProvider implements Provider for GitHub Copilot API
type GitHubCopilotProvider struct {
	AccessToken   string
	RefreshToken  string
	BaseURL       string
	Client        *http.Client
	Authenticated bool
}

// NewGitHubCopilotProvider creates a new GitHub Copilot provider
func NewGitHubCopilotProvider(accessToken string) *GitHubCopilotProvider {
	return &GitHubCopilotProvider{
		AccessToken: accessToken,
		BaseURL:     "https://api.github.com/copilot_internal",
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Authenticated: accessToken != "",
	}
}

// NewGitHubCopilotProviderWithOAuth creates a new GitHub Copilot provider with OAuth tokens
func NewGitHubCopilotProviderWithOAuth(accessToken, refreshToken string) *GitHubCopilotProvider {
	return &GitHubCopilotProvider{
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		BaseURL:       "https://api.github.com/copilot_internal",
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Authenticated: accessToken != "",
	}
}

// Complete implements Provider interface for GitHub Copilot
func (p *GitHubCopilotProvider) Complete(ctx context.Context, prompt string, options CompletionOptions) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "gpt-4o"
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
	req.Header.Set("Authorization", "Bearer "+p.AccessToken)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var copilotResp struct {
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

	if err := json.NewDecoder(resp.Body).Decode(&copilotResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(copilotResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	return &CompletionResponse{
		Content: copilotResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     copilotResp.Usage.PromptTokens,
			CompletionTokens: copilotResp.Usage.CompletionTokens,
			TotalTokens:      copilotResp.Usage.TotalTokens,
		},
		Model:        copilotResp.Model,
		ProviderName: "github-copilot",
	}, nil
}

// GetProviderName returns the provider name
func (p *GitHubCopilotProvider) GetProviderName() string {
	return "github-copilot"
}

// ModelInfo represents information about a model
type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	Description string   `json:"description"`
	ContextSize int      `json:"context_size"`
	Capabilities []string `json:"capabilities"`
}

// GetAvailableModels returns available models for a provider
func (p *OpenAIProvider) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.BaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	var modelsResp struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []ModelInfo
	for _, model := range modelsResp.Data {
		if model.Object == "model" {
			models = append(models, ModelInfo{
				ID:          model.ID,
				Name:        model.ID,
				Provider:    "openai",
				Description: "OpenAI " + model.ID,
				ContextSize: getOpenAIContextSize(model.ID),
				Capabilities: []string{"text-generation", "chat"},
			})
		}
	}

	return models, nil
}

// GetAvailableModels returns available models for Gemini
func (p *GeminiProvider) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	// Gemini models are predefined
	models := []ModelInfo{
		{
			ID:          "gemini-1.5-flash",
			Name:        "Gemini 1.5 Flash",
			Provider:    "gemini",
			Description: "Fast and efficient model for most tasks",
			ContextSize: 1048576,
			Capabilities: []string{"text-generation", "chat", "multimodal"},
		},
		{
			ID:          "gemini-1.5-pro",
			Name:        "Gemini 1.5 Pro",
			Provider:    "gemini",
			Description: "Advanced model for complex reasoning tasks",
			ContextSize: 2097152,
			Capabilities: []string{"text-generation", "chat", "multimodal", "reasoning"},
		},
		{
			ID:          "gemini-1.0-pro",
			Name:        "Gemini 1.0 Pro",
			Provider:    "gemini",
			Description: "Previous generation pro model",
			ContextSize: 32768,
			Capabilities: []string{"text-generation", "chat"},
		},
	}

	return models, nil
}

// GetAvailableModels returns available models for Claude
func (p *ClaudeProvider) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	// Claude models are predefined
	models := []ModelInfo{
		{
			ID:          "claude-3-5-sonnet-20241022",
			Name:        "Claude 3.5 Sonnet",
			Provider:    "claude",
			Description: "Most capable model for complex tasks",
			ContextSize: 200000,
			Capabilities: []string{"text-generation", "chat", "reasoning", "coding"},
		},
		{
			ID:          "claude-3-haiku-20240307",
			Name:        "Claude 3 Haiku",
			Provider:    "claude",
			Description: "Fast and efficient for simple tasks",
			ContextSize: 200000,
			Capabilities: []string{"text-generation", "chat"},
		},
	}

	return models, nil
}

// GetAvailableModels returns available models for GitHub Copilot
func (p *GitHubCopilotProvider) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	// GitHub Copilot models are predefined
	models := []ModelInfo{
		{
			ID:          "gpt-4o",
			Name:        "GPT-4o",
			Provider:    "github-copilot",
			Description: "Latest GPT-4 optimized model via GitHub Copilot",
			ContextSize: 128000,
			Capabilities: []string{"text-generation", "chat", "coding"},
		},
		{
			ID:          "gpt-4o-mini",
			Name:        "GPT-4o Mini",
			Provider:    "github-copilot",
			Description: "Smaller, faster version of GPT-4o",
			ContextSize: 128000,
			Capabilities: []string{"text-generation", "chat", "coding"},
		},
	}

	return models, nil
}

// GetAvailableModels returns available models for local provider
func (p *LocalProvider) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	models := []ModelInfo{
		{
			ID:          "local-heuristic",
			Name:        "Local Heuristic",
			Provider:    "local",
			Description: "Local rule-based processing (no API required)",
			ContextSize: 8192,
			Capabilities: []string{"text-processing", "basic-analysis"},
		},
	}

	return models, nil
}

func getOpenAIContextSize(modelID string) int {
	switch {
	case strings.Contains(modelID, "gpt-4-turbo"):
		return 128000
	case strings.Contains(modelID, "gpt-4"):
		return 8192
	case strings.Contains(modelID, "gpt-3.5-turbo"):
		return 16385
	default:
		return 4096
	}
}