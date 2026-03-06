package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yumem/internal/workspace"
)

func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := workspace.Initialize(dir); err != nil {
		t.Fatalf("failed to initialize workspace: %v", err)
	}
	return dir
}

func TestL0SaveLoad(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Traits: map[string]map[string][]TimestampedValue{
			"background": {
				"name": {
					{Value: "Alice", ObservedAt: "2024-01-01"},
				},
			},
		},
		Agenda: []AgendaItem{
			{Item: "Learn Go", Priority: "high", Status: "active"},
		},
		Meta: L0Meta{Version: "2.0.0", UpdateTrigger: "test"},
	}

	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.UserID != "test-user" {
		t.Errorf("expected UserID 'test-user', got '%s'", loaded.UserID)
	}

	if len(loaded.Traits["background"]["name"]) != 1 {
		t.Errorf("expected 1 trait value, got %d", len(loaded.Traits["background"]["name"]))
	}

	if loaded.Traits["background"]["name"][0].Value != "Alice" {
		t.Errorf("expected trait value 'Alice', got '%s'", loaded.Traits["background"]["name"][0].Value)
	}

	if len(loaded.Agenda) != 1 || loaded.Agenda[0].Item != "Learn Go" {
		t.Errorf("unexpected agenda: %+v", loaded.Agenda)
	}
}

func TestL0SizeEnforcement(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	// Create data that exceeds 10KB
	data := &L0Data{
		UserID: "test-user",
		Traits: make(map[string]map[string][]TimestampedValue),
		Agenda: []AgendaItem{},
		Meta:   L0Meta{Version: "2.0.0", UpdateTrigger: "test"},
	}

	// Fill with enough traits to exceed 10KB
	data.Traits["large_category"] = make(map[string][]TimestampedValue)
	for i := 0; i < 200; i++ {
		key := strings.Repeat("k", 20)
		value := strings.Repeat("v", 50)
		data.Traits["large_category"][key+string(rune(i+'A'))] = []TimestampedValue{
			{Value: value, ObservedAt: "2024-01-01"},
		}
	}

	err := m.Save(data)
	if err == nil {
		t.Fatal("expected Save to fail with size limit error, but it succeeded")
	}

	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("expected size limit error, got: %v", err)
	}

	// Verify file was NOT written (check traits.json doesn't exist or is empty)
	traitsPath := filepath.Join(m.dataPath, "traits.json")
	if _, err := os.Stat(traitsPath); err == nil {
		// If file exists, it might be from a prior run. Check content.
		content, _ := os.ReadFile(traitsPath)
		if len(content) > 0 {
			t.Error("traits.json should not have been written when size limit exceeded")
		}
	}
}

func TestL0SizeUpdated(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Traits: map[string]map[string][]TimestampedValue{
			"bg": {"name": {{Value: "Bob", ObservedAt: "2024-01-01"}}},
		},
		Agenda: []AgendaItem{},
		Meta:   L0Meta{Version: "2.0.0"},
	}

	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Meta.SizeBytes <= 0 {
		t.Errorf("expected SizeBytes > 0, got %d", loaded.Meta.SizeBytes)
	}
}

func TestL0MergeTraits(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	// Save initial data
	data := &L0Data{
		UserID: "test-user",
		Traits: make(map[string]map[string][]TimestampedValue),
		Agenda: []AgendaItem{},
		Meta:   L0Meta{Version: "2.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Merge a trait
	err := m.MergeTraits("background", "city", TimestampedValue{
		Value:      "Tokyo",
		ObservedAt: "2024-06-01",
	})
	if err != nil {
		t.Fatalf("MergeTraits failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	timeline := loaded.Traits["background"]["city"]
	if len(timeline) != 1 || timeline[0].Value != "Tokyo" {
		t.Errorf("unexpected traits: %+v", timeline)
	}
}
