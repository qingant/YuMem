package memory

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Message represents a single message in a conversation.
type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`                // "user" or "assistant"
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`           // RFC3339
	Model     string `json:"model,omitempty"`
}

// ConversationMeta holds metadata for a conversation L2 entry.
type ConversationMeta struct {
	SessionID          string `json:"session_id"`
	Title              string `json:"title"`
	Source             string `json:"source"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	TotalMessages      int    `json:"total_messages"`
	CurrentSegment     int    `json:"current_segment"`
	SegmentMaxMessages int    `json:"segment_max_messages"`
}

const DefaultSegmentMaxMessages = 50

// CreateConversation creates a new conversation L2 entry with directory structure.
func (m *L2Manager) CreateConversation(sessionID, title, source string) (*L2Entry, error) {
	virtualPath := fmt.Sprintf("conversation/%s", sessionID)
	id := m.generateID(virtualPath)

	convDir := filepath.Join(m.indexDir, "conversations", sessionID)
	if err := os.MkdirAll(convDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create conversation directory: %w", err)
	}

	now := time.Now()
	meta := &ConversationMeta{
		SessionID:          sessionID,
		Title:              title,
		Source:             source,
		CreatedAt:          now.Format(time.RFC3339),
		UpdatedAt:          now.Format(time.RFC3339),
		TotalMessages:      0,
		CurrentSegment:     0,
		SegmentMaxMessages: DefaultSegmentMaxMessages,
	}

	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(convDir, "meta.json"), metaData, 0644); err != nil {
		return nil, err
	}

	// Write empty initial segment
	if err := os.WriteFile(filepath.Join(convDir, "seg_000.json"), []byte("[]"), 0644); err != nil {
		return nil, err
	}

	entry := &L2Entry{
		ID:          id,
		Type:        "conversation",
		FilePath:    convDir,
		ContentHash: fmt.Sprintf("%x", md5.Sum([]byte(sessionID))),
		Size:        0,
		MimeType:    "application/x-conversation",
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        []string{"conversation", source},
		Metadata: map[string]string{
			"session_id":   sessionID,
			"content_type": "conversation",
			"source":       source,
			"title":        title,
			"turn_count":   "0",
			"started_at":   now.Format(time.RFC3339),
			"updated_at":   now.Format(time.RFC3339),
		},
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entries[id] = entry
	if err := m.saveIndexUnlocked(entries); err != nil {
		return nil, err
	}

	return entry, nil
}

// AddMessage appends a message to the conversation's current segment.
// Automatically creates a new segment when the current one is full.
func (m *L2Manager) AddMessage(l2ID string, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return fmt.Errorf("conversation entry %s not found", l2ID)
	}
	if entry.Type != "conversation" {
		return fmt.Errorf("entry %s is not a conversation", l2ID)
	}

	convDir := entry.FilePath

	// Read meta
	meta, err := readConversationMeta(convDir)
	if err != nil {
		return err
	}

	// Read current segment
	segFile := filepath.Join(convDir, fmt.Sprintf("seg_%03d.json", meta.CurrentSegment))
	messages, err := readSegment(segFile)
	if err != nil {
		return err
	}

	// Check if segment is full
	if len(messages) >= meta.SegmentMaxMessages {
		meta.CurrentSegment++
		segFile = filepath.Join(convDir, fmt.Sprintf("seg_%03d.json", meta.CurrentSegment))
		messages = []Message{}
	}

	// Append message
	messages = append(messages, msg)
	if err := writeSegment(segFile, messages); err != nil {
		return err
	}

	// Update meta
	meta.TotalMessages++
	meta.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := writeConversationMeta(convDir, meta); err != nil {
		return err
	}

	// Update index entry
	entry.UpdatedAt = time.Now()
	entry.Metadata["turn_count"] = fmt.Sprintf("%d", meta.TotalMessages)
	entry.Metadata["updated_at"] = meta.UpdatedAt
	entry.Metadata["last_role"] = msg.Role
	if meta.TotalMessages == 1 {
		entry.Metadata["first_role"] = msg.Role
	}

	// Update content hash based on total messages
	entry.ContentHash = fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s-%d", entry.Metadata["session_id"], meta.TotalMessages))))

	return m.saveIndexUnlocked(entries)
}

// GetMessages reads messages from a specific segment of a conversation.
func (m *L2Manager) GetMessages(l2ID string, segmentIndex int) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return nil, fmt.Errorf("conversation entry %s not found", l2ID)
	}

	segFile := filepath.Join(entry.FilePath, fmt.Sprintf("seg_%03d.json", segmentIndex))
	return readSegment(segFile)
}

// GetAllMessages reads all messages from all segments, in order.
func (m *L2Manager) GetAllMessages(l2ID string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return nil, fmt.Errorf("conversation entry %s not found", l2ID)
	}

	return readAllSegments(entry.FilePath)
}

// GetConversationMeta reads the meta.json of a conversation.
func (m *L2Manager) GetConversationMeta(l2ID string) (*ConversationMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return nil, fmt.Errorf("conversation entry %s not found", l2ID)
	}

	return readConversationMeta(entry.FilePath)
}

// UpdateConversationMeta applies updates to a conversation's meta.json.
func (m *L2Manager) UpdateConversationMeta(l2ID string, updates func(*ConversationMeta)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return fmt.Errorf("conversation entry %s not found", l2ID)
	}

	meta, err := readConversationMeta(entry.FilePath)
	if err != nil {
		return err
	}

	updates(meta)
	meta.UpdatedAt = time.Now().Format(time.RFC3339)

	return writeConversationMeta(entry.FilePath, meta)
}

// getConversationAsText reads all segments and formats as readable text.
// Used by GetContent for conversation entries.
func (m *L2Manager) getConversationAsText(l2ID string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entry, exists := entries[l2ID]
	if !exists {
		return nil, fmt.Errorf("conversation entry %s not found", l2ID)
	}

	messages, err := readAllSegments(entry.FilePath)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n\n", msg.Timestamp, msg.Role, msg.Content))
	}

	return []byte(sb.String()), nil
}

// --- File I/O helpers ---

func readConversationMeta(convDir string) (*ConversationMeta, error) {
	data, err := os.ReadFile(filepath.Join(convDir, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read conversation meta: %w", err)
	}
	var meta ConversationMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse conversation meta: %w", err)
	}
	return &meta, nil
}

func writeConversationMeta(convDir string, meta *ConversationMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(convDir, "meta.json"), data, 0644)
}

func readSegment(segFile string) ([]Message, error) {
	data, err := os.ReadFile(segFile)
	if os.IsNotExist(err) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read segment: %w", err)
	}
	var messages []Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("failed to parse segment: %w", err)
	}
	return messages, nil
}

func writeSegment(segFile string, messages []Message) error {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(segFile, data, 0644)
}

func readAllSegments(convDir string) ([]Message, error) {
	dirEntries, err := os.ReadDir(convDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read conversation directory: %w", err)
	}

	var segFiles []string
	for _, de := range dirEntries {
		if strings.HasPrefix(de.Name(), "seg_") && strings.HasSuffix(de.Name(), ".json") {
			segFiles = append(segFiles, de.Name())
		}
	}
	sort.Strings(segFiles)

	var allMessages []Message
	for _, segName := range segFiles {
		messages, err := readSegment(filepath.Join(convDir, segName))
		if err != nil {
			return nil, err
		}
		allMessages = append(allMessages, messages...)
	}

	return allMessages, nil
}
