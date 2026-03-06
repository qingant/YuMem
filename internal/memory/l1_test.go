package memory

import (
	"testing"
)

func TestL1CreateAndSearch(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL1Manager()

	node, err := m.CreateNode("work/projects/yumem", "YuMem Project", "Memory management system", []string{"memory", "ai"}, nil)
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	if node.Title != "YuMem Project" {
		t.Errorf("expected title 'YuMem Project', got '%s'", node.Title)
	}

	// Search
	results, err := m.SearchNodes("yumem")
	if err != nil {
		t.Fatalf("SearchNodes failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].ID != node.ID {
		t.Errorf("expected ID '%s', got '%s'", node.ID, results[0].ID)
	}
}

func TestL1ParentChild(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL1Manager()

	// Create parent
	parent, err := m.CreateNode("work", "Work", "Work related", []string{"work"}, nil)
	if err != nil {
		t.Fatalf("CreateNode (parent) failed: %v", err)
	}

	// Create child
	child, err := m.CreateNode("work/projects", "Projects", "Project list", []string{"projects"}, nil)
	if err != nil {
		t.Fatalf("CreateNode (child) failed: %v", err)
	}

	if child.Parent != parent.ID {
		t.Errorf("expected child.Parent '%s', got '%s'", parent.ID, child.Parent)
	}

	// Reload parent to check children
	reloadedParent, err := m.GetNode(parent.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	found := false
	for _, childID := range reloadedParent.Children {
		if childID == child.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("parent.Children does not contain child ID '%s'", child.ID)
	}
}

func TestL1UpdateNode(t *testing.T) {
	setupTestWorkspace(t)
	m := NewL1Manager()

	node, err := m.CreateNode("test/node", "Test Node", "Original summary", []string{"test"}, nil)
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	err = m.UpdateNode(node.ID, "Updated summary", []string{"test", "updated"})
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}

	updated, err := m.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	if updated.Summary != "Updated summary" {
		t.Errorf("expected summary 'Updated summary', got '%s'", updated.Summary)
	}

	if len(updated.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %d", len(updated.Keywords))
	}
}
