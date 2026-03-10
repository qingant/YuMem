package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"yumem/internal/ai"
	"yumem/internal/logging"
	"yumem/internal/memory"
	"yumem/internal/retrieval"

	"github.com/google/uuid"
)

// ChatSession represents a chat conversation session.
type ChatSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Messages  []ChatMessage `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	L2ID      string        `json:"l2_id,omitempty"` // L2 conversation entry ID
}

// ChatMessage represents a single message in a chat session.
type ChatMessage struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Tokens    int       `json:"tokens,omitempty"`
	Cost      float64   `json:"cost,omitempty"`
}

// ToolEvent is passed to the caller when the AI invokes (or finishes) a tool.
type ToolEvent struct {
	Type  string `json:"type"`  // "tool_start" or "tool_end"
	Tool  string `json:"tool"`  // tool name
	Query string `json:"query,omitempty"`
}

// chatTools defines the tools exposed to the AI in chat mode.
var chatTools = []ai.ToolDefinition{
	{
		Name:        "recall_memory",
		Description: "Search the user's stored memories for information about a topic. Use when the user asks about something that might be in their notes, past conversations, or stored knowledge. Do NOT use for simple greetings or general knowledge questions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query to find relevant memories",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default 3)",
				},
			},
			"required": []string{"query"},
		},
	},
}

// Service manages chat sessions and AI interactions with memory augmentation.
type Service struct {
	aiManager       *ai.Manager
	l0Manager       *memory.L0Manager
	l2Manager       *memory.L2Manager
	retrievalEngine *retrieval.RetrievalEngine

	mu       sync.RWMutex
	sessions map[string]*ChatSession
}

// NewService creates a new ChatService.
func NewService(aiManager *ai.Manager, l0Manager *memory.L0Manager, l2Manager *memory.L2Manager, retrievalEngine *retrieval.RetrievalEngine) *Service {
	return &Service{
		aiManager:       aiManager,
		l0Manager:       l0Manager,
		l2Manager:       l2Manager,
		retrievalEngine: retrievalEngine,
		sessions:        make(map[string]*ChatSession),
	}
}

// CreateSession creates a new chat session.
func (s *Service) CreateSession() *ChatSession {
	session := &ChatSession{
		ID:        uuid.New().String(),
		Title:     "New Chat",
		Messages:  []ChatMessage{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	return session
}

// GetSession returns a session by ID.
func (s *Service) GetSession(id string) *ChatSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

// ListSessions returns all sessions sorted by UpdatedAt descending.
func (s *Service) ListSessions() []*ChatSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*ChatSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}

	// Sort by UpdatedAt desc
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].UpdatedAt.After(sessions[i].UpdatedAt) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	return sessions
}

// DeleteSession removes a session.
func (s *Service) DeleteSession(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// SendMessage sends a user message and streams the AI response via callback.
// onChunk receives text chunks as they stream. onToolEvent is called when the AI
// invokes or finishes a tool (may be nil).
func (s *Service) SendMessage(ctx context.Context, sessionID, userMsg string, onChunk func(chunk string), onToolEvent func(event ToolEvent)) (*ai.CompletionResponse, error) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	// Append user message
	session.Messages = append(session.Messages, ChatMessage{
		Role:      "user",
		Content:   userMsg,
		Timestamp: time.Now(),
	})
	session.UpdatedAt = time.Now()
	s.mu.Unlock()

	// Build memory-augmented messages (only core memory — fast file read)
	messages, err := s.buildMessages(session)
	if err != nil {
		logging.Get().Warn("chat", fmt.Sprintf("Failed to build memory context: %v", err))
		messages = s.buildBasicMessages(session)
	}

	// Tool call loop — max 3 rounds to prevent infinite loops
	const maxRounds = 3
	var lastResp *ai.CompletionResponse

	for round := 0; round < maxRounds; round++ {
		resp, err := s.aiManager.CompleteStreamChatWithTools(ctx, messages, chatTools, ai.CompletionOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
			Purpose:     "chat",
		}, onChunk)
		if err != nil {
			return nil, fmt.Errorf("AI completion failed: %w", err)
		}

		lastResp = resp

		// No tool calls — we're done
		if len(resp.ToolCalls) == 0 {
			break
		}

		// Append assistant message with tool calls (no visible content yet, or partial)
		messages = append(messages, ai.ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			if onToolEvent != nil {
				query := extractQueryFromArgs(tc.Arguments)
				onToolEvent(ToolEvent{Type: "tool_start", Tool: tc.Name, Query: query})
			}

			result := s.executeTool(tc)

			if onToolEvent != nil {
				onToolEvent(ToolEvent{Type: "tool_end", Tool: tc.Name})
			}

			messages = append(messages, ai.ChatMessage{
				Role: "tool",
				ToolResult: &ai.ToolResult{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    result,
				},
			})
		}
	}

	if lastResp == nil {
		return nil, fmt.Errorf("no response from AI")
	}

	// Append assistant message to session
	s.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{
		Role:      "assistant",
		Content:   lastResp.Content,
		Timestamp: time.Now(),
		Tokens:    lastResp.Usage.TotalTokens,
		Cost:      ai.EstimateCost(lastResp.Model, lastResp.Usage.PromptTokens, lastResp.Usage.CompletionTokens),
	})
	session.UpdatedAt = time.Now()

	// Auto-title from first message
	if len(session.Messages) == 2 && session.Title == "New Chat" {
		title := userMsg
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		session.Title = title
	}
	s.mu.Unlock()

	// Persist to L2 asynchronously
	go s.persistToL2(session)

	return lastResp, nil
}

// executeTool dispatches a tool call and returns the result string.
func (s *Service) executeTool(tc ai.ToolCall) string {
	switch tc.Name {
	case "recall_memory":
		return s.executeRecallMemory(tc.Arguments)
	default:
		return fmt.Sprintf("Unknown tool: %s", tc.Name)
	}
}

// executeRecallMemory parses args and calls RecallMemory.
func (s *Service) executeRecallMemory(argsJSON string) string {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("Failed to parse arguments: %v", err)
	}
	if args.Query == "" {
		return "No query provided"
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 3
	}

	result, err := s.retrievalEngine.RecallMemory(args.Query, args.MaxResults)
	if err != nil {
		return fmt.Sprintf("Memory recall failed: %v", err)
	}

	if result == nil || len(result.Entries) == 0 {
		return "No relevant memories found for this query."
	}

	// Format results for the AI
	var sb strings.Builder
	if result.Summary != "" {
		sb.WriteString(result.Summary)
		sb.WriteString("\n\n")
	}
	for _, entry := range result.Entries {
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s", entry.Title, entry.Path, entry.Summary))
		if entry.Content != "" {
			contentPreview := entry.Content
			if len(contentPreview) > 500 {
				contentPreview = contentPreview[:500] + "..."
			}
			sb.WriteString("\n  ")
			sb.WriteString(contentPreview)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// extractQueryFromArgs pulls the "query" field from a JSON arguments string.
func extractQueryFromArgs(argsJSON string) string {
	var args struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	return args.Query
}

// buildMessages constructs the full message array with core memory only (fast).
// RecallMemory is no longer called here — the AI decides via tool calling.
func (s *Service) buildMessages(session *ChatSession) ([]ai.ChatMessage, error) {
	var systemParts []string
	systemParts = append(systemParts, "You are a personal AI assistant with memory about the user. Be helpful, concise, and natural.")
	systemParts = append(systemParts, "You have access to a recall_memory tool that searches the user's stored memories. Use it when the conversation involves topics the user may have stored — like their notes, past conversations, preferences, or personal knowledge. Do NOT use it for simple greetings, general knowledge, or when you already have enough context.")

	// Get core memory (cheap file read)
	coreMemory, err := s.retrievalEngine.GetCoreMemory()
	if err == nil && coreMemory != "" {
		systemParts = append(systemParts, fmt.Sprintf("Here is what you know about the user:\n\n%s", coreMemory))
	}

	systemPrompt := strings.Join(systemParts, "\n\n")

	var messages []ai.ChatMessage
	messages = append(messages, ai.ChatMessage{Role: "system", Content: systemPrompt})

	// Add conversation history (last N messages to fit context)
	s.mu.RLock()
	historyMessages := session.Messages
	s.mu.RUnlock()

	maxHistory := 20
	if len(historyMessages) > maxHistory {
		historyMessages = historyMessages[len(historyMessages)-maxHistory:]
	}
	for _, msg := range historyMessages {
		messages = append(messages, ai.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages, nil
}

// buildBasicMessages constructs messages without memory augmentation.
func (s *Service) buildBasicMessages(session *ChatSession) []ai.ChatMessage {
	messages := []ai.ChatMessage{
		{Role: "system", Content: "You are a helpful AI assistant. Be concise and natural."},
	}

	s.mu.RLock()
	historyMessages := session.Messages
	s.mu.RUnlock()

	maxHistory := 20
	if len(historyMessages) > maxHistory {
		historyMessages = historyMessages[len(historyMessages)-maxHistory:]
	}
	for _, msg := range historyMessages {
		messages = append(messages, ai.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages
}

// persistToL2 stores the conversation to L2 storage.
func (s *Service) persistToL2(session *ChatSession) {
	s.mu.RLock()
	sessionID := session.ID
	title := session.Title
	msgs := make([]ChatMessage, len(session.Messages))
	copy(msgs, session.Messages)
	l2ID := session.L2ID
	s.mu.RUnlock()

	if l2ID == "" {
		// Create new conversation
		entry, err := s.l2Manager.CreateConversation(sessionID, title, "web_chat")
		if err != nil {
			logging.Get().Error("chat", fmt.Sprintf("Failed to create L2 conversation: %v", err))
			return
		}
		s.mu.Lock()
		session.L2ID = entry.ID
		s.mu.Unlock()
		l2ID = entry.ID
	}

	// Add the latest messages (last 2: user + assistant)
	if len(msgs) >= 2 {
		lastTwo := msgs[len(msgs)-2:]
		for _, msg := range lastTwo {
			memMsg := memory.Message{
				ID:        uuid.New().String(),
				Role:      msg.Role,
				Content:   msg.Content,
				Timestamp: msg.Timestamp.Format(time.RFC3339),
			}
			if err := s.l2Manager.AddMessage(l2ID, memMsg); err != nil {
				logging.Get().Error("chat", fmt.Sprintf("Failed to add message to L2: %v", err))
			}
		}
	}

	// Update title
	if err := s.l2Manager.UpdateConversationMeta(l2ID, func(meta *memory.ConversationMeta) {
		meta.Title = title
	}); err != nil {
		logging.Get().Warn("chat", fmt.Sprintf("Failed to update L2 conversation meta: %v", err))
	}
}
