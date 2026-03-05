package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"yumem/internal/workspace"
)

// L0Data represents core user information that's always included in conversations
type L0Data struct {
	UserID string `json:"user_id"`
	
	// Long-term personal characteristics
	LongTermTraits struct {
		Personality     map[string]TimestampedValue `json:"personality"`
		Philosophy      map[string]TimestampedValue `json:"philosophy"`
		Background      map[string]TimestampedValue `json:"background"`
		Skills          map[string]TimestampedValue `json:"skills"`
	} `json:"long_term_traits"`
	
	// Recent focus and agenda items
	RecentAgenda struct {
		CurrentFocus    []AgendaItem `json:"current_focus"`
		CompletedItems  []AgendaItem `json:"completed_items"`
		OnHoldItems     []AgendaItem `json:"on_hold_items"`
	} `json:"recent_agenda"`
	
	// System metadata
	Meta struct {
		Version        string    `json:"version"`
		LastUpdated    time.Time `json:"last_updated"`
		SizeBytes      int64     `json:"size_bytes"`
		UpdateTrigger  string    `json:"update_trigger"`
	} `json:"meta"`
}

type TimestampedValue struct {
	Value       string    `json:"value"`
	UpdatedAt   time.Time `json:"updated_at"`
	Confidence  float64   `json:"confidence"`
	Source      string    `json:"source"`
}

type AgendaItem struct {
	Item        string    `json:"item"`
	Priority    string    `json:"priority"` // high, medium, low
	Since       time.Time `json:"since"`
	LastUpdated time.Time `json:"last_updated"`
	Status      string    `json:"status"` // active, paused, completed
	Context     string    `json:"context"`
	Tags        []string  `json:"tags"`
}

type L0Manager struct {
	dataPath string
}

func NewL0Manager() *L0Manager {
	config := workspace.GetConfig()
	return &L0Manager{
		dataPath: filepath.Join(config.L0Dir, "current"),
	}
}

func (m *L0Manager) Load() (*L0Data, error) {
	// Try to load from subcategory files
	return m.loadFromSubcategories()
}

func (m *L0Manager) loadFromSubcategories() (*L0Data, error) {
	data := &L0Data{
		UserID: "default",
		LongTermTraits: struct {
			Personality map[string]TimestampedValue `json:"personality"`
			Philosophy  map[string]TimestampedValue `json:"philosophy"`
			Background  map[string]TimestampedValue `json:"background"`
			Skills      map[string]TimestampedValue `json:"skills"`
		}{
			Personality: make(map[string]TimestampedValue),
			Philosophy:  make(map[string]TimestampedValue),
			Background:  make(map[string]TimestampedValue),
			Skills:      make(map[string]TimestampedValue),
		},
		RecentAgenda: struct {
			CurrentFocus   []AgendaItem `json:"current_focus"`
			CompletedItems []AgendaItem `json:"completed_items"`
			OnHoldItems    []AgendaItem `json:"on_hold_items"`
		}{
			CurrentFocus:   []AgendaItem{},
			CompletedItems: []AgendaItem{},
			OnHoldItems:    []AgendaItem{},
		},
		Meta: struct {
			Version       string    `json:"version"`
			LastUpdated   time.Time `json:"last_updated"`
			SizeBytes     int64     `json:"size_bytes"`
			UpdateTrigger string    `json:"update_trigger"`
		}{
			Version:       "1.0.0",
			LastUpdated:   time.Now(),
			SizeBytes:     0,
			UpdateTrigger: "initialization",
		},
	}

	// Load traits
	if traitsData, err := m.loadSubcategory("traits.json"); err == nil {
		var traits struct {
			Personality map[string]TimestampedValue `json:"personality"`
			Philosophy  map[string]TimestampedValue `json:"philosophy"`
			Background  map[string]TimestampedValue `json:"background"`
			Skills      map[string]TimestampedValue `json:"skills"`
		}
		if json.Unmarshal(traitsData, &traits) == nil {
			data.LongTermTraits = traits
		}
	}

	// Load agenda
	if agendaData, err := m.loadSubcategory("agenda.json"); err == nil {
		var agenda struct {
			CurrentFocus   []AgendaItem `json:"current_focus"`
			CompletedItems []AgendaItem `json:"completed_items"`
			OnHoldItems    []AgendaItem `json:"on_hold_items"`
		}
		if json.Unmarshal(agendaData, &agenda) == nil {
			data.RecentAgenda = agenda
		}
	}

	// Load meta
	if metaData, err := m.loadSubcategory("meta.json"); err == nil {
		var meta struct {
			Version       string    `json:"version"`
			LastUpdated   time.Time `json:"last_updated"`
			SizeBytes     int64     `json:"size_bytes"`
			UpdateTrigger string    `json:"update_trigger"`
			UserID        string    `json:"user_id"`
		}
		if json.Unmarshal(metaData, &meta) == nil {
			data.Meta.Version = meta.Version
			data.Meta.LastUpdated = meta.LastUpdated
			data.Meta.SizeBytes = meta.SizeBytes
			data.Meta.UpdateTrigger = meta.UpdateTrigger
			if meta.UserID != "" {
				data.UserID = meta.UserID
			}
		}
	}

	return data, nil
}

