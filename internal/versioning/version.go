package versioning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"yumem/internal/workspace"
)

type VersionManager struct {
	versionsDir string
	manifestPath string
}

type Manifest struct {
	WorkspaceVersion string          `json:"workspace_version"`
	CreatedAt        time.Time       `json:"created_at"`
	LastUpdated      time.Time       `json:"last_updated"`
	VersionHistory   []VersionRecord `json:"version_history"`
	L0Config         L0Config        `json:"l0_config"`
}

type VersionRecord struct {
	Version   string                 `json:"version"`
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"` // major, minor, patch
	Changes   map[string]interface{} `json:"changes"`
	SizeStats SizeStats              `json:"size_stats"`
}

type L0Config struct {
	SizeLimit struct {
		MinKB     int     `json:"min_kb"`
		MaxKB     int     `json:"max_kb"`
		CurrentKB float64 `json:"current_kb"`
	} `json:"size_limit"`
	Subcategories    []string `json:"subcategories"`
	VersionRetention int      `json:"version_retention"`
}

type SizeStats struct {
	L0TotalKB     float64 `json:"l0_total_kb"`
	L1NodesCount  int     `json:"l1_nodes_count"`
	L2EntryCount  int     `json:"l2_entries_count"`
}

func NewVersionManager() *VersionManager {
	config := workspace.GetConfig()
	versionsDir := filepath.Join(config.WorkspaceDir, "_yumem", "versions")
	
	return &VersionManager{
		versionsDir:  versionsDir,
		manifestPath: filepath.Join(versionsDir, "manifest.json"),
	}
}

func (vm *VersionManager) Initialize() error {
	if err := os.MkdirAll(vm.versionsDir, 0755); err != nil {
		return err
	}
	
	if err := os.MkdirAll(filepath.Join(vm.versionsDir, "history"), 0755); err != nil {
		return err
	}
	
	// Create initial manifest if it doesn't exist
	if _, err := os.Stat(vm.manifestPath); os.IsNotExist(err) {
		manifest := &Manifest{
			WorkspaceVersion: "1.0.0",
			CreatedAt:        time.Now(),
			LastUpdated:      time.Now(),
			VersionHistory:   []VersionRecord{},
			L0Config: L0Config{
				SizeLimit: struct {
					MinKB     int     `json:"min_kb"`
					MaxKB     int     `json:"max_kb"`
					CurrentKB float64 `json:"current_kb"`
				}{
					MinKB:     2,
					MaxKB:     10,
					CurrentKB: 0,
				},
				Subcategories:    []string{"traits", "agenda", "meta"},
				VersionRetention: 10,
			},
		}
		return vm.saveManifest(manifest)
	}
	
	return nil
}

func (vm *VersionManager) LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(vm.manifestPath)
	if err != nil {
		return nil, err
	}
	
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	
	return &manifest, nil
}

func (vm *VersionManager) saveManifest(manifest *Manifest) error {
	manifest.LastUpdated = time.Now()
	
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(vm.manifestPath, data, 0644)
}

func (vm *VersionManager) CreateVersion(changeType, description string, changes map[string]interface{}) (*VersionRecord, error) {
	manifest, err := vm.LoadManifest()
	if err != nil {
		return nil, err
	}
	
	// Generate new version number
	newVersion := vm.generateNextVersion(manifest.WorkspaceVersion, changeType)
	
	record := VersionRecord{
		Version:   newVersion,
		Timestamp: time.Now(),
		Type:      changeType,
		Changes:   changes,
		SizeStats: vm.calculateCurrentStats(),
	}
	
	// Save version history
	if err := vm.saveVersionHistory(record); err != nil {
		return nil, err
	}
	
	// Update manifest
	manifest.WorkspaceVersion = newVersion
	manifest.VersionHistory = append(manifest.VersionHistory, record)
	
	// Keep only recent versions
	if len(manifest.VersionHistory) > manifest.L0Config.VersionRetention {
		manifest.VersionHistory = manifest.VersionHistory[len(manifest.VersionHistory)-manifest.L0Config.VersionRetention:]
	}
	
	if err := vm.saveManifest(manifest); err != nil {
		return nil, err
	}
	
	return &record, nil
}

func (vm *VersionManager) generateNextVersion(current, changeType string) string {
	// Simple version increment logic
	switch changeType {
	case "major":
		return fmt.Sprintf("%.0f.0.0", parseVersionMajor(current)+1)
	case "minor":
		return fmt.Sprintf("%.0f.%.0f.0", parseVersionMajor(current), parseVersionMinor(current)+1)
	default: // patch
		return fmt.Sprintf("%.0f.%.0f.%.0f", parseVersionMajor(current), parseVersionMinor(current), parseVersionPatch(current)+1)
	}
}

func parseVersionMajor(version string) float64 {
	// Simplified version parsing
	return 1.0 // TODO: implement proper version parsing
}

func parseVersionMinor(version string) float64 {
	return 0.0 // TODO: implement proper version parsing
}

func parseVersionPatch(version string) float64 {
	return 0.0 // TODO: implement proper version parsing
}

func (vm *VersionManager) calculateCurrentStats() SizeStats {
	// TODO: implement actual stats calculation
	return SizeStats{
		L0TotalKB:    0,
		L1NodesCount: 0,
		L2EntryCount: 0,
	}
}

func (vm *VersionManager) saveVersionHistory(record VersionRecord) error {
	filename := fmt.Sprintf("v%s.json", record.Version)
	path := filepath.Join(vm.versionsDir, "history", filename)
	
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

func (vm *VersionManager) GetVersionHistory(limit int) ([]VersionRecord, error) {
	manifest, err := vm.LoadManifest()
	if err != nil {
		return nil, err
	}
	
	history := manifest.VersionHistory
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	
	return history, nil
}