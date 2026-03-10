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
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
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

// === Tool-aware streaming ===

// StreamChatWithToolsProvider is an optional interface for providers that support
// streaming multi-turn chat with tool/function calling.
type StreamChatWithToolsProvider interface {
	CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error)
}

// CompleteStreamChatWithTools performs a streaming chat completion with tool support.
// If the provider implements StreamChatWithToolsProvider, it uses it; otherwise
// falls back to CompleteStreamChat (no tools).
func (m *Manager) CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string), providerName ...string) (*CompletionResponse, error) {
	var name string
	if len(providerName) > 0 {
		name = providerName[0]
	}

	provider, err := m.GetProvider(name)
	if err != nil {
		return nil, err
	}

	var resp *CompletionResponse

	if tp, ok := provider.(StreamChatWithToolsProvider); ok && len(tools) > 0 {
		resp, err = tp.CompleteStreamChatWithTools(ctx, messages, tools, options, callback)
	} else if sp, ok := provider.(StreamChatProvider); ok {
		resp, err = sp.CompleteStreamChat(ctx, messages, options, callback)
	} else if sp, ok := provider.(StreamProvider); ok {
		prompt := flattenMessages(messages)
		resp, err = sp.CompleteStream(ctx, prompt, options, callback)
	} else {
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

// openAIToolsToAPI converts ToolDefinitions to the OpenAI-compatible tools format.
func openAIToolsToAPI(tools []ToolDefinition) []map[string]any {
	var result []map[string]any
	for _, t := range tools {
		result = append(result, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return result
}

// openAIMessagesToAPI converts ChatMessages (including tool results) to OpenAI wire format.
func openAIMessagesToAPI(messages []ChatMessage) []map[string]any {
	var result []map[string]any
	for _, m := range messages {
		msg := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}
		if len(m.ToolCalls) > 0 {
			var tc []map[string]any
			for _, call := range m.ToolCalls {
				tc = append(tc, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			msg["tool_calls"] = tc
		}
		if m.ToolResult != nil {
			msg["role"] = "tool"
			msg["tool_call_id"] = m.ToolResult.ToolCallID
			msg["content"] = m.ToolResult.Content
		}
		result = append(result, msg)
	}
	return result
}

// === OpenAI CompleteStreamChatWithTools ===

func (p *OpenAIProvider) CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
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

	requestBody := map[string]any{
		"model":       model,
		"messages":    openAIMessagesToAPI(messages),
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      true,
		"tools":       openAIToolsToAPI(tools),
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
	var toolCalls []ToolCall
	// Track partial tool call accumulation
	tcNames := make(map[int]string)
	tcArgs := make(map[int]string)
	tcIDs := make(map[int]string)

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
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
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

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				fullContent.WriteString(delta.Content)
				callback(delta.Content)
			}
			for _, tc := range delta.ToolCalls {
				idx := tc.Index
				if tc.ID != "" {
					tcIDs[idx] = tc.ID
				}
				if tc.Function.Name != "" {
					tcNames[idx] = tc.Function.Name
				}
				tcArgs[idx] += tc.Function.Arguments
			}
		}

		if chunk.Usage != nil {
			usage = Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
	}

	// Assemble accumulated tool calls
	for idx := range tcNames {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tcIDs[idx],
			Name:      tcNames[idx],
			Arguments: tcArgs[idx],
		})
	}

	content := fullContent.String()
	if usage.TotalTokens == 0 {
		usage.CompletionTokens = len(content) / 4
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return &CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage:        usage,
		Model:        model,
		ProviderName: "openai",
	}, nil
}

// === Claude CompleteStreamChatWithTools ===

func (p *ClaudeProvider) CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
	model := options.Model
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}

	// Build API messages — Claude uses structured content for tool results
	var apiMessages []map[string]any
	var systemPrompt string
	for _, m := range messages {
		if m.Role == "system" {
			systemPrompt += m.Content + "\n"
			continue
		}
		if m.ToolResult != nil {
			// Tool result → user message with tool_result content block
			apiMessages = append(apiMessages, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": m.ToolResult.ToolCallID,
						"content":     m.ToolResult.Content,
					},
				},
			})
			continue
		}
		if len(m.ToolCalls) > 0 {
			// Assistant message with tool_use content blocks
			var content []map[string]any
			if m.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				var input any
				_ = json.Unmarshal([]byte(tc.Arguments), &input)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			apiMessages = append(apiMessages, map[string]any{
				"role":    "assistant",
				"content": content,
			})
			continue
		}
		apiMessages = append(apiMessages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	// Build tools array for Claude
	var claudeTools []map[string]any
	for _, t := range tools {
		claudeTools = append(claudeTools, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.Parameters,
		})
	}

	requestBody := map[string]any{
		"model":      model,
		"messages":   apiMessages,
		"max_tokens": maxTokens,
		"stream":     true,
		"tools":      claudeTools,
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
	var toolCalls []ToolCall

	// Track current tool use block
	var curToolID, curToolName string
	var curToolArgs strings.Builder
	inToolUse := false

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type         string `json:"type"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
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
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				inToolUse = true
				curToolID = event.ContentBlock.ID
				curToolName = event.ContentBlock.Name
				curToolArgs.Reset()
			}
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				fullContent.WriteString(event.Delta.Text)
				callback(event.Delta.Text)
			} else if event.Delta.Type == "input_json_delta" && event.Delta.PartialJSON != "" {
				curToolArgs.WriteString(event.Delta.PartialJSON)
			}
		case "content_block_stop":
			if inToolUse {
				toolCalls = append(toolCalls, ToolCall{
					ID:        curToolID,
					Name:      curToolName,
					Arguments: curToolArgs.String(),
				})
				inToolUse = false
			}
		case "message_delta":
			usage.CompletionTokens = event.Usage.OutputTokens
		}
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return &CompletionResponse{
		Content:      fullContent.String(),
		ToolCalls:    toolCalls,
		Usage:        usage,
		Model:        model,
		ProviderName: "claude",
	}, nil
}

// === Gemini CompleteStreamChatWithTools ===

func (p *GeminiProvider) CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
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

	// Build contents
	var contents []map[string]any
	var systemInstruction string
	for _, m := range messages {
		if m.Role == "system" {
			systemInstruction += m.Content + "\n"
			continue
		}
		if m.ToolResult != nil {
			// Function response
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{
						"functionResponse": map[string]any{
							"name":     m.ToolResult.Name,
							"response": map[string]any{"result": m.ToolResult.Content},
						},
					},
				},
			})
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if len(m.ToolCalls) > 0 {
			// Model message with function calls
			var parts []map[string]any
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(tc.Arguments), &args)
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Name,
						"args": args,
					},
				})
			}
			contents = append(contents, map[string]any{
				"role":  role,
				"parts": parts,
			})
			continue
		}
		contents = append(contents, map[string]any{
			"role": role,
			"parts": []map[string]any{
				{"text": m.Content},
			},
		})
	}

	// Build function declarations
	var funcDecls []map[string]any
	for _, t := range tools {
		funcDecls = append(funcDecls, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		})
	}

	requestBody := map[string]any{
		"contents": contents,
		"tools": []map[string]any{
			{"functionDeclarations": funcDecls},
		},
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
	var toolCalls []ToolCall

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0), 1024*1024)
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
						Text         string `json:"text"`
						FunctionCall *struct {
							Name string         `json:"name"`
							Args map[string]any `json:"args"`
						} `json:"functionCall"`
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

		if len(chunk.Candidates) > 0 {
			for _, part := range chunk.Candidates[0].Content.Parts {
				if part.Text != "" {
					fullContent.WriteString(part.Text)
					callback(part.Text)
				}
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					toolCalls = append(toolCalls, ToolCall{
						ID:        fmt.Sprintf("gemini_%s_%d", part.FunctionCall.Name, len(toolCalls)),
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					})
				}
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
		ToolCalls:    toolCalls,
		Usage:        usage,
		Model:        model,
		ProviderName: "gemini",
	}, nil
}

// === GitHub Copilot CompleteStreamChatWithTools (OpenAI-compatible) ===

func (p *GitHubCopilotProvider) CompleteStreamChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options CompletionOptions, callback func(chunk string)) (*CompletionResponse, error) {
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

	requestBody := map[string]any{
		"model":       model,
		"messages":    openAIMessagesToAPI(messages),
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      true,
		"tools":       openAIToolsToAPI(tools),
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
	var toolCalls []ToolCall
	tcNames := make(map[int]string)
	tcArgs := make(map[int]string)
	tcIDs := make(map[int]string)

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
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				fullContent.WriteString(delta.Content)
				callback(delta.Content)
			}
			for _, tc := range delta.ToolCalls {
				idx := tc.Index
				if tc.ID != "" {
					tcIDs[idx] = tc.ID
				}
				if tc.Function.Name != "" {
					tcNames[idx] = tc.Function.Name
				}
				tcArgs[idx] += tc.Function.Arguments
			}
		}
	}

	for idx := range tcNames {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tcIDs[idx],
			Name:      tcNames[idx],
			Arguments: tcArgs[idx],
		})
	}

	content := fullContent.String()
	usage.CompletionTokens = len(content) / 4
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return &CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage:        usage,
		Model:        model,
		ProviderName: "github-copilot",
	}, nil
}
