package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"yumem/internal/workspace"
)

// L0MaxSizeBytes is the maximum allowed size for L0 data (50KB).
const L0MaxSizeBytes = 50 * 1024

// Fact represents a single observed fact about the user.
type Fact struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	ObservedAt string `json:"observed_at"`
	Source     string `json:"source,omitempty"`
	SourceName string `json:"source_name,omitempty"`
	CreatedAt  string `json:"created_at"`
	Expired    bool   `json:"expired,omitempty"`
}

// L0Data represents core user information that's always included in conversations.
type L0Data struct {
	UserID string `json:"user_id"`
	Facts  []Fact `json:"facts"`
	Meta   L0Meta `json:"meta"`
}

type L0Meta struct {
	Version       string    `json:"version"`
	LastUpdated   time.Time `json:"last_updated"`
	SizeBytes     int64     `json:"size_bytes"`
	UpdateTrigger string    `json:"update_trigger"`
}

type L0Manager struct {
	mu         sync.RWMutex
	dataPath   string
	historyDir string
}

func NewL0Manager() *L0Manager {
	config := workspace.GetConfig()
	return &L0Manager{
		dataPath:   filepath.Join(config.L0Dir, "current"),
		historyDir: filepath.Join(config.L0Dir, "history"),
	}
}

// SnapshotBeforeConsolidate saves the current facts to a timestamped snapshot file
// in the history directory. This preserves pre-consolidation state.
func (m *L0Manager) SnapshotBeforeConsolidate() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return fmt.Errorf("failed to load L0 for snapshot: %w", err)
	}

	if len(data.Facts) == 0 {
		return nil
	}

	if err := os.MkdirAll(m.historyDir, 0755); err != nil {
		return fmt.Errorf("failed to create history dir: %w", err)
	}

	factsData, err := json.MarshalIndent(data.Facts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal facts for snapshot: %w", err)
	}

	filename := fmt.Sprintf("snapshot_%s.json", time.Now().Format("20060102_150405"))
	path := filepath.Join(m.historyDir, filename)
	if err := os.WriteFile(path, factsData, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	return nil
}

func (m *L0Manager) Load() (*L0Data, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadUnlocked()
}

