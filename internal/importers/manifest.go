package importers

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ImportManifest tracks previously imported items for incremental import.
type ImportManifest struct {
	Source       string                   `json:"source"`
	LastImportAt time.Time               `json:"last_import_at"`
	Entries      map[string]ManifestEntry `json:"entries"`
}

// ManifestEntry records a single imported item.
type ManifestEntry struct {
	Title       string    `json:"title"`
	ContentHash string    `json:"content_hash"`
	L2ID        string    `json:"l2_id"`
	ImportedAt  time.Time `json:"imported_at"`
}

// LoadManifest loads a manifest from disk. Returns an empty manifest if the file doesn't exist.
func LoadManifest(path string) (*ImportManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ImportManifest{
				Entries: make(map[string]ManifestEntry),
			}, nil
		}
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var m ImportManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.Entries == nil {
		m.Entries = make(map[string]ManifestEntry)
	}
	return &m, nil
}

// Save writes the manifest to disk atomically.
func (m *ImportManifest) Save(path string) error {
	m.LastImportAt = time.Now()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// NeedsProcessing returns true if the item is new or its content has changed.
func (m *ImportManifest) NeedsProcessing(id, contentHash string) bool {
	entry, exists := m.Entries[id]
	if !exists {
		return true
	}
	return entry.ContentHash != contentHash
}

// Record stores a processed item in the manifest.
func (m *ImportManifest) Record(id string, entry ManifestEntry) {
	m.Entries[id] = entry
}

// ContentHash computes MD5 hex digest of content for change detection.
func ContentHash(content string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(content)))
}
