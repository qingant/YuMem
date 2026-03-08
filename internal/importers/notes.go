package importers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type NotesImporter struct {
	*BaseImporter
}

type NotesImportConfig struct {
	FolderFilter string `json:"folder_filter"`
	LimitCount   int    `json:"limit_count"`
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

// NewAppleNotesImporter creates a new Apple Notes importer with config from ~/.yumem.yaml
func NewAppleNotesImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager) *NotesImporter {
	promptManager := prompts.NewPromptManager()
	promptManager.Initialize()

	aiManager := ai.NewManager()
	cfg := config.LoadFromFile("")
	aiProviders := make(map[string]ai.ProviderConfig)
	for name, pc := range cfg.AI.Providers {
		aiProviders[name] = ai.ProviderConfig{
			Type:    pc.Type,
			APIKey:  pc.APIKey,
			BaseURL: pc.BaseURL,
			Model:   pc.Model,
		}
	}
	aiManager.InitializeFromConfig(cfg.AI.DefaultProvider, aiProviders)

	return NewNotesImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager)
}

func (ni *NotesImporter) Import(cfg NotesImportConfig) (*ImportResult, error) {
	result := &ImportResult{
		Errors: []string{},
	}

	if !ni.isNotesAppAvailable() {
		return nil, fmt.Errorf("Apple Notes.app is not available on this system")
	}

	notes, err := ni.extractNotes(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to extract notes: %w", err)
	}

	fmt.Printf("\n📥 Processing %d notes...\n\n", len(notes))

	for i, note := range notes {
		fmt.Printf("[%d/%d] %s\n", i+1, len(notes), note.Title)

		item := ImportItem{
			ID:          note.ID,
			Title:       note.Title,
			Content:     note.Body,
			Source:      "apple_notes",
			ContentDate: note.CreationDate,
		}

		if err := ni.ProcessItem(item, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error processing '%s': %v", note.Title, err))
			fmt.Printf("  ❌ %v\n", err)
		}
		result.TotalProcessed++
		fmt.Println()
	}

	// Post-import L0 consolidation
	if result.L0Updates > 0 || len(notes) > 0 {
		fmt.Println("🔄 Running L0 consolidation...")
		if cr, err := ni.RunConsolidation(); err != nil {
			fmt.Printf("  ⚠️  L0 consolidation failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ Consolidated: facts %d→%d\n",
				cr.FactsBefore, cr.FactsAfter)
			if cr.ChangesSummary != "" {
				fmt.Printf("  📝 %s\n", cr.ChangesSummary)
			}
		}
	}

	return result, nil
}

func (ni *NotesImporter) ImportAll(ctx context.Context) (*ImportResult, error) {
	return ni.Import(NotesImportConfig{})
}

func (ni *NotesImporter) isNotesAppAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "osascript", "-e", "tell application \"Notes\" to get name")
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		fmt.Printf("⏰ Timeout: AppleScript took too long\n")
		return false
	}

	if err != nil {
		fmt.Printf("❌ AppleScript error: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("   Output: %s\n", string(output))
		}
		return false
	}

	fmt.Printf("✅ Notes.app accessible\n")
	return true
}

const notesPageSize = 200

