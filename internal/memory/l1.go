package memory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"yumem/internal/workspace"
)

// L1Node represents a node in the semantic index tree
type L1Node struct {
	ID          string            `json:"id"`
	Path        string            `json:"path"`        // Tree path like "work/projects/yumem"
	Title       string            `json:"title"`       // Human readable title
	Summary     string            `json:"summary"`     // LLM generated summary
	Keywords    []string          `json:"keywords"`    // Extracted keywords
	Children    []string          `json:"children"`    // Child node IDs
	Parent      string            `json:"parent"`      // Parent node ID
	L2Refs      []string          `json:"l2_refs"`     // References to L2 data
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Metadata    map[string]string `json:"metadata"`
}

type L1Manager struct {
	mu        sync.RWMutex
	indexDir  string
	indexFile string
}

func NewL1Manager() *L1Manager {
	config := workspace.GetConfig()
	return &L1Manager{
		indexDir: config.L1Dir,
		indexFile: filepath.Join(config.L1Dir, "index.json"),
	}
}

func (m *L1Manager) generateID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", hash[:8])
}

// GenerateID is the exported version of generateID for external use.
func (m *L1Manager) GenerateID(path string) string {
	return m.generateID(path)
}

func (m *L1Manager) LoadIndex() (map[string]*L1Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadIndexUnlocked()
}

func (m *L1Manager) loadIndexUnlocked() (map[string]*L1Node, error) {
	data, err := os.ReadFile(m.indexFile)
	if os.IsNotExist(err) {
		return make(map[string]*L1Node), nil
	}
	if err != nil {
		return nil, err
	}

	var nodes map[string]*L1Node
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

func (m *L1Manager) SaveIndex(nodes map[string]*L1Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveIndexUnlocked(nodes)
}

func (m *L1Manager) saveIndexUnlocked(nodes map[string]*L1Node) error {
	jsonData, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.indexFile, jsonData, 0644)
}

func (m *L1Manager) CreateNode(path, title, summary string, keywords []string, l2Refs []string) (*L1Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	id := m.generateID(path)
	node := &L1Node{
		ID:        id,
		Path:      path,
		Title:     title,
		Summary:   summary,
		Keywords:  keywords,
		Children:  []string{},
		L2Refs:    l2Refs,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]string),
	}

	// Find parent
	parentPath := m.getParentPath(path)
	if parentPath != "" {
		parentID := m.generateID(parentPath)
		if parent, exists := nodes[parentID]; exists {
			node.Parent = parentID
			parent.Children = append(parent.Children, id)
		}
	}

	nodes[id] = node

	if err := m.saveIndexUnlocked(nodes); err != nil {
		return nil, err
	}

	return node, nil
}

func (m *L1Manager) UpdateNode(id, summary string, keywords []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return err
	}

	node, exists := nodes[id]
	if !exists {
		return fmt.Errorf("node with id %s not found", id)
	}

	if summary != "" {
		node.Summary = summary
	}
	if keywords != nil {
		node.Keywords = keywords
	}
	node.UpdatedAt = time.Now()

	return m.saveIndexUnlocked(nodes)
}

func (m *L1Manager) GetNode(id string) (*L1Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	node, exists := nodes[id]
	if !exists {
		return nil, fmt.Errorf("node with id %s not found", id)
	}

	return node, nil
}

func (m *L1Manager) SearchNodes(query string) ([]*L1Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	var results []*L1Node
	queryLower := strings.ToLower(query)

	for _, node := range nodes {
		// Skip conversation index nodes from general search
		if strings.HasPrefix(node.Path, "conversations/") {
			continue
		}
		if m.nodeMatches(node, queryLower) {
			results = append(results, node)
		}
	}

	return results, nil
}

// SearchConversationNodes searches only conversation index nodes.
func (m *L1Manager) SearchConversationNodes(query string) ([]*L1Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return nil, err
	}

	var results []*L1Node
	queryLower := strings.ToLower(query)

	for _, node := range nodes {
		if !strings.HasPrefix(node.Path, "conversations/") {
			continue
		}
		if m.nodeMatches(node, queryLower) {
			results = append(results, node)
		}
	}

	return results, nil
}

// GetConversationNode returns the L1 node for a specific conversation session.
func (m *L1Manager) GetConversationNode(sessionID string) (*L1Node, error) {
	convPath := "conversations/" + sessionID
	id := m.generateID(convPath)
	return m.GetNode(id)
}

func (m *L1Manager) nodeMatches(node *L1Node, query string) bool {
	// Check title
	if strings.Contains(strings.ToLower(node.Title), query) {
		return true
	}
	
	// Check summary
	if strings.Contains(strings.ToLower(node.Summary), query) {
		return true
	}
	
	// Check keywords
	for _, keyword := range node.Keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			return true
		}
	}
	
	// Check path
	if strings.Contains(strings.ToLower(node.Path), query) {
		return true
	}

	return false
}

func (m *L1Manager) getParentPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// RemoveNodesByL2Ref removes all nodes that reference the given L2 ID.
// Also cleans up parent→child references. Returns the number of nodes removed.
func (m *L1Manager) RemoveNodesByL2Ref(l2ID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nodes, err := m.loadIndexUnlocked()
	if err != nil {
		return 0, err
	}

	// Find nodes to remove
	var toRemove []string
	for id, node := range nodes {
		for _, ref := range node.L2Refs {
			if ref == l2ID {
				toRemove = append(toRemove, id)
				break
			}
		}
	}

	if len(toRemove) == 0 {
		return 0, nil
	}

	removeSet := make(map[string]bool, len(toRemove))
	for _, id := range toRemove {
		removeSet[id] = true
	}

	// Clean up parent→child references
	for _, node := range nodes {
		var filtered []string
		for _, childID := range node.Children {
			if !removeSet[childID] {
				filtered = append(filtered, childID)
			}
		}
		node.Children = filtered
	}

	// Delete the nodes
	for _, id := range toRemove {
		delete(nodes, id)
	}

	if err := m.saveIndexUnlocked(nodes); err != nil {
		return 0, err
	}

	return len(toRemove), nil
}

func (m *L1Manager) GetTree() (map[string]*L1Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadIndexUnlocked()
}