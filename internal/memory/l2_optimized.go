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

// L2IndexShard represents a shard of the L2 index for performance
type L2IndexShard struct {
	ShardID   string             `json:"shard_id"`
	Entries   map[string]*L2Entry `json:"entries"`
	MaxSize   int                 `json:"max_size"`   // Maximum entries per shard
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// L2OptimizedManager provides optimized L2 management with sharding
type L2OptimizedManager struct {
	mu            sync.RWMutex
	indexDir      string
	masterIndex   string // Master index tracking all shards
	shardsDir     string // Directory containing shard files
	maxShardSize  int    // Maximum entries per shard (default: 1000)
}

// L2MasterIndex tracks all shards and provides quick lookup
type L2MasterIndex struct {
	Version    int                        `json:"version"`
	Shards     map[string]*L2ShardMeta    `json:"shards"`     // shard_id -> metadata
	EntryMap   map[string]string          `json:"entry_map"`  // entry_id -> shard_id
	Statistics L2IndexStats              `json:"statistics"`
	CreatedAt  time.Time                  `json:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at"`
}

type L2ShardMeta struct {
	ShardID     string    `json:"shard_id"`
	FilePath    string    `json:"file_path"`
	EntryCount  int       `json:"entry_count"`
	TotalSize   int64     `json:"total_size"`
	LastUpdated time.Time `json:"last_updated"`
	Tags        []string  `json:"common_tags"` // Most frequent tags in this shard
}

type L2IndexStats struct {
	TotalEntries    int   `json:"total_entries"`
	TotalShards     int   `json:"total_shards"`
	TotalSizeBytes  int64 `json:"total_size_bytes"`
	AverageFileSize int64 `json:"average_file_size"`
	LargestShard    int   `json:"largest_shard"`
	SmallestShard   int   `json:"smallest_shard"`
}

// NewL2OptimizedManager creates a new optimized L2 manager
func NewL2OptimizedManager() *L2OptimizedManager {
	config := workspace.GetConfig()
	return &L2OptimizedManager{
		indexDir:     config.L2Dir,
		masterIndex:  filepath.Join(config.L2Dir, "master_index.json"),
		shardsDir:    filepath.Join(config.L2Dir, "shards"),
		maxShardSize: 1000, // Configurable: 1000 entries per shard
	}
}

// Initialize sets up the optimized L2 index structure
func (m *L2OptimizedManager) Initialize() error {
	// Create necessary directories
	if err := os.MkdirAll(m.shardsDir, 0755); err != nil {
		return err
	}

	// Initialize master index if it doesn't exist
	if _, err := os.Stat(m.masterIndex); os.IsNotExist(err) {
		masterIdx := &L2MasterIndex{
			Version:   1,
			Shards:    make(map[string]*L2ShardMeta),
			EntryMap:  make(map[string]string),
			Statistics: L2IndexStats{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		return m.saveMasterIndex(masterIdx)
	}

	return nil
}

// AddEntry adds content to the optimized L2 index
func (m *L2OptimizedManager) AddEntry(title, content, contentType, source string, tags []string) (*L2Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load master index
	masterIdx, err := m.loadMasterIndex()
	if err != nil {
		return nil, err
	}

	// Create the entry
	virtualPath := fmt.Sprintf("virtual/%s/%s.txt", source, strings.ReplaceAll(title, "/", "_"))
	hash := fmt.Sprintf("%x", md5.Sum([]byte(content)))
	id := m.generateID(virtualPath)

	entry := &L2Entry{
		ID:          id,
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

	// Find or create appropriate shard
	shardID, err := m.findOrCreateShardForEntry(masterIdx, entry)
	if err != nil {
		return nil, err
	}

	// Load the shard
	shard, err := m.loadShard(shardID)
	if err != nil {
		return nil, err
	}

	// Add entry to shard
	shard.Entries[id] = entry
	shard.UpdatedAt = time.Now()

	// Store content file
	contentFile := filepath.Join(m.indexDir, "content", id+".txt")
	if err := os.WriteFile(contentFile, []byte(content), 0644); err != nil {
		return nil, err
	}
	entry.FilePath = contentFile

	// Save shard
	if err := m.saveShard(shard); err != nil {
		return nil, err
	}

	// Update master index
	masterIdx.EntryMap[id] = shardID
	masterIdx.Statistics.TotalEntries++
	masterIdx.Statistics.TotalSizeBytes += entry.Size
	masterIdx.UpdatedAt = time.Now()

	// Update shard metadata
	if shardMeta, exists := masterIdx.Shards[shardID]; exists {
		shardMeta.EntryCount = len(shard.Entries)
		shardMeta.TotalSize += entry.Size
		shardMeta.LastUpdated = time.Now()
		// Update common tags
		shardMeta.Tags = m.calculateCommonTags(shard)
	}

	// Save master index
	if err := m.saveMasterIndex(masterIdx); err != nil {
		return nil, err
	}

	return entry, nil
}

// findOrCreateShardForEntry finds the best shard for an entry or creates a new one
func (m *L2OptimizedManager) findOrCreateShardForEntry(masterIdx *L2MasterIndex, entry *L2Entry) (string, error) {
	// Strategy 1: Find shard with similar tags (content affinity)
	for shardID, shardMeta := range masterIdx.Shards {
		if shardMeta.EntryCount < m.maxShardSize {
			// Check tag overlap
			if m.hasTagOverlap(entry.Tags, shardMeta.Tags) {
				return shardID, nil
			}
		}
	}

	// Strategy 2: Find any shard with space
	for shardID, shardMeta := range masterIdx.Shards {
		if shardMeta.EntryCount < m.maxShardSize {
			return shardID, nil
		}
	}

	// Strategy 3: Create new shard
	return m.createNewShard(masterIdx, entry)
}

// hasTagOverlap checks if there's meaningful tag overlap between entry and shard
func (m *L2OptimizedManager) hasTagOverlap(entryTags, shardTags []string) bool {
	if len(shardTags) == 0 || len(entryTags) == 0 {
		return false
	}

	overlap := 0
	for _, entryTag := range entryTags {
		for _, shardTag := range shardTags {
			if entryTag == shardTag {
				overlap++
				break
			}
		}
	}
	
	// Require at least 50% overlap
	return float64(overlap)/float64(len(entryTags)) >= 0.5
}

// createNewShard creates a new shard for entries
func (m *L2OptimizedManager) createNewShard(masterIdx *L2MasterIndex, firstEntry *L2Entry) (string, error) {
	hash := md5.Sum([]byte(firstEntry.ID))
	shardID := fmt.Sprintf("shard_%d_%x", time.Now().Unix(), hash[:4])
	
	shard := &L2IndexShard{
		ShardID:   shardID,
		Entries:   make(map[string]*L2Entry),
		MaxSize:   m.maxShardSize,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	shardMeta := &L2ShardMeta{
		ShardID:     shardID,
		FilePath:    filepath.Join(m.shardsDir, shardID+".json"),
		EntryCount:  0,
		TotalSize:   0,
		LastUpdated: time.Now(),
		Tags:        firstEntry.Tags, // Initialize with first entry's tags
	}

	// Add to master index
	masterIdx.Shards[shardID] = shardMeta
	masterIdx.Statistics.TotalShards++

	// Save initial empty shard
	return shardID, m.saveShard(shard)
}

// Helper methods for shard management
func (m *L2OptimizedManager) loadMasterIndex() (*L2MasterIndex, error) {
	data, err := os.ReadFile(m.masterIndex)
	if err != nil {
		return nil, err
	}

	var masterIdx L2MasterIndex
	err = json.Unmarshal(data, &masterIdx)
	return &masterIdx, err
}

func (m *L2OptimizedManager) saveMasterIndex(masterIdx *L2MasterIndex) error {
	data, err := json.MarshalIndent(masterIdx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.masterIndex, data, 0644)
}

func (m *L2OptimizedManager) loadShard(shardID string) (*L2IndexShard, error) {
	shardFile := filepath.Join(m.shardsDir, shardID+".json")
	data, err := os.ReadFile(shardFile)
	if err != nil {
		return nil, err
	}

	var shard L2IndexShard
	err = json.Unmarshal(data, &shard)
	return &shard, err
}

func (m *L2OptimizedManager) saveShard(shard *L2IndexShard) error {
	shardFile := filepath.Join(m.shardsDir, shard.ShardID+".json")
	data, err := json.MarshalIndent(shard, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(shardFile, data, 0644)
}

func (m *L2OptimizedManager) generateID(virtualPath string) string {
	hash := md5.Sum([]byte(virtualPath))
	return fmt.Sprintf("%x", hash)
}

func (m *L2OptimizedManager) calculateCommonTags(shard *L2IndexShard) []string {
	tagCount := make(map[string]int)
	totalEntries := len(shard.Entries)
	
	// Count tag frequencies
	for _, entry := range shard.Entries {
		for _, tag := range entry.Tags {
			tagCount[tag]++
		}
	}
	
	// Return tags that appear in >50% of entries
	var commonTags []string
	threshold := totalEntries / 2
	for tag, count := range tagCount {
		if count > threshold {
			commonTags = append(commonTags, tag)
		}
	}
	
	return commonTags
}

// SearchEntries provides optimized search across shards
func (m *L2OptimizedManager) SearchEntries(query string, tags []string) ([]*L2Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	masterIdx, err := m.loadMasterIndex()
	if err != nil {
		return nil, err
	}

	var results []*L2Entry

	// Search strategy: prioritize shards with matching tags
	shardPriority := m.rankShardsByRelevance(masterIdx, tags)

	for _, shardID := range shardPriority {
		shard, err := m.loadShard(shardID)
		if err != nil {
			continue // Skip problematic shards
		}

		// Search within shard
		for _, entry := range shard.Entries {
			if m.entryMatches(entry, query, tags) {
				results = append(results, entry)
			}
		}
	}

	return results, nil
}

// rankShardsByRelevance orders shards by relevance to search tags
func (m *L2OptimizedManager) rankShardsByRelevance(masterIdx *L2MasterIndex, searchTags []string) []string {
	if len(searchTags) == 0 {
		// Return all shards if no specific tags
		var allShards []string
		for shardID := range masterIdx.Shards {
			allShards = append(allShards, shardID)
		}
		return allShards
	}

	type shardScore struct {
		shardID string
		score   float64
	}

	var scores []shardScore
	for shardID, shardMeta := range masterIdx.Shards {
		overlap := 0
		for _, searchTag := range searchTags {
			for _, shardTag := range shardMeta.Tags {
				if searchTag == shardTag {
					overlap++
					break
				}
			}
		}
		
		score := float64(overlap) / float64(len(searchTags))
		scores = append(scores, shardScore{shardID, score})
	}

	// Sort by score (simple bubble sort for small datasets)
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score < scores[j].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	var rankedShards []string
	for _, score := range scores {
		rankedShards = append(rankedShards, score.shardID)
	}

	return rankedShards
}

// entryMatches checks if an entry matches search criteria
func (m *L2OptimizedManager) entryMatches(entry *L2Entry, query string, filterTags []string) bool {
	// Tag filtering
	if len(filterTags) > 0 {
		hasMatchingTag := false
		for _, filterTag := range filterTags {
			for _, entryTag := range entry.Tags {
				if entryTag == filterTag {
					hasMatchingTag = true
					break
				}
			}
			if hasMatchingTag {
				break
			}
		}
		if !hasMatchingTag {
			return false
		}
	}

	// Query matching (simple substring search)
	if query != "" {
		queryLower := strings.ToLower(query)
		
		// Search in title
		if title, ok := entry.Metadata["title"]; ok {
			if strings.Contains(strings.ToLower(title), queryLower) {
				return true
			}
		}
		
		// Search in source
		if source, ok := entry.Metadata["source"]; ok {
			if strings.Contains(strings.ToLower(source), queryLower) {
				return true
			}
		}

		// Search in tags
		for _, tag := range entry.Tags {
			if strings.Contains(strings.ToLower(tag), queryLower) {
				return true
			}
		}

		return false
	}

	return true
}

// GetEntry retrieves a specific entry by ID
func (m *L2OptimizedManager) GetEntry(id string) (*L2Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	masterIdx, err := m.loadMasterIndex()
	if err != nil {
		return nil, err
	}

	shardID, exists := masterIdx.EntryMap[id]
	if !exists {
		return nil, fmt.Errorf("entry not found: %s", id)
	}

	shard, err := m.loadShard(shardID)
	if err != nil {
		return nil, err
	}

	entry, exists := shard.Entries[id]
	if !exists {
		return nil, fmt.Errorf("entry not found in shard: %s", id)
	}

	return entry, nil
}

// GetContent retrieves the actual content of an entry
func (m *L2OptimizedManager) GetContent(id string) ([]byte, error) {
	m.mu.RLock()
	masterIdx, err := m.loadMasterIndex()
	if err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	shardID, exists := masterIdx.EntryMap[id]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("entry not found: %s", id)
	}
	shard, err := m.loadShard(shardID)
	if err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	entry, exists := shard.Entries[id]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("entry not found in shard: %s", id)
	}
	filePath := entry.FilePath
	m.mu.RUnlock()

	return os.ReadFile(filePath)
}


// GetStatistics returns current L2 index statistics
func (m *L2OptimizedManager) GetStatistics() (*L2IndexStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	masterIdx, err := m.loadMasterIndex()
	if err != nil {
		return nil, err
	}

	// Update statistics
	stats := &masterIdx.Statistics
	if stats.TotalEntries > 0 {
		stats.AverageFileSize = stats.TotalSizeBytes / int64(stats.TotalEntries)
	}

	// Calculate shard size range
	var minShard, maxShard int = 999999, 0
	for _, shardMeta := range masterIdx.Shards {
		if shardMeta.EntryCount < minShard {
			minShard = shardMeta.EntryCount
		}
		if shardMeta.EntryCount > maxShard {
			maxShard = shardMeta.EntryCount
		}
	}
	stats.SmallestShard = minShard
	stats.LargestShard = maxShard

	return stats, nil
}