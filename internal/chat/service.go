package chat

import (
	"context"
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
// Returns the final CompletionResponse after streaming is complete.
func (s *Service) SendMessage(ctx context.Context, sessionID, userMsg string, callback func(chunk string)) (*ai.CompletionResponse, error) {
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

	// Build memory-augmented messages
	messages, err := s.buildMessages(session, userMsg)
	if err != nil {
		logging.Get().Warn("chat", fmt.Sprintf("Failed to build memory context: %v", err))
		// Continue without memory augmentation
		messages = s.buildBasicMessages(session)
	}

	// Stream the response
	resp, err := s.aiManager.CompleteStreamChat(ctx, messages, ai.CompletionOptions{
		MaxTokens:   4096,
		Temperature: 0.7,
		Purpose:     "chat",
	}, callback)
	if err != nil {
		return nil, fmt.Errorf("AI completion failed: %w", err)
	}

	// Append assistant message
	s.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{
		Role:      "assistant",
		Content:   resp.Content,
		Timestamp: time.Now(),
		Tokens:    resp.Usage.TotalTokens,
		Cost:      ai.EstimateCost(resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens),
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

	return resp, nil
}

// buildMessages constructs the full message array with memory injection.
func (s *Service) buildMessages(session *ChatSession, userMsg string) ([]ai.ChatMessage, error) {
	var systemParts []string

	systemParts = append(systemParts, "You are a personal AI assistant with memory about the user. Be helpful, concise, and natural.")

	// Inject core memory (L0)
	coreMemory, err := s.retrievalEngine.GetCoreMemory()
	if err == nil && coreMemory != "" {
		systemParts = append(systemParts, fmt.Sprintf("Here is what you know about the user:\n\n%s", coreMemory))
	}

	// Recall relevant memories
	recallResult, err := s.retrievalEngine.RecallMemory(userMsg, 3)
	if err == nil && recallResult != nil && len(recallResult.Entries) > 0 {
		var memoryLines []string
		for _, entry := range recallResult.Entries {
			line := fmt.Sprintf("- %s: %s", entry.Title, entry.Summary)
			if entry.Content != "" {
				contentPreview := entry.Content
				if len(contentPreview) > 500 {
					contentPreview = contentPreview[:500] + "..."
				}
				line += "\n  " + contentPreview
			}
			memoryLines = append(memoryLines, line)
		}
		systemParts = append(systemParts, fmt.Sprintf("Relevant memories for this conversation:\n%s", strings.Join(memoryLines, "\n")))
	}

	systemPrompt := strings.Join(systemParts, "\n\n")

	var messages []ai.ChatMessage
	messages = append(messages, ai.ChatMessage{Role: "system", Content: systemPrompt})

	// Add conversation history (skip system messages, last N messages to fit context)
	historyMessages := session.Messages
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

	historyMessages := session.Messages
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
