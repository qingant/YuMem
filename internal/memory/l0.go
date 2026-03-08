package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"yumem/internal/workspace"
)

// L0MaxSizeBytes is the maximum allowed size for L0 data (10KB).
const L0MaxSizeBytes = 10 * 1024

// L0Data represents core user information that's always included in conversations.
// Traits are organized as dynamic categories defined by AI during import.
type L0Data struct {
	UserID string                                   `json:"user_id"`
	Traits map[string]map[string][]TimestampedValue `json:"traits"` // category → key → timeline
	Agenda []AgendaItem                             `json:"agenda"`
	Meta   L0Meta                                   `json:"meta"`
}

type L0Meta struct {
	Version       string    `json:"version"`
	LastUpdated   time.Time `json:"last_updated"`
	SizeBytes     int64     `json:"size_bytes"`
	UpdateTrigger string    `json:"update_trigger"`
}

type TimestampedValue struct {
	Value      string  `json:"value"`
	ValidFrom  string  `json:"valid_from,omitempty"`  // e.g. "2022-07" or "2022-07-01"
	ValidUntil string  `json:"valid_until,omitempty"` // empty = ongoing/current
	ObservedAt string  `json:"observed_at"`           // when we learned this
	Confidence float64 `json:"confidence,omitempty"`
	Source     string  `json:"source,omitempty"`      // L2 ID reference
}

type AgendaItem struct {
	Item        string   `json:"item"`
	Priority    string   `json:"priority"` // high, medium, low
	Since       string   `json:"since,omitempty"`
	LastUpdated string   `json:"last_updated,omitempty"`
	Status      string   `json:"status"` // active, paused, completed
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Source      string   `json:"source,omitempty"` // L2 ID reference
}

type L0Manager struct {
	mu       sync.RWMutex
	dataPath string
}

func NewL0Manager() *L0Manager {
	config := workspace.GetConfig()
	return &L0Manager{
		dataPath: filepath.Join(config.L0Dir, "current"),
	}
}

func (m *L0Manager) Load() (*L0Data, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadUnlocked()
}

func (m *L0Manager) loadUnlocked() (*L0Data, error) {
	data := &L0Data{
		UserID: "default",
		Traits: make(map[string]map[string][]TimestampedValue),
		Agenda: []AgendaItem{},
		Meta: L0Meta{
			Version:       "2.0.0",
			LastUpdated:   time.Now(),
			UpdateTrigger: "initialization",
		},
	}

	// Load traits
	if traitsData, err := m.loadFile("traits.json"); err == nil {
		var traits map[string]map[string][]TimestampedValue
		if json.Unmarshal(traitsData, &traits) == nil {
			data.Traits = traits
		}
	}

	// Load agenda
	if agendaData, err := m.loadFile("agenda.json"); err == nil {
		var agenda []AgendaItem
		if json.Unmarshal(agendaData, &agenda) == nil {
			data.Agenda = agenda
		}
	}

	// Load meta
	if metaData, err := m.loadFile("meta.json"); err == nil {
		var meta L0Meta
		if json.Unmarshal(metaData, &meta) == nil {
			data.Meta = meta
		}
	}

	// Load user_id from meta (backward compat)
	if metaData, err := m.loadFile("meta.json"); err == nil {
		var raw map[string]interface{}
		if json.Unmarshal(metaData, &raw) == nil {
			if uid, ok := raw["user_id"].(string); ok && uid != "" {
				data.UserID = uid
			}
		}
	}

	return data, nil
}

func (m *L0Manager) loadFile(filename string) ([]byte, error) {
	path := filepath.Join(m.dataPath, filename)
	return os.ReadFile(path)
}

func (m *L0Manager) Save(data *L0Data) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveUnlocked(data)
}

