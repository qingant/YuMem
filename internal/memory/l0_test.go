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
		Facts: []Fact{
			{ID: "f-001", Text: "Name: Alice", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
			{ID: "f-002", Text: "Learning Go programming", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
		},
		Meta: L0Meta{Version: "3.0.0", UpdateTrigger: "test"},
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

	if len(loaded.Facts) != 2 {
		t.Errorf("expected 2 facts, got %d", len(loaded.Facts))
	}

	if loaded.Facts[0].Text != "Name: Alice" {
		t.Errorf("expected fact text 'Name: Alice', got '%s'", loaded.Facts[0].Text)
	}
}

func TestL0AddFact(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	// Save initial empty data
	data := &L0Data{
		UserID: "test-user",
		Facts:  []Fact{},
		Meta:   L0Meta{Version: "3.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Add a fact
	err := m.AddFact(Fact{
		Text:       "Lives in Tokyo",
		ObservedAt: "2024-06-01",
		Source:     "l2-001",
		SourceName: "travel notes",
	})
	if err != nil {
		t.Fatalf("AddFact failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(loaded.Facts))
	}
	if loaded.Facts[0].Text != "Lives in Tokyo" {
		t.Errorf("unexpected fact text: %s", loaded.Facts[0].Text)
	}

	// Add duplicate — should be no-op
	err = m.AddFact(Fact{
		Text:   "Lives in Tokyo",
		Source: "l2-001",
	})
	if err != nil {
		t.Fatalf("AddFact (duplicate) failed: %v", err)
	}

	loaded, err = m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Facts) != 1 {
		t.Errorf("expected 1 fact after duplicate add, got %d", len(loaded.Facts))
	}
}

func TestL0GetFilteredFacts(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Facts: []Fact{
			{ID: "f-001", Text: "Active fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
			{ID: "f-002", Text: "Expired fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01", Expired: true},
			{ID: "f-003", Text: "Another active", ObservedAt: "2024-06-01", CreatedAt: "2024-06-01"},
		},
		Meta: L0Meta{Version: "3.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	filtered, err := m.GetFilteredFacts()
	if err != nil {
		t.Fatalf("GetFilteredFacts failed: %v", err)
	}

	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered facts, got %d", len(filtered))
	}
	for _, f := range filtered {
		if f.Expired {
			t.Error("filtered facts should not include expired")
		}
	}
}

func TestL0Oversize(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Facts:  []Fact{},
		Meta:   L0Meta{Version: "3.0.0", UpdateTrigger: "test"},
	}

	// Fill with enough facts to exceed 10KB
	for i := 0; i < 200; i++ {
		data.Facts = append(data.Facts, Fact{
			ID:         "f-" + strings.Repeat("x", 10),
			Text:       strings.Repeat("v", 50),
			ObservedAt: "2024-01-01",
			CreatedAt:  "2024-01-01",
		})
	}

	err := m.Save(data)
	if err != nil {
		t.Fatalf("expected Save to succeed for oversize data, got: %v", err)
	}

	if !m.IsOversize() {
		t.Error("expected IsOversize() to return true for data exceeding 10KB")
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("failed to load oversize data: %v", err)
	}
	if len(loaded.Facts) != 200 {
		t.Errorf("expected 200 facts, got %d", len(loaded.Facts))
	}
}

func TestL0IsOversizeFalseForSmallData(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Facts: []Fact{
			{ID: "f-001", Text: "Small fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
		},
		Meta: L0Meta{Version: "3.0.0"},
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
		Facts: []Fact{
			{ID: "f-001", Text: "Some fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
		},
		Meta: L0Meta{Version: "3.0.0"},
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

func TestL0Update(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	// Initialize
	data := &L0Data{
		UserID: "test-user",
		Facts:  []Fact{},
		Meta:   L0Meta{Version: "3.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update with name and context
	err := m.Update("", "Bob", "Software developer", map[string]string{"lang": "Go"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Facts) != 3 {
		t.Errorf("expected 3 facts (name, context, pref), got %d", len(loaded.Facts))
	}
}

func TestL0GetFactsJSON(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Facts: []Fact{
			{ID: "f-001", Text: "Active fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
			{ID: "f-002", Text: "Expired", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01", Expired: true},
		},
		Meta: L0Meta{Version: "3.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	jsonStr, err := m.GetFactsJSON()
	if err != nil {
		t.Fatalf("GetFactsJSON failed: %v", err)
	}

	if !strings.Contains(jsonStr, "Active fact") {
		t.Error("expected JSON to contain active fact")
	}
	if strings.Contains(jsonStr, "Expired") {
		t.Error("expected JSON to not contain expired fact")
	}
}

func TestL0ReplaceFacts(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL0Manager()

	data := &L0Data{
		UserID: "test-user",
		Facts: []Fact{
			{ID: "f-001", Text: "Old fact", ObservedAt: "2024-01-01", CreatedAt: "2024-01-01"},
		},
		Meta: L0Meta{Version: "3.0.0"},
	}
	if err := m.Save(data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Replace all facts
	newFacts := []Fact{
		{ID: "f-new-001", Text: "Consolidated fact", ObservedAt: "2024-06-01", CreatedAt: "2024-06-01"},
	}
	if err := m.ReplaceFacts(newFacts); err != nil {
		t.Fatalf("ReplaceFacts failed: %v", err)
	}

	loaded, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Facts) != 1 || loaded.Facts[0].Text != "Consolidated fact" {
		t.Errorf("unexpected facts after replace: %+v", loaded.Facts)
	}
}
