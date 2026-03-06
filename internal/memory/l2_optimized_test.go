package memory

import (
	"testing"
)

func TestHasTagOverlapEmptyEntryTags(t *testing.T) {
	m := &L2OptimizedManager{}

	// This should NOT panic (was division by zero before fix)
	result := m.hasTagOverlap([]string{}, []string{"tag1", "tag2"})
	if result {
		t.Error("expected false for empty entry tags")
	}
}

func TestHasTagOverlapEmptyShardTags(t *testing.T) {
	m := &L2OptimizedManager{}

	result := m.hasTagOverlap([]string{"tag1"}, []string{})
	if result {
		t.Error("expected false for empty shard tags")
	}
}

func TestHasTagOverlapBothEmpty(t *testing.T) {
	m := &L2OptimizedManager{}

	result := m.hasTagOverlap([]string{}, []string{})
	if result {
		t.Error("expected false for both empty")
	}
}

func TestHasTagOverlapMatch(t *testing.T) {
	m := &L2OptimizedManager{}

	// 1 out of 2 = 50%, should match
	result := m.hasTagOverlap([]string{"a", "b"}, []string{"a", "c"})
	if !result {
		t.Error("expected true for 50% overlap")
	}
}

func TestHasTagOverlapNoMatch(t *testing.T) {
	m := &L2OptimizedManager{}

	// 0 out of 3 < 50%, should not match
	result := m.hasTagOverlap([]string{"x", "y", "z"}, []string{"a", "b"})
	if result {
		t.Error("expected false for 0% overlap")
	}
}