func (m *L0Manager) loadUnlocked() (*L0Data, error) {
	data := &L0Data{
		UserID: "default",
		Facts:  []Fact{},
		Meta: L0Meta{
			Version:       "3.0.0",
			LastUpdated:   time.Now(),
			UpdateTrigger: "initialization",
		},
	}

	// Try new format first (facts.json)
	if factsData, err := m.loadFile("facts.json"); err == nil {
		var facts []Fact
		if json.Unmarshal(factsData, &facts) == nil {
			data.Facts = facts
		}
	} else {
		// Try legacy migration from traits.json + agenda.json
		migrated := m.migrateFromLegacy(data)
		if migrated {
			// Save in new format (best-effort, don't fail load)
			_ = m.saveUnlocked(data)
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

// migrateFromLegacy reads old traits.json + agenda.json and converts to facts.
// Returns true if migration happened.
func (m *L0Manager) migrateFromLegacy(data *L0Data) bool {
	var migrated bool

	// Migrate traits
	if traitsData, err := m.loadFile("traits.json"); err == nil {
		var traits map[string]map[string][]legacyTimestampedValue
		if json.Unmarshal(traitsData, &traits) == nil {
			now := time.Now().Format("2006-01-02")
			seq := 1
			for category, keys := range traits {
				for key, timeline := range keys {
					for _, tv := range timeline {
						if tv.ValidUntil != "" {
							continue // skip historical values
						}
						fact := Fact{
							ID:         fmt.Sprintf("f-migrated-%03d", seq),
							Text:       tv.Value,
							ObservedAt: tv.ObservedAt,
							Source:     tv.Source,
							SourceName: fmt.Sprintf("migrated from %s/%s", category, key),
							CreatedAt:  now,
						}
						data.Facts = append(data.Facts, fact)
						seq++
					}
				}
			}
			migrated = true
		}
	}

	// Migrate agenda
	if agendaData, err := m.loadFile("agenda.json"); err == nil {
		var agenda []legacyAgendaItem
		if json.Unmarshal(agendaData, &agenda) == nil {
			now := time.Now().Format("2006-01-02")
			seq := len(data.Facts) + 1
			for _, item := range agenda {
				if item.Status != "active" {
					continue
				}
				fact := Fact{
					ID:         fmt.Sprintf("f-migrated-%03d", seq),
					Text:       item.Item,
					ObservedAt: item.Since,
					Source:     item.Source,
					SourceName: "migrated from agenda",
					CreatedAt:  now,
				}
				if fact.ObservedAt == "" {
					fact.ObservedAt = now
				}
				data.Facts = append(data.Facts, fact)
				seq++
			}
			migrated = true
		}
	}

	if migrated {
		data.Meta.Version = "3.0.0"
		data.Meta.UpdateTrigger = "migration"
	}

	return migrated
}

// Legacy types for migration only
type legacyTimestampedValue struct {
	Value      string  `json:"value"`
	ValidFrom  string  `json:"valid_from,omitempty"`
	ValidUntil string  `json:"valid_until,omitempty"`
	ObservedAt string  `json:"observed_at"`
	Confidence float64 `json:"confidence,omitempty"`
	Source     string  `json:"source,omitempty"`
}

type legacyAgendaItem struct {
	Item        string   `json:"item"`
	Priority    string   `json:"priority"`
	Since       string   `json:"since,omitempty"`
	LastUpdated string   `json:"last_updated,omitempty"`
	Status      string   `json:"status"`
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Source      string   `json:"source,omitempty"`
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

	factsData, err := json.MarshalIndent(data.Facts, "", "  ")
	if err != nil {
		return err
	}

	data.Meta.SizeBytes = int64(len(factsData))

	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(m.dataPath, "facts.json"), factsData, 0644); err != nil {
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

	factsData, err := json.Marshal(data.Facts)
	if err != nil {
		return false
	}

	return int64(len(factsData)) > L0MaxSizeBytes
}

// AddFact appends a single fact. Deduplicates by Text+Source.
func (m *L0Manager) AddFact(fact Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}

	// Deduplicate: same Text+Source = skip
	for _, existing := range data.Facts {
		if existing.Text == fact.Text && existing.Source == fact.Source {
			return nil
		}
	}

	if fact.ID == "" {
		fact.ID = fmt.Sprintf("f-%s-%03d", time.Now().Format("20060102"), len(data.Facts)+1)
	}
	if fact.CreatedAt == "" {
		fact.CreatedAt = time.Now().Format("2006-01-02")
	}
	if fact.ObservedAt == "" {
		fact.ObservedAt = time.Now().Format("2006-01-02")
	}

	data.Facts = append(data.Facts, fact)
	data.Meta.UpdateTrigger = "import"

	return m.saveUnlocked(data)
}

// AddFacts appends multiple facts (batch version of AddFact).
func (m *L0Manager) AddFacts(facts []Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}

	added := 0
	for _, fact := range facts {
		// Deduplicate
		dup := false
		for _, existing := range data.Facts {
			if existing.Text == fact.Text && existing.Source == fact.Source {
				dup = true
				break
			}
		}
		if dup {
			continue
		}

		if fact.ID == "" {
			fact.ID = fmt.Sprintf("f-%s-%03d", time.Now().Format("20060102"), len(data.Facts)+1)
		}
		if fact.CreatedAt == "" {
			fact.CreatedAt = time.Now().Format("2006-01-02")
		}
		if fact.ObservedAt == "" {
			fact.ObservedAt = time.Now().Format("2006-01-02")
		}

		data.Facts = append(data.Facts, fact)
		added++
	}

	if added > 0 {
		data.Meta.UpdateTrigger = "import"
		return m.saveUnlocked(data)
	}
	return nil
}

// ReplaceFacts atomically replaces the entire facts list (used by consolidation).
func (m *L0Manager) ReplaceFacts(facts []Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := m.loadUnlocked()
	if err != nil {
		return err
	}
	data.Facts = facts
	data.Meta.UpdateTrigger = "consolidation"
	return m.saveUnlocked(data)
}

// GetFactsJSON returns non-expired facts as a JSON string for prompt injection.
func (m *L0Manager) GetFactsJSON() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return "[]", err
	}

	filtered := m.filterFacts(data.Facts)

	type factView struct {
		Text       string `json:"text"`
		ObservedAt string `json:"observed_at"`
		SourceName string `json:"source_name,omitempty"`
	}

	var views []factView
	for _, f := range filtered {
		views = append(views, factView{
			Text:       f.Text,
			ObservedAt: f.ObservedAt,
			SourceName: f.SourceName,
		})
	}

	bytes, err := json.MarshalIndent(views, "", "  ")
	if err != nil {
		return "[]", err
	}
	return string(bytes), nil
}

