package importers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"yumem/internal/ai"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type NotesImporter struct {
	*BaseImporter
}

type NotesImportConfig struct {
	FolderFilter string `json:"folder_filter"` // Optional: filter by folder name
	LimitCount   int    `json:"limit_count"`   // Optional: limit number of notes to import
}

type AppleNote struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Folder       string    `json:"folder"`
	CreationDate time.Time `json:"creation_date"`
	ModifiedDate time.Time `json:"modified_date"`
}

func NewNotesImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager) *NotesImporter {
	return &NotesImporter{
		BaseImporter: NewBaseImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager),
	}
}

// NewAppleNotesImporter creates a new Apple Notes importer (alias for backward compatibility)
func NewAppleNotesImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager) *NotesImporter {
	// Create managers we need
	promptManager := prompts.NewPromptManager()
	promptManager.Initialize()
	
	aiManager := ai.NewManager()
	// Add local provider as fallback
	aiManager.AddProvider("local", ai.NewLocalProvider())
	
	return NewNotesImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager)
}

func (ni *NotesImporter) Import(config NotesImportConfig) (*ImportResult, error) {
	result := &ImportResult{
		TotalProcessed: 0,
		L0Updates:      0,
		L1Created:      0,
		L2Created:      0,
		Errors:         []string{},
		Details:        make(map[string]interface{}),
	}

	// Check if we're on macOS and Notes.app is available
	if !ni.isNotesAppAvailable() {
		return nil, fmt.Errorf("Apple Notes.app is not available on this system")
	}

	// Extract notes using AppleScript
	notes, err := ni.extractNotes(config)
	if err != nil {
		return nil, fmt.Errorf("failed to extract notes: %w", err)
	}

	// Process each note
	for _, note := range notes {
		if err := ni.processNote(note, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error processing note '%s': %v", note.Title, err))
		}
	}

	result.Details["config"] = config
	result.Details["notes_found"] = len(notes)
	result.Details["completed_at"] = time.Now()

	return result, nil
}

func (ni *NotesImporter) isNotesAppAvailable() bool {
	// Check if we can run AppleScript
	cmd := exec.Command("osascript", "-e", "tell application \"Notes\" to get name")
	err := cmd.Run()
	return err == nil
}

func (ni *NotesImporter) extractNotes(config NotesImportConfig) ([]AppleNote, error) {
	// Build AppleScript to extract notes
	script := `
	tell application "Notes"
		set noteList to {}
		set allNotes to every note
		repeat with currentNote in allNotes
			try
				set noteRecord to {id:(id of currentNote), title:(name of currentNote), body:(body of currentNote), folder:(name of folder of currentNote), creationDate:(creation date of currentNote), modifiedDate:(modification date of currentNote)}
				set end of noteList to noteRecord
			on error
				-- Skip notes that can't be accessed
			end try
		end repeat
		return noteList
	end tell
	`

	// Execute AppleScript
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute AppleScript: %w", err)
	}

	// Parse the output (this is a simplified version)
	// In a real implementation, you'd want more robust parsing
	return ni.parseNotesOutput(string(output), config)
}

func (ni *NotesImporter) parseNotesOutput(output string, config NotesImportConfig) ([]AppleNote, error) {
	var notes []AppleNote

	// This is a very simplified parser
	// In reality, you'd need a more sophisticated approach to parse AppleScript output
	lines := strings.Split(output, "\n")
	
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Create a dummy note for demonstration
		// In a real implementation, you'd properly parse the AppleScript output
		note := AppleNote{
			ID:           fmt.Sprintf("note_%d", i),
			Title:        fmt.Sprintf("Note %d", i),
			Body:         line,
			Folder:       "Notes",
			CreationDate: time.Now(),
			ModifiedDate: time.Now(),
		}

		// Apply filters
		if config.FolderFilter != "" && note.Folder != config.FolderFilter {
			continue
		}

		notes = append(notes, note)

		// Apply limit
		if config.LimitCount > 0 && len(notes) >= config.LimitCount {
			break
		}
	}

	return notes, nil
}

func (ni *NotesImporter) processNote(note AppleNote, result *ImportResult) error {
	// Convert to ImportItem
	item := ImportItem{
		ID:         note.ID,
		Title:      note.Title,
		Content:    note.Body,
		Source:     "apple_notes",
		CreatedAt:  note.CreationDate.Format(time.RFC3339),
		ModifiedAt: note.ModifiedDate.Format(time.RFC3339),
		Metadata: map[string]string{
			"folder":    note.Folder,
			"note_id":   note.ID,
		},
	}

	// Analyze content
	analysis, err := ni.AnalyzeContent(item)
	if err != nil {
		return fmt.Errorf("failed to analyze content: %w", err)
	}

	// Process based on analysis
	if err := ni.ProcessAnalysisResult(item, analysis, result); err != nil {
		return fmt.Errorf("failed to process analysis result: %w", err)
	}

	result.TotalProcessed++
	return nil
}

func (ni *NotesImporter) ImportFolders() ([]string, error) {
	// Extract folder names from Notes.app
	script := `
	tell application "Notes"
		set folderNames to {}
		set allFolders to every folder
		repeat with currentFolder in allFolders
			set end of folderNames to name of currentFolder
		end repeat
		return folderNames
	end tell
	`

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to extract folder names: %w", err)
	}

	// Parse folder names
	folders := strings.Split(strings.TrimSpace(string(output)), ", ")
	var cleanFolders []string
	for _, folder := range folders {
		clean := strings.Trim(folder, "\"")
		if clean != "" {
			cleanFolders = append(cleanFolders, clean)
		}
	}

	return cleanFolders, nil
}

// ImportAll imports all Apple Notes (convenience method for CLI)
func (ni *NotesImporter) ImportAll(ctx context.Context) (*ImportResult, error) {
	config := NotesImportConfig{
		LimitCount: 0, // Import all notes
	}
	return ni.Import(config)
}