package memory

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"yumem/internal/workspace"
)

// L2Entry represents an entry in the raw text index
type L2Entry struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`         // "entity" (default) or "conversation"
	FilePath    string            `json:"file_path"`    // Absolute path to the file
	ContentHash string            `json:"content_hash"` // MD5 hash for change detection
	Size        int64             `json:"size"`         // File size in bytes
	MimeType    string            `json:"mime_type"`    // File MIME type
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Tags        []string          `json:"tags"`         // User defined tags
	Metadata    map[string]string `json:"metadata"`     // Additional metadata
}

type L2Manager struct {
	mu        sync.RWMutex
	indexDir  string
	indexFile string
}

func NewL2Manager() *L2Manager {
	config := workspace.GetConfig()
	return &L2Manager{
		indexDir:  config.L2Dir,
		indexFile: filepath.Join(config.L2Dir, "index.json"),
	}
}

func (m *L2Manager) generateID(filePath string) string {
	hash := md5.Sum([]byte(filePath))
	return fmt.Sprintf("%x", hash)
}

func (m *L2Manager) LoadIndex() (map[string]*L2Entry, error) {
	// Check for content/ → entities/ migration (needs write lock)
	contentDir := filepath.Join(m.indexDir, "content")
	if _, err := os.Stat(contentDir); err == nil {
		m.mu.Lock()
		if err := m.migrateContentToEntities(); err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("migration failed: %w", err)
		}
		m.mu.Unlock()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadIndexUnlocked()
}

func (m *L2Manager) loadIndexUnlocked() (map[string]*L2Entry, error) {
	data, err := os.ReadFile(m.indexFile)
	if os.IsNotExist(err) {
		return make(map[string]*L2Entry), nil
	}
	if err != nil {
		return nil, err
	}

	var entries map[string]*L2Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	// Default empty Type to "entity" for backward compatibility
	for _, entry := range entries {
		if entry.Type == "" {
			entry.Type = "entity"
		}
	}

	return entries, nil
}

func (m *L2Manager) SaveIndex(entries map[string]*L2Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveIndexUnlocked(entries)
}

func (m *L2Manager) saveIndexUnlocked(entries map[string]*L2Entry) error {
	jsonData, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.indexFile, jsonData, 0644)
}

func (m *L2Manager) AddFile(filePath string, tags []string) (*L2Entry, error) {
	// Get file info (outside lock - just filesystem reads)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", md5.Sum(content))

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}

	id := m.generateID(absPath)

	entry := &L2Entry{
		ID:          id,
		Type:        "entity",
		FilePath:    absPath,
		ContentHash: hash,
		Size:        fileInfo.Size(),
		MimeType:    m.detectMimeType(absPath),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Tags:        tags,
		Metadata:    make(map[string]string),
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

func (m *L2Manager) UpdateFile(id string, tags []string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	entry, exists := entries[id]
	if !exists {
		return fmt.Errorf("entry with id %s not found", id)
	}

	// Update content hash if file has changed
	if _, err := os.Stat(entry.FilePath); err == nil {
		content, err := os.ReadFile(entry.FilePath)
		if err == nil {
			newHash := fmt.Sprintf("%x", md5.Sum(content))
			if newHash != entry.ContentHash {
				entry.ContentHash = newHash
				entry.UpdatedAt = time.Now()
			}
		}
	}

	if tags != nil {
		entry.Tags = tags
	}
	if metadata != nil {
		for k, v := range metadata {
			entry.Metadata[k] = v
		}
	}
	entry.UpdatedAt = time.Now()

	return m.saveIndexUnlocked(entries)
}

func (m *L2Manager) GetEntry(id string) (*L2Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	entry, exists := entries[id]
	if !exists {
		return nil, fmt.Errorf("entry with id %s not found", id)
	}

	return entry, nil
}

func (m *L2Manager) GetContent(id string) ([]byte, error) {
	m.mu.RLock()
	entries, err := m.loadIndexUnlocked()
	if err != nil {
		m.mu.RUnlock()
		return nil, err
	}

	entry, exists := entries[id]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("entry with id %s not found", id)
	}
	entryType := entry.Type
	filePath := entry.FilePath
	m.mu.RUnlock()

	if entryType == "conversation" {
		return m.getConversationAsText(id)
	}

	return os.ReadFile(filePath)
}

func (m *L2Manager) SearchEntries(query string, tags []string) ([]*L2Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	var results []*L2Entry
	queryLower := strings.ToLower(query)

	for _, entry := range entries {
		if m.entryMatches(entry, queryLower, tags) {
			results = append(results, entry)
		}
	}

	return results, nil
}

func (m *L2Manager) entryMatches(entry *L2Entry, query string, filterTags []string) bool {
	// Check tags filter
	if len(filterTags) > 0 {
		hasTag := false
		for _, filterTag := range filterTags {
			for _, tag := range entry.Tags {
				if strings.EqualFold(tag, filterTag) {
					hasTag = true
					break
				}
			}
			if hasTag {
				break
			}
		}
		if !hasTag {
			return false
		}
	}

	// If no query, return true (tags filter already applied)
	if query == "" {
		return true
	}

	// Check file path
	if strings.Contains(strings.ToLower(entry.FilePath), query) {
		return true
	}

	// Check tags
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}

	return false
}

// AddEntry adds text content directly to L2 without requiring a file
func (m *L2Manager) AddEntry(title, content, contentType, source string, tags []string) (*L2Entry, error) {
	virtualPath := fmt.Sprintf("virtual/%s/%s.txt", source, strings.ReplaceAll(title, "/", "_"))
	hash := fmt.Sprintf("%x", md5.Sum([]byte(content)))
	id := m.generateID(virtualPath)

	entry := &L2Entry{
		ID:          id,
		Type:        "entity",
		FilePath:    virtualPath,
		ContentHash: hash,
		Size:        int64(len(content)),
		MimeType:    "text/plain",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Tags:        tags,
		Metadata: map[string]string{
			"title":        title,
			"content_type": contentType,
			"source":       source,
			"virtual":      "true",
		},
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	if existingEntry, exists := entries[id]; exists {
		if existingEntry.ContentHash != hash {
			entry.CreatedAt = existingEntry.CreatedAt
			entries[id] = entry
		} else {
			return existingEntry, nil
		}
	} else {
		entries[id] = entry
	}

	contentDir := filepath.Join(m.indexDir, "entities")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return nil, err
	}

	contentFile := filepath.Join(contentDir, id+".txt")
	if err := os.WriteFile(contentFile, []byte(content), 0644); err != nil {
		return nil, err
	}

	entry.FilePath = contentFile

	if err := m.saveIndexUnlocked(entries); err != nil {
		return nil, err
	}

	return entry, nil
}

// AppendContent appends text to an existing virtual L2 entry's content file.
// Updates ContentHash, Size, UpdatedAt in the index.
func (m *L2Manager) AppendContent(id string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	entry, exists := entries[id]
	if !exists {
		return fmt.Errorf("entry with id %s not found", id)
	}

	// Append to content file (check both entities/ and legacy content/ paths)
	contentFile := entry.FilePath
	if !filepath.IsAbs(contentFile) || !fileExists(contentFile) {
		contentFile = filepath.Join(m.indexDir, "entities", id+".txt")
	}
	f, err := os.OpenFile(contentFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open content file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("failed to append content: %w", err)
	}

	// Re-read full content for hash/size update
	fullContent, err := os.ReadFile(contentFile)
	if err != nil {
		return fmt.Errorf("failed to read content file: %w", err)
	}

	entry.ContentHash = fmt.Sprintf("%x", md5.Sum(fullContent))
	entry.Size = int64(len(fullContent))
	entry.UpdatedAt = time.Now()

	return m.saveIndexUnlocked(entries)
}

// FindByMetadata finds the first L2 entry matching a metadata key-value pair.
// Returns nil, nil if not found.
func (m *L2Manager) FindByMetadata(key, value string) (*L2Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Metadata != nil && entry.Metadata[key] == value {
			return entry, nil
		}
	}

	return nil, nil
}

// UpdateMetadata updates metadata fields on an existing L2 entry.
func (m *L2Manager) UpdateMetadata(id string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	entry, exists := entries[id]
	if !exists {
		return fmt.Errorf("entry with id %s not found", id)
	}

	if entry.Metadata == nil {
		entry.Metadata = make(map[string]string)
	}
	for k, v := range metadata {
		entry.Metadata[k] = v
	}
	entry.UpdatedAt = time.Now()

	return m.saveIndexUnlocked(entries)
}

// migrateContentToEntities migrates files from the legacy content/ directory to entities/.
// Called automatically during LoadIndex. Only runs if content/ exists and has files.
func (m *L2Manager) migrateContentToEntities() error {
	contentDir := filepath.Join(m.indexDir, "content")
	entitiesDir := filepath.Join(m.indexDir, "entities")

	// Check if content/ exists
	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		return nil
	}

	dirEntries, err := os.ReadDir(contentDir)
	if err != nil {
		return err
	}
	if len(dirEntries) == 0 {
		os.Remove(contentDir)
		return nil
	}

	// Ensure entities/ exists
	if err := os.MkdirAll(entitiesDir, 0755); err != nil {
		return err
	}

	// Move files
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		oldPath := filepath.Join(contentDir, de.Name())
		newPath := filepath.Join(entitiesDir, de.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("failed to migrate %s: %w", de.Name(), err)
		}
	}

	// Update index entries to point to new paths
	entries, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if strings.Contains(entry.FilePath, filepath.Join("l2", "content")) {
			entry.FilePath = strings.Replace(entry.FilePath, filepath.Join("l2", "content"), filepath.Join("l2", "entities"), 1)
		}
		if entry.Type == "" {
			entry.Type = "entity"
		}
	}

	if err := m.saveIndexUnlocked(entries); err != nil {
		return err
	}

	// Remove empty content/ directory
	os.Remove(contentDir)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (m *L2Manager) detectMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	default:
		return "application/octet-stream"
	}
}