// GetFilteredFacts returns facts excluding expired ones.
func (m *L0Manager) GetFilteredFacts() ([]Fact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return nil, err
	}

	return m.filterFacts(data.Facts), nil
}

// filterFacts excludes expired facts.
func (m *L0Manager) filterFacts(facts []Fact) []Fact {
	var result []Fact
	for _, f := range facts {
		if !f.Expired {
			result = append(result, f)
		}
	}
	return result
}

// Update provides backward-compatible update for CLI and MCP.
// name and context are converted to facts.
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
	var newFacts []Fact

	if name != "" {
		newFacts = append(newFacts, Fact{
			ID:         fmt.Sprintf("f-%s-name", now),
			Text:       fmt.Sprintf("Name: %s", name),
			ObservedAt: now,
			Source:     "user_update",
			SourceName: "user setting",
			CreatedAt:  now,
		})
	}
	if context != "" {
		newFacts = append(newFacts, Fact{
			ID:         fmt.Sprintf("f-%s-ctx", now),
			Text:       context,
			ObservedAt: now,
			Source:     "user_update",
			SourceName: "user setting",
			CreatedAt:  now,
		})
	}
	if preferences != nil {
		for k, v := range preferences {
			newFacts = append(newFacts, Fact{
				ID:         fmt.Sprintf("f-%s-pref-%s", now, k),
				Text:       fmt.Sprintf("Preference — %s: %s", k, v),
				ObservedAt: now,
				Source:     "user_preference",
				SourceName: "user setting",
				CreatedAt:  now,
			})
		}
	}

	// Deduplicate and append
	for _, nf := range newFacts {
		dup := false
		for _, existing := range data.Facts {
			if existing.Text == nf.Text && existing.Source == nf.Source {
				dup = true
				break
			}
		}
		if !dup {
			data.Facts = append(data.Facts, nf)
		}
	}

	data.Meta.UpdateTrigger = "user_update"
	return m.saveUnlocked(data)
}

// RemoveFactsBySource removes all facts with the given source ID.
// Returns the number of facts removed.
func (m *L0Manager) RemoveFactsBySource(sourceID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return 0, err
	}

	var kept []Fact
	for _, f := range data.Facts {
		if f.Source != sourceID {
			kept = append(kept, f)
		}
	}

	removed := len(data.Facts) - len(kept)
	if removed > 0 {
		data.Facts = kept
		data.Meta.UpdateTrigger = "reindex"
		if err := m.saveUnlocked(data); err != nil {
			return 0, err
		}
	}

	return removed, nil
}

// GetContext returns a human-readable context string for AI conversations.
func (m *L0Manager) GetContext() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.loadUnlocked()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("User: %s\n", data.UserID))

	filtered := m.filterFacts(data.Facts)
	if len(filtered) > 0 {
		sb.WriteString("\nFacts:\n")
		for _, f := range filtered {
			sb.WriteString(fmt.Sprintf("  - %s", f.Text))
			if f.ObservedAt != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", f.ObservedAt))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}