func (m *L0Manager) saveUnlocked(data *L0Data) error {
	data.Meta.LastUpdated = time.Now()

	// Marshal traits and agenda to check total size before writing
	traitsData, err := json.MarshalIndent(data.Traits, "", "  ")
	if err != nil {
		return err
	}
	agendaData, err := json.MarshalIndent(data.Agenda, "", "  ")
	if err != nil {
		return err
	}

	totalSize := int64(len(traitsData) + len(agendaData))
	data.Meta.SizeBytes = totalSize

	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(m.dataPath, "traits.json"), traitsData, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.dataPath, "agenda.json"), agendaData, 0644); err != nil {
		return err
	}

	// Save meta (include user_id)
	metaOut := map[string]interface{}{
		"version":        data.Meta.Version,
		"last_updated":   data.Meta.LastUpdated,
		"size_bytes":     data.Meta.SizeBytes,
		"update_trigger": data.Meta.UpdateTrigger,
		"user_id":        data.UserID,
	}
	metaData, err := json.MarshalIndent(metaOut, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dataPath, "meta.json"), metaData, 0644)
}

// IsOversize returns true if current L0 data exceeds L0MaxSizeBytes.
func (m *L0Manager) IsOversize() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return false
	}

	traitsData, err := json.Marshal(data.Traits)
	if err != nil {
		return false
	}
	agendaData, err := json.Marshal(data.Agenda)
	if err != nil {
		return false
	}

	return int64(len(traitsData)+len(agendaData)) > L0MaxSizeBytes
}

// MergeTraits adds or updates a trait value in the timeline.
// If a current value (ValidUntil empty) exists for the same key with the same value, it's a no-op.
// If a current value exists with a different value, the old one gets closed and the new one is appended.
func (m *L0Manager) MergeTraits(category, key string, value TimestampedValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}

	if data.Traits[category] == nil {
		data.Traits[category] = make(map[string][]TimestampedValue)
	}

	timeline := data.Traits[category][key]

	// Check if there's a current value that matches
	for _, existing := range timeline {
		if existing.ValidUntil == "" && existing.Value == value.Value {
			// Same current value, no-op
			return nil
		}
	}

	// Close any current values (set ValidUntil to now)
	now := time.Now().Format("2006-01-02")
	for i := range timeline {
		if timeline[i].ValidUntil == "" {
			timeline[i].ValidUntil = now
		}
	}

	// Set ObservedAt if not already set
	if value.ObservedAt == "" {
		value.ObservedAt = time.Now().Format("2006-01-02")
	}

	timeline = append(timeline, value)
	data.Traits[category][key] = timeline
	data.Meta.UpdateTrigger = "import"

	return m.saveUnlocked(data)
}

// AddAgenda adds or updates an agenda item.
func (m *L0Manager) AddAgenda(item AgendaItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}

	if item.Since == "" {
		item.Since = time.Now().Format("2006-01-02")
	}
	if item.LastUpdated == "" {
		item.LastUpdated = time.Now().Format("2006-01-02")
	}
	if item.Status == "" {
		item.Status = "active"
	}

	data.Agenda = append(data.Agenda, item)
	data.Meta.UpdateTrigger = "import"

	return m.saveUnlocked(data)
}

// Update provides backward-compatible update for CLI and MCP.
func (m *L0Manager) Update(userID, name, context string, preferences map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}

	if userID != "" {
		data.UserID = userID
	}

	now := time.Now().Format("2006-01-02")

	if name != "" {
		m.mergeTraitInto(data, "background", "name", TimestampedValue{
			Value:      name,
			ObservedAt: now,
			Source:     "user_update",
		})
	}
	if context != "" {
		m.mergeTraitInto(data, "background", "context", TimestampedValue{
			Value:      context,
			ObservedAt: now,
			Source:     "user_update",
		})
	}

	if preferences != nil {
		for k, v := range preferences {
			m.mergeTraitInto(data, "personality", k, TimestampedValue{
				Value:      v,
				ObservedAt: now,
				Source:     "user_preference",
			})
		}
	}

	data.Meta.UpdateTrigger = "user_update"
	return m.saveUnlocked(data)
}

