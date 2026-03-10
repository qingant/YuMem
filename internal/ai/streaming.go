package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamProvider is an optional interface for providers that support streaming completions.
type StreamProvider interface {
	CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error)
}

// ChatMessage represents a message in a multi-turn conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChatProvider is an optional interface for providers that support streaming multi-turn chat.
type StreamChatProvider interface {
	CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error)
}

// CompleteStream performs a streaming completion via the default provider.
// If the provider implements StreamProvider, it uses streaming; otherwise falls back to Complete.
func (m *Manager) CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string), providerName ...string) (*CompletionResponse, error) {
	var name string
	if len(providerName) > 0 {
		name = providerName[0]
	}

	provider, err := m.GetProvider(name)
	if err != nil {
		return nil, err
	}

	var resp *CompletionResponse

	if sp, ok := provider.(StreamProvider); ok {
		resp, err = sp.CompleteStream(ctx, prompt, options, callback)
	} else {
		// Fallback: non-streaming complete, send as single chunk
		resp, err = provider.Complete(ctx, prompt, options)
		if err == nil {
			callback(resp.Content)
		}
	}

	if err != nil {
		return nil, err
	}

	// Auto-track usage
	if m.Usage != nil {
		m.Usage.Track(UsageRecord{
			Provider:         resp.ProviderName,
			Model:            resp.Model,
			Purpose:          options.Purpose,
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		})
	}

	return resp, nil
}

// CompleteStreamChat performs a streaming multi-turn chat completion.
// If the provider implements StreamChatProvider, it uses it; otherwise falls back to single-prompt streaming.
func (m *Manager) CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string), providerName ...string) (*CompletionResponse, error) {
	var name string
	if len(providerName) > 0 {
		name = providerName[0]
	}

	provider, err := m.GetProvider(name)
	if err != nil {
		return nil, err
	}

	var resp *CompletionResponse

	if sp, ok := provider.(StreamChatProvider); ok {
		resp, err = sp.CompleteStreamChat(ctx, messages, options, callback)
	} else if sp, ok := provider.(StreamProvider); ok {
		// Fallback: flatten messages into a single prompt
		prompt := flattenMessages(messages)
		resp, err = sp.CompleteStream(ctx, prompt, options, callback)
	} else {
		// Final fallback: non-streaming
		prompt := flattenMessages(messages)
		resp, err = provider.Complete(ctx, prompt, options)
		if err == nil {
			callback(resp.Content)
		}
	}

	if err != nil {
		return nil, err
	}

	if m.Usage != nil {
		m.Usage.Track(UsageRecord{
			Provider:         resp.ProviderName,
			Model:            resp.Model,
			Purpose:          options.Purpose,
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		})
	}

	return resp, nil
}

func flattenMessages(messages []ChatMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "system":
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		}
	}
	sb.WriteString("Assistant: ")
	return sb.String()
}

// === OpenAI Streaming ===

func (p *OpenAIProvider) CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	messages := []ChatMessage{{Role: "user", Content: prompt}}
	return p.CompleteStreamChat(ctx, messages, options, callback)
}

func (p *OpenAIProvider) CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
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

	var apiMessages []map[string]interface{}
	for _, m := range messages {
		apiMessages = append(apiMessages, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	requestBody := map[string]interface{}{
		"model":       model,
		"messages":    apiMessages,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      true,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var fullContent strings.Builder
	var usage Usage

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

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			text := chunk.Choices[0].Delta.Content
			fullContent.WriteString(text)
			callback(text)
		}
		if chunk.Usage != nil {
			usage = Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
	}

	content := fullContent.String()
	if usage.TotalTokens == 0 {
		usage.CompletionTokens = len(content) / 4
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return &CompletionResponse{
		Content:      content,
		Usage:        usage,
		Model:        model,
		ProviderName: "openai",
	}, nil
}

// === Claude Streaming ===

func (p *ClaudeProvider) CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	messages := []ChatMessage{{Role: "user", Content: prompt}}
	return p.CompleteStreamChat(ctx, messages, options, callback)
}

func (p *ClaudeProvider) CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}

	var apiMessages []map[string]interface{}
	var systemPrompt string
	for _, m := range messages {
		if m.Role == "system" {
			systemPrompt += m.Content + "\n"
			continue
		}
		apiMessages = append(apiMessages, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	requestBody := map[string]interface{}{
		"model":      model,
		"messages":   apiMessages,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	if systemPrompt != "" {
		requestBody["system"] = strings.TrimSpace(systemPrompt)
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var fullContent strings.Builder
	var usage Usage

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			usage.PromptTokens = event.Message.Usage.InputTokens
		case "content_block_delta":
			if event.Delta.Text != "" {
				fullContent.WriteString(event.Delta.Text)
				callback(event.Delta.Text)
			}
		case "message_delta":
			usage.CompletionTokens = event.Usage.OutputTokens
		}
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return &CompletionResponse{
		Content:      fullContent.String(),
		Usage:        usage,
		Model:        model,
		ProviderName: "claude",
	}, nil
}

// === Gemini Streaming ===

func (p *GeminiProvider) CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	messages := []ChatMessage{{Role: "user", Content: prompt}}
	return p.CompleteStreamChat(ctx, messages, options, callback)
}

func (p *GeminiProvider) CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "gemini-3.1-flash-lite-preview"
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.7
	}

	var contents []map[string]any
	var systemInstruction string
	for _, m := range messages {
		if m.Role == "system" {
			systemInstruction += m.Content + "\n"
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role": role,
			"parts": []map[string]any{
				{"text": m.Content},
			},
		})
	}

	requestBody := map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTokens,
			"temperature":     temperature,
		},
		"safetySettings": []map[string]string{
			{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "BLOCK_NONE"},
		},
	}

	if systemInstruction != "" {
		requestBody["systemInstruction"] = map[string]any{
			"parts": []map[string]any{
				{"text": strings.TrimSpace(systemInstruction)},
			},
		}
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.BaseURL, model, p.APIKey)
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var fullContent strings.Builder
	var usage Usage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0), 1024*1024) // 1MB buffer for large chunks
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk struct {
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
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
			text := chunk.Candidates[0].Content.Parts[0].Text
			if text != "" {
				fullContent.WriteString(text)
				callback(text)
			}
		}

		if chunk.UsageMetadata.TotalTokenCount > 0 {
			usage = Usage{
				PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
				CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
			}
		}
	}

	return &CompletionResponse{
		Content:      fullContent.String(),
		Usage:        usage,
		Model:        model,
		ProviderName: "gemini",
	}, nil
}

// === GitHub Copilot Streaming (OpenAI-compatible) ===

func (p *GitHubCopilotProvider) CompleteStream(ctx context.Context, prompt string, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	messages := []ChatMessage{{Role: "user", Content: prompt}}
	return p.CompleteStreamChat(ctx, messages, options, callback)
}

func (p *GitHubCopilotProvider) CompleteStreamChat(ctx context.Context, messages []ChatMessage, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
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

	var apiMessages []map[string]interface{}
	for _, m := range messages {
		apiMessages = append(apiMessages, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	requestBody := map[string]interface{}{
		"model":       model,
		"messages":    apiMessages,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      true,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var fullContent strings.Builder
	var usage Usage

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

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			text := chunk.Choices[0].Delta.Content
			fullContent.WriteString(text)
			callback(text)
		}
	}

	content := fullContent.String()
	usage.CompletionTokens = len(content) / 4
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return &CompletionResponse{
		Content:      content,
		Usage:        usage,
		Model:        model,
		ProviderName: "github-copilot",
	}, nil
}