func (m *L0Manager) loadSubcategory(filename string) ([]byte, error) {
	path := filepath.Join(m.dataPath, filename)
	return os.ReadFile(path)
}

func (m *L0Manager) Save(data *L0Data) error {
	data.Meta.LastUpdated = time.Now()
	
	// Ensure directory exists
	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}
	
	// Save subcategories
	if err := m.saveTraits(data); err != nil {
		return err
	}
	if err := m.saveAgenda(data); err != nil {
		return err
	}
	if err := m.saveMeta(data); err != nil {
		return err
	}
	
	return nil
}

func (m *L0Manager) saveTraits(data *L0Data) error {
	traitsData, err := json.MarshalIndent(data.LongTermTraits, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dataPath, "traits.json"), traitsData, 0644)
}

func (m *L0Manager) saveAgenda(data *L0Data) error {
	agendaData, err := json.MarshalIndent(data.RecentAgenda, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dataPath, "agenda.json"), agendaData, 0644)
}

func (m *L0Manager) saveMeta(data *L0Data) error {
	meta := struct {
		Version       string    `json:"version"`
		LastUpdated   time.Time `json:"last_updated"`
		SizeBytes     int64     `json:"size_bytes"`
		UpdateTrigger string    `json:"update_trigger"`
		UserID        string    `json:"user_id"`
	}{
		Version:       data.Meta.Version,
		LastUpdated:   data.Meta.LastUpdated,
		SizeBytes:     data.Meta.SizeBytes,
		UpdateTrigger: data.Meta.UpdateTrigger,
		UserID:        data.UserID,
	}
	
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dataPath, "meta.json"), metaData, 0644)
}

func (m *L0Manager) Update(userID, name, context string, preferences map[string]string) error {
	data, err := m.Load()
	if err != nil {
		return err
	}

	if userID != "" {
		data.UserID = userID
	}
	
	// Store name and context in background traits
	if name != "" {
		data.LongTermTraits.Background["name"] = TimestampedValue{
			Value:     name,
			UpdatedAt: time.Now(),
			Source:    "user_update",
		}
	}
	if context != "" {
		data.LongTermTraits.Background["context"] = TimestampedValue{
			Value:     context,
			UpdatedAt: time.Now(),
			Source:    "user_update",
		}
	}
	
	// Store preferences in personality traits
	if preferences != nil {
		for k, v := range preferences {
			data.LongTermTraits.Personality[k] = TimestampedValue{
				Value:     v,
				UpdatedAt: time.Now(),
				Source:    "user_preference",
			}
		}
	}

	return m.Save(data)
}

func (m *L0Manager) GetContext() (string, error) {
	data, err := m.Load()
	if err != nil {
		return "", err
	}

	contextStr := fmt.Sprintf("User: %s\n", data.UserID)
	
	// Add name if available
	if nameValue, exists := data.LongTermTraits.Background["name"]; exists {
		contextStr += fmt.Sprintf("Name: %s\n", nameValue.Value)
	}
	
	// Add context if available
	if contextValue, exists := data.LongTermTraits.Background["context"]; exists {
		contextStr += fmt.Sprintf("Context: %s\n", contextValue.Value)
	}
	
	// Add personality traits
	if len(data.LongTermTraits.Personality) > 0 {
		contextStr += "Personality:\n"
		for k, v := range data.LongTermTraits.Personality {
			contextStr += fmt.Sprintf("  %s: %s\n", k, v.Value)
		}
	}

	return contextStr, nil
}