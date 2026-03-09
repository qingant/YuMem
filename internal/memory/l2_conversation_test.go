package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yumem/internal/workspace"
)

func setupTestL2Manager(t *testing.T) (*L2Manager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	if err := workspace.Initialize(tmpDir); err != nil {
		t.Fatalf("failed to init workspace: %v", err)
	}
	return NewL2Manager(), tmpDir
}

func TestCreateConversation(t *testing.T) {
	mgr, _ := setupTestL2Manager(t)

	entry, err := mgr.CreateConversation("sess-001", "Test Conversation", "test")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	if entry.Type != "conversation" {
		t.Errorf("expected type conversation, got %s", entry.Type)
	}
	if entry.Metadata["session_id"] != "sess-001" {
		t.Errorf("expected session_id sess-001, got %s", entry.Metadata["session_id"])
	}

	// Verify directory structure
	convDir := entry.FilePath
	if _, err := os.Stat(filepath.Join(convDir, "meta.json")); err != nil {
		t.Error("meta.json should exist")
	}
	if _, err := os.Stat(filepath.Join(convDir, "seg_000.json")); err != nil {
		t.Error("seg_000.json should exist")
	}
}

func TestAddMessage(t *testing.T) {
	mgr, _ := setupTestL2Manager(t)

	entry, err := mgr.CreateConversation("sess-002", "Msg Test", "test")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	msg := Message{
		ID:        "msg-001",
		Role:      "user",
		Content:   "Hello!",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := mgr.AddMessage(entry.ID, msg); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	meta, err := mgr.GetConversationMeta(entry.ID)
	if err != nil {
		t.Fatalf("GetConversationMeta failed: %v", err)
	}
	if meta.TotalMessages != 1 {
		t.Errorf("expected 1 message, got %d", meta.TotalMessages)
	}

	// Read back
	messages, err := mgr.GetMessages(entry.ID, 0)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Content != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", messages[0].Content)
	}
}

func TestSegmentRollover(t *testing.T) {
	mgr, _ := setupTestL2Manager(t)

	entry, err := mgr.CreateConversation("sess-003", "Segment Test", "test")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	// Override segment size for testing
	if err := mgr.UpdateConversationMeta(entry.ID, func(m *ConversationMeta) {
		m.SegmentMaxMessages = 3
	}); err != nil {
		t.Fatalf("UpdateConversationMeta failed: %v", err)
	}

	// Add 5 messages (should create seg_000 with 3, seg_001 with 2)
	for i := 0; i < 5; i++ {
		msg := Message{
			ID:        fmt.Sprintf("msg-%03d", i),
			Role:      "user",
			Content:   fmt.Sprintf("Message %d", i),
			Timestamp: time.Now().Format(time.RFC3339),
		}
		if err := mgr.AddMessage(entry.ID, msg); err != nil {
			t.Fatalf("AddMessage %d failed: %v", i, err)
		}
	}

	meta, err := mgr.GetConversationMeta(entry.ID)
	if err != nil {
		t.Fatalf("GetConversationMeta failed: %v", err)
	}
	if meta.TotalMessages != 5 {
		t.Errorf("expected 5 messages, got %d", meta.TotalMessages)
	}
	if meta.CurrentSegment != 1 {
		t.Errorf("expected current segment 1, got %d", meta.CurrentSegment)
	}

	// Verify seg_000 has 3 messages
	seg0, err := mgr.GetMessages(entry.ID, 0)
	if err != nil {
		t.Fatalf("GetMessages(0) failed: %v", err)
	}
	if len(seg0) != 3 {
		t.Errorf("seg_000 should have 3 messages, got %d", len(seg0))
	}

	// Verify seg_001 has 2 messages
	seg1, err := mgr.GetMessages(entry.ID, 1)
	if err != nil {
		t.Fatalf("GetMessages(1) failed: %v", err)
	}
	if len(seg1) != 2 {
		t.Errorf("seg_001 should have 2 messages, got %d", len(seg1))
	}

	// GetAllMessages should return all 5
	all, err := mgr.GetAllMessages(entry.ID)
	if err != nil {
		t.Fatalf("GetAllMessages failed: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("expected 5 total messages, got %d", len(all))
	}
}

func TestGetContentConversation(t *testing.T) {
	mgr, _ := setupTestL2Manager(t)

	entry, err := mgr.CreateConversation("sess-004", "Content Test", "test")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	ts := "2026-03-09T10:00:00Z"
	if err := mgr.AddMessage(entry.ID, Message{
		ID: "msg-001", Role: "user", Content: "Hi there", Timestamp: ts,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddMessage(entry.ID, Message{
		ID: "msg-002", Role: "assistant", Content: "Hello!", Timestamp: ts,
	}); err != nil {
		t.Fatal(err)
	}

	content, err := mgr.GetContent(entry.ID)
	if err != nil {
		t.Fatalf("GetContent failed: %v", err)
	}

	text := string(content)
	if !contains(text, "user: Hi there") {
		t.Errorf("content should contain user message, got: %s", text)
	}
	if !contains(text, "assistant: Hello!") {
		t.Errorf("content should contain assistant message, got: %s", text)
	}
}

func TestAddEntryIsEntity(t *testing.T) {
	mgr, _ := setupTestL2Manager(t)

	entry, err := mgr.AddEntry("test note", "some content", "note", "test", []string{"test"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	if entry.Type != "entity" {
		t.Errorf("expected type entity, got %s", entry.Type)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