// mergeTraitInto merges a trait into data in-memory (without saving).
func (m *L0Manager) mergeTraitInto(data *L0Data, category, key string, value TimestampedValue) {
	if data.Traits[category] == nil {
		data.Traits[category] = make(map[string][]TimestampedValue)
	}

	timeline := data.Traits[category][key]

	// Close existing current values if value changed
	now := time.Now().Format("2006-01-02")
	for i := range timeline {
		if timeline[i].ValidUntil == "" && timeline[i].Value != value.Value {
			timeline[i].ValidUntil = now
		}
	}

	// Check if same value already current
	for _, existing := range timeline {
		if existing.ValidUntil == "" && existing.Value == value.Value {
			return
		}
	}

	if value.ObservedAt == "" {
		value.ObservedAt = now
	}

	timeline = append(timeline, value)
	data.Traits[category][key] = timeline
}

// GetContext returns a human-readable context string for AI conversations.
// Only includes current values (ValidUntil is empty).
func (m *L0Manager) GetContext() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("User: %s\n", data.UserID))

	// Sort categories for stable output
	categories := make([]string, 0, len(data.Traits))
	for cat := range data.Traits {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	for _, category := range categories {
		keys := data.Traits[category]
		currentValues := make(map[string]string)

		for key, timeline := range keys {
			for _, tv := range timeline {
				if tv.ValidUntil == "" {
					val := tv.Value
					if tv.ValidFrom != "" {
						val += fmt.Sprintf(" (since %s)", tv.ValidFrom)
					}
					currentValues[key] = val
				}
			}
		}

		if len(currentValues) > 0 {
			sb.WriteString(fmt.Sprintf("\n%s:\n", strings.Title(category)))
			sortedKeys := make([]string, 0, len(currentValues))
			for k := range currentValues {
				sortedKeys = append(sortedKeys, k)
			}
			sort.Strings(sortedKeys)
			for _, k := range sortedKeys {
				sb.WriteString(fmt.Sprintf("  %s: %s\n", k, currentValues[k]))
			}
		}
	}

	// Agenda
	activeAgenda := []AgendaItem{}
	for _, item := range data.Agenda {
		if item.Status == "active" {
			activeAgenda = append(activeAgenda, item)
		}
	}
	if len(activeAgenda) > 0 {
		sb.WriteString("\nCurrent Focus:\n")
		for _, item := range activeAgenda {
			sb.WriteString(fmt.Sprintf("  - %s (priority: %s", item.Item, item.Priority))
			if item.Since != "" {
				sb.WriteString(fmt.Sprintf(", since %s", item.Since))
			}
			sb.WriteString(")\n")
		}
	}

	return sb.String(), nil
}

// ReplaceAgenda atomically replaces the entire agenda list.
func (m *L0Manager) ReplaceAgenda(items []AgendaItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	data.Agenda = items
	data.Meta.UpdateTrigger = "consolidation"
	return m.saveUnlocked(data)
}

// ReplaceTraits atomically replaces the entire traits map.
func (m *L0Manager) ReplaceTraits(traits map[string]map[string][]TimestampedValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	data.Traits = traits
	data.Meta.UpdateTrigger = "consolidation"
	return m.saveUnlocked(data)
}

// GetAgendaJSON returns current agenda as JSON string for prompt injection.
func (m *L0Manager) GetAgendaJSON() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, err := m.loadUnlocked()
	if err != nil {
		return "[]", err
	}
	bytes, err := json.MarshalIndent(data.Agenda, "", "  ")
	if err != nil {
		return "[]", err
	}
	return string(bytes), nil
}

// GetTraitsJSON returns L0 traits as a JSON string for prompt injection.
func (m *L0Manager) GetTraitsJSON() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return "{}", err
	}

	// Build a simplified view with only current values
	current := make(map[string]map[string]string)
	for category, keys := range data.Traits {
		for key, timeline := range keys {
			for _, tv := range timeline {
				if tv.ValidUntil == "" {
					if current[category] == nil {
						current[category] = make(map[string]string)
					}
					val := tv.Value
					if tv.ValidFrom != "" {
						val += fmt.Sprintf(" (since %s)", tv.ValidFrom)
					}
					current[category][key] = val
				}
			}
		}
	}

	data2, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return "{}", err
	}
	return string(data2), nil
}