func (ni *NotesImporter) extractNotes(cfg NotesImportConfig) ([]AppleNote, error) {
	// Get total count
	countScript := `tell application "Notes" to count every note`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	countCmd := exec.CommandContext(ctx, "osascript", "-e", countScript)
	countOutput, err := countCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get note count: %w", err)
	}

	var totalCount int
	fmt.Sscanf(strings.TrimSpace(string(countOutput)), "%d", &totalCount)
	fmt.Printf("📊 Total notes in Notes.app: %d\n", totalCount)

	// Determine how many notes to extract
	extractCount := totalCount
	if cfg.LimitCount > 0 && cfg.LimitCount < extractCount {
		extractCount = cfg.LimitCount
		fmt.Printf("🔢 Limiting to %d notes\n", extractCount)
	}

	if extractCount == 0 {
		return nil, nil
	}

	// Calculate pages
	totalPages := (extractCount + notesPageSize - 1) / notesPageSize

	// AppleScript date format helper
	dateFormatScript := `
				set noteCreated to creation date of currentNote
				set noteModified to modification date of currentNote
				set cYear to year of noteCreated as string
				set cMonth to text -2 thru -1 of ("0" & ((month of noteCreated as integer) as string))
				set cDay to text -2 thru -1 of ("0" & (day of noteCreated as string))
				set createdStr to cYear & "-" & cMonth & "-" & cDay
				set mYear to year of noteModified as string
				set mMonth to text -2 thru -1 of ("0" & ((month of noteModified as integer) as string))
				set mDay to text -2 thru -1 of ("0" & (day of noteModified as string))
				set modifiedStr to mYear & "-" & mMonth & "-" & mDay
	`

	var allNotes []AppleNote

	for page := 0; page < totalPages; page++ {
		startIdx := page*notesPageSize + 1 // AppleScript is 1-indexed
		endIdx := startIdx + notesPageSize - 1
		if endIdx > extractCount {
			endIdx = extractCount
		}

		fmt.Printf("⏳ Extracting page %d/%d (notes %d-%d)...\n", page+1, totalPages, startIdx, endIdx)

		script := fmt.Sprintf(`
		tell application "Notes"
			set noteList to ""
			set allNotes to every note
			repeat with i from %d to %d
				try
					set currentNote to item i of allNotes
					set noteTitle to name of currentNote as string
					set noteID to id of currentNote as string
					set noteBody to body of currentNote as string
					try
						set noteFolder to name of folder of currentNote as string
					on error
						set noteFolder to "Notes"
					end try
					%s
					set noteList to noteList & noteID & "|" & noteTitle & "|" & noteFolder & "|" & createdStr & "|" & modifiedStr & "|" & noteBody & "|||END|||" & "\n"
				end try
			end repeat
			return noteList
		end tell
		`, startIdx, endIdx, dateFormatScript)

		pageCtx, pageCancel := context.WithTimeout(context.Background(), 120*time.Second)
		cmd := exec.CommandContext(pageCtx, "osascript", "-e", script)
		output, err := cmd.CombinedOutput()
		pageCancel()

		if pageCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout extracting page %d/%d (notes %d-%d, >120s)", page+1, totalPages, startIdx, endIdx)
		}
		if err != nil {
			return nil, fmt.Errorf("AppleScript extraction failed on page %d: %w\nOutput: %s", page+1, err, string(output))
		}

		pageNotes, err := ni.parseNotesOutput(string(output), NotesImportConfig{})
		if err != nil {
			return nil, fmt.Errorf("failed to parse page %d: %w", page+1, err)
		}

		allNotes = append(allNotes, pageNotes...)
		fmt.Printf("  ✅ Page %d: %d notes extracted\n", page+1, len(pageNotes))
	}

	fmt.Printf("✅ Extraction completed: %d notes total\n", len(allNotes))
	return allNotes, nil
}

func (ni *NotesImporter) parseNotesOutput(output string, cfg NotesImportConfig) ([]AppleNote, error) {
	var notes []AppleNote
	output = strings.TrimSpace(output)
	if output == "" {
		return notes, nil
	}

	entries := strings.Split(output, "|||END|||\n")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, "|", 6)
		if len(parts) < 6 {
			continue
		}

		body := ni.cleanHTMLContent(strings.TrimSpace(parts[5]))

		creationDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[3]))
		if err != nil {
			creationDate = time.Now()
		}
		modifiedDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[4]))
		if err != nil {
			modifiedDate = time.Now()
		}

		notes = append(notes, AppleNote{
			ID:           strings.TrimSpace(parts[0]),
			Title:        strings.TrimSpace(parts[1]),
			Body:         body,
			Folder:       strings.TrimSpace(parts[2]),
			CreationDate: creationDate,
			ModifiedDate: modifiedDate,
		})

		if cfg.LimitCount > 0 && len(notes) >= cfg.LimitCount {
			break
		}
	}

	fmt.Printf("📝 Parsed %d notes\n", len(notes))
	return notes, nil
}

func (ni *NotesImporter) cleanHTMLContent(content string) string {
	replacements := map[string]string{
		"<div>": "", "</div>": "\n", "<br>": "\n",
		"<h1>": "", "</h1>": "\n", "<h2>": "", "</h2>": "\n",
		"<h3>": "", "</h3>": "\n", "<p>": "", "</p>": "\n",
		"<span>": "", "</span>": "", "<b>": "", "</b>": "",
		"<i>": "", "</i>": "", "<u>": "", "</u>": "",
		"\u00a0": " ", "│": "",
	}
	for old, new := range replacements {
		content = strings.ReplaceAll(content, old, new)
	}

	content = strings.TrimSuffix(content, "|||END|||")

	var lines []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func (ni *NotesImporter) ImportFolders() ([]string, error) {
	script := `
	tell application "Notes"
		set folderNames to {}
		repeat with currentFolder in every folder
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

	var folders []string
	for _, f := range strings.Split(strings.TrimSpace(string(output)), ", ") {
		clean := strings.Trim(f, "\"")
		if clean != "" {
			folders = append(folders, clean)
		}
	}
	return folders, nil
}
