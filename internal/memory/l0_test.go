package memory

import (
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

func TestL0OversizeAllowedWithIsOversize(t *testing.T) {
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

	// Save should succeed even when oversize
	err := m.Save(data)
	if err != nil {
		t.Fatalf("expected Save to succeed for oversize data, got: %v", err)
	}

	// IsOversize should return true
	if !m.IsOversize() {
		t.Error("expected IsOversize() to return true for data exceeding 10KB")
	}

	// Verify data was actually written and can be loaded back
	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("failed to load oversize data: %v", err)
	}
	if len(loaded.Traits["large_category"]) != 200 {
		t.Errorf("expected 200 traits, got %d", len(loaded.Traits["large_category"]))
	}
}

func TestL0IsOversizeFalseForSmallData(t *testing.T) {
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

	if m.IsOversize() {
		t.Error("expected IsOversize() to return false for small data")
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
