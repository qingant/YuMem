package importers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	
	"github.com/spf13/viper"
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
	
	// Initialize AI manager with configuration
	aiManager := ai.NewManager()
	cfg := loadConfigFromFile()
	fmt.Printf("🔧 Initializing AI providers with config: %+v\n", cfg.AI)
	initializeAIProviders(aiManager, cfg)
	fmt.Printf("🔧 Available providers after initialization: %v\n", aiManager.ListProviders())
	
	return NewNotesImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager)
}

// loadConfigFromFile loads configuration from ~/.yumem.yaml file
func loadConfigFromFile() *config.Config {
	// Set up viper to read config file
	home, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".yumem")
		
		// Try to read config file
		if err := viper.ReadInConfig(); err == nil {
			fmt.Printf("🔧 Config file loaded: %s\n", viper.ConfigFileUsed())
			fmt.Printf("🔧 Raw config data: ai.default_provider=%s\n", viper.GetString("ai.default_provider"))
			fmt.Printf("🔧 Raw config data: ai.providers.gemini.api_key=%s\n", viper.GetString("ai.providers.gemini.api_key"))
			
			// Manual config extraction since viper unmarshal has issues with nested structs
			cfg := config.GetDefault("")
			
			// Extract AI configuration manually
			cfg.AI.DefaultProvider = viper.GetString("ai.default_provider")
			cfg.AI.Providers = make(map[string]config.ProviderConfig)
			
			// Get provider names from viper
			providers := viper.GetStringMapString("ai.providers")
			for providerName := range providers {
				cfg.AI.Providers[providerName] = config.ProviderConfig{
					Type:    viper.GetString(fmt.Sprintf("ai.providers.%s.type", providerName)),
					APIKey:  viper.GetString(fmt.Sprintf("ai.providers.%s.api_key", providerName)),
					BaseURL: viper.GetString(fmt.Sprintf("ai.providers.%s.base_url", providerName)),
					Model:   viper.GetString(fmt.Sprintf("ai.providers.%s.model", providerName)),
				}
			}
			
			return cfg
		} else {
			fmt.Printf("🔧 Config read error: %v\n", err)
		}
	}
	
	// Fallback to default config if file reading fails
	return config.GetDefault("")
}

// initializeAIProviders sets up AI providers based on configuration
func initializeAIProviders(aiManager *ai.Manager, cfg *config.Config) {
	// Always add local provider as fallback
	aiManager.AddProvider("local", ai.NewLocalProvider())
	
	// Add configured providers
	for name, providerConfig := range cfg.AI.Providers {
		switch providerConfig.Type {
		case "openai":
			if providerConfig.APIKey != "" {
				provider := ai.NewOpenAIProvider(providerConfig.APIKey)
				if providerConfig.BaseURL != "" {
					provider.BaseURL = providerConfig.BaseURL
				}
				aiManager.AddProvider(name, provider)
			}
		case "claude":
			if providerConfig.APIKey != "" {
				provider := ai.NewClaudeProvider(providerConfig.APIKey)
				if providerConfig.BaseURL != "" {
					provider.BaseURL = providerConfig.BaseURL
				}
				aiManager.AddProvider(name, provider)
			}
		case "gemini":
			if providerConfig.APIKey != "" {
				provider := ai.NewGeminiProvider(providerConfig.APIKey)
				if providerConfig.BaseURL != "" {
					provider.BaseURL = providerConfig.BaseURL
				}
				aiManager.AddProvider(name, provider)
			}
		case "github-copilot":
			if providerConfig.APIKey != "" {
				provider := ai.NewGitHubCopilotProvider(providerConfig.APIKey)
				if providerConfig.BaseURL != "" {
					provider.BaseURL = providerConfig.BaseURL
				}
				aiManager.AddProvider(name, provider)
			}
		case "local":
			// Already added above
		}
	}
	
	// Set default provider
	if cfg.AI.DefaultProvider != "" {
		aiManager.SetDefaultProvider(cfg.AI.DefaultProvider)
	}
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
	// Check if we can run AppleScript with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	fmt.Printf("🔍 Testing AppleScript access to Notes.app...\n")
	cmd := exec.CommandContext(ctx, "osascript", "-e", "tell application \"Notes\" to get name")
	
	// Run command and capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Printf("⏰ Timeout: AppleScript command took too long (>10s)\n")
		fmt.Printf("💡 This might be because:\n")
		fmt.Printf("   - Notes.app requires permission prompt\n")
		fmt.Printf("   - macOS is waiting for user interaction\n")
		fmt.Printf("   - Notes.app is not responding\n")
		return false
	}
	
	if err != nil {
		fmt.Printf("❌ AppleScript error: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("   Output: %s\n", string(output))
		}
		
		// Check for common error patterns
		outputStr := string(output)
		if strings.Contains(outputStr, "not authorized") || strings.Contains(outputStr, "permission") {
			fmt.Printf("🔒 Permission issue detected\n")
			fmt.Printf("💡 To fix this:\n")
			fmt.Printf("   1. Go to System Preferences → Security & Privacy → Privacy\n")
			fmt.Printf("   2. Select 'Automation' from the left side\n")
			fmt.Printf("   3. Find 'yumem' and enable access to Notes\n")
		} else if strings.Contains(outputStr, "application isn't running") {
			fmt.Printf("📱 Notes.app is not running\n")
			fmt.Printf("💡 Trying to launch Notes.app...\n")
			
			// Try to launch Notes.app
			launchCtx, launchCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer launchCancel()
			launchCmd := exec.CommandContext(launchCtx, "open", "-a", "Notes")
			if launchErr := launchCmd.Run(); launchErr != nil {
				fmt.Printf("❌ Failed to launch Notes.app: %v\n", launchErr)
			} else {
				fmt.Printf("✅ Notes.app launched, please try import again\n")
			}
		}
		return false
	}
	
	fmt.Printf("✅ AppleScript access to Notes.app successful\n")
	fmt.Printf("   Response: %s\n", strings.TrimSpace(string(output)))
	return true
}

func (ni *NotesImporter) extractNotes(config NotesImportConfig) ([]AppleNote, error) {
	fmt.Printf("📝 Extracting notes from Notes.app...\n")
	
	// First, get count of notes to set expectations
	countScript := `tell application "Notes" to count every note`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	countCmd := exec.CommandContext(ctx, "osascript", "-e", countScript)
	countOutput, err := countCmd.Output()
	if err == nil {
		fmt.Printf("📊 Total notes found: %s\n", strings.TrimSpace(string(countOutput)))
	}
	
	// Build AppleScript that respects the limit from the start
	var script string
	if config.LimitCount > 0 {
		fmt.Printf("🔢 Limiting extraction to first %d notes\n", config.LimitCount)
		script = fmt.Sprintf(`
		tell application "Notes"
			set noteList to ""
			set allNotes to every note
			set noteCount to count of allNotes
			if noteCount > %d then set noteCount to %d
			repeat with i from 1 to noteCount
				try
					set currentNote to item i of allNotes
					set noteTitle to name of currentNote as string
					set noteID to id of currentNote as string
					set noteBody to body of currentNote as string
					-- Try to get folder name safely
					try
						set noteFolder to name of folder of currentNote as string
					on error
						set noteFolder to "Notes"
					end try
					-- Use delimiter format: ID|TITLE|FOLDER|BODY (with special separator for body)
					set noteList to noteList & noteID & "|" & noteTitle & "|" & noteFolder & "|" & noteBody & "|||END|||" & "\n"
				on error errorMsg
					-- Skip notes that can't be accessed
					log "Skipped note " & i & ": " & errorMsg
				end try
			end repeat
			return noteList
		end tell
		`, config.LimitCount, config.LimitCount)
	} else {
		script = `
		tell application "Notes"
			set noteList to ""
			set allNotes to every note
			set noteCount to count of allNotes
			repeat with i from 1 to noteCount
				try
					set currentNote to item i of allNotes
					set noteTitle to name of currentNote as string
					set noteID to id of currentNote as string
					set noteBody to body of currentNote as string
					-- Try to get folder name safely
					try
						set noteFolder to name of folder of currentNote as string
					on error
						set noteFolder to "Notes"
					end try
					-- Use delimiter format: ID|TITLE|FOLDER|BODY (with special separator for body)
					set noteList to noteList & noteID & "|" & noteTitle & "|" & noteFolder & "|" & noteBody & "|||END|||" & "\n"
				on error errorMsg
					-- Skip notes that can't be accessed
					log "Skipped note " & i & ": " & errorMsg
				end try
				-- Progress feedback every 100 notes
				if i mod 100 = 0 then
					display notification "Processed " & i & " of " & noteCount & " notes" with title "YuMem Import"
				end if
			end repeat
			return noteList
		end tell
		`
	}

	// Execute AppleScript with longer timeout and progress feedback
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Second) // 5 minute timeout
	defer cancel2()
	
	fmt.Printf("⏳ Running AppleScript to extract note metadata (timeout: 5min)...\n")
	fmt.Printf("💡 This may take a while if you have many notes. Check for macOS notifications for progress.\n")
	
	cmd := exec.CommandContext(ctx2, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	
	if ctx2.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("timeout while extracting notes (>5min) - you have too many notes. Try using a smaller batch or contact support")
	}
	
	if err != nil {
		fmt.Printf("❌ AppleScript extraction failed: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("   Output: %s\n", string(output))
		}
		
		// Try a fallback approach with just basic info
		fmt.Printf("🔄 Trying fallback approach with minimal data...\n")
		return ni.extractNotesBasic(config)
	}

	fmt.Printf("✅ AppleScript extraction completed\n")
	fmt.Printf("📊 Raw output length: %d characters\n", len(output))
	fmt.Printf("🔍 Raw output content: %q\n", string(output))
	
	// Parse the output (this is a simplified version)
	// In a real implementation, you'd want more robust parsing
	return ni.parseNotesOutput(string(output), config)
}

func (ni *NotesImporter) extractNotesBasic(config NotesImportConfig) ([]AppleNote, error) {
	fmt.Printf("📝 Using basic extraction method...\n")
	
	// Very simple approach - just get note titles and IDs
	script := `
	tell application "Notes"
		set noteList to {}
		set allNotes to every note
		repeat with currentNote in allNotes
			try
				set noteRecord to (name of currentNote)
				set end of noteList to noteRecord
			on error
				-- Skip notes that can't be accessed
			end try
		end repeat
		return noteList
	end tell
	`

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // 2 minute timeout
	defer cancel()
	
	fmt.Printf("⏳ Running basic AppleScript extraction...\n")
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("even basic extraction timed out - Notes.app may be unresponsive")
	}
	
	if err != nil {
		fmt.Printf("❌ Basic extraction also failed: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("   Output: %s\n", string(output))
		}
		return nil, fmt.Errorf("all extraction methods failed: %w", err)
	}

	fmt.Printf("✅ Basic extraction completed\n")
	
	// Parse simple output
	return ni.parseBasicNotesOutput(string(output), config)
}

func (ni *NotesImporter) parseBasicNotesOutput(output string, config NotesImportConfig) ([]AppleNote, error) {
	var notes []AppleNote
	
	// Split by commas and clean up
	titles := strings.Split(output, ",")
	
	for i, title := range titles {
		title = strings.TrimSpace(title)
		title = strings.Trim(title, "\"")
		
		if title == "" {
			continue
		}

		note := AppleNote{
			ID:           fmt.Sprintf("note_%d", i),
			Title:        title,
			Body:         "Content extraction skipped due to timeout",
			Folder:       "Unknown",
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

	fmt.Printf("📊 Parsed %d notes from basic extraction\n", len(notes))
	return notes, nil
}

func (ni *NotesImporter) parseNotesOutput(output string, config NotesImportConfig) ([]AppleNote, error) {
	var notes []AppleNote
	
	fmt.Printf("🔍 Parsing AppleScript output...\n")
	
	// Clean up the output - remove whitespace
	output = strings.TrimSpace(output)
	
	if output == "" {
		fmt.Printf("⚠️  Empty output from AppleScript\n")
		return notes, nil
	}
	
	fmt.Printf("📝 Processing delimited format (ID|TITLE|FOLDER|BODY)...\n")
	
	// Split by lines, but be careful with body content that might have newlines
	entries := strings.Split(output, "|||END|||\n")
	
	for i, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		
		// Parse the delimited format: ID|TITLE|FOLDER|BODY
		parts := strings.SplitN(entry, "|", 4) // Split into at most 4 parts
		if len(parts) < 4 {
			preview := entry
			if len(entry) > 50 {
				preview = entry[:50] + "..."
			}
			fmt.Printf("⚠️  Skipping malformed entry %d (parts: %d): %s\n", i+1, len(parts), preview)
			continue
		}
		
		// Extract the body content (everything after the 3rd pipe)
		bodyContent := strings.TrimSpace(parts[3])
		
		// Clean up HTML content from Apple Notes
		bodyContent = ni.cleanHTMLContent(bodyContent)
		
		note := AppleNote{
			ID:           strings.TrimSpace(parts[0]),
			Title:        strings.TrimSpace(parts[1]),
			Body:         bodyContent,
			Folder:       strings.TrimSpace(parts[2]),
			CreationDate: time.Now(),
			ModifiedDate: time.Now(),
		}
		
		// Apply filters
		if config.FolderFilter != "" && note.Folder != config.FolderFilter {
			continue
		}
		
		notes = append(notes, note)
		
		// Apply limit (should already be handled by AppleScript, but double-check)
		if config.LimitCount > 0 && len(notes) >= config.LimitCount {
			break
		}
	}
	
	fmt.Printf("✅ Successfully parsed %d notes from AppleScript output\n", len(notes))
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
	fmt.Printf("🔍 Analyzing note: %s\n", item.Title)
	analysis, err := ni.AnalyzeContent(item)
	if err != nil {
		return fmt.Errorf("failed to analyze content: %w", err)
	}
	
	fmt.Printf("📊 Analysis result: layer=%s, path=%s\n", analysis.StorageLayer, analysis.Path)

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

// cleanHTMLContent removes HTML tags and cleans up Apple Notes content
func (ni *NotesImporter) cleanHTMLContent(content string) string {
	// Remove common Apple Notes HTML tags
	content = strings.ReplaceAll(content, "<div>", "")
	content = strings.ReplaceAll(content, "</div>", "\n")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "<h1>", "")
	content = strings.ReplaceAll(content, "</h1>", "\n")
	content = strings.ReplaceAll(content, "<h2>", "")
	content = strings.ReplaceAll(content, "</h2>", "\n")
	content = strings.ReplaceAll(content, "<h3>", "")
	content = strings.ReplaceAll(content, "</h3>", "\n")
	content = strings.ReplaceAll(content, "<p>", "")
	content = strings.ReplaceAll(content, "</p>", "\n")
	content = strings.ReplaceAll(content, "<span>", "")
	content = strings.ReplaceAll(content, "</span>", "")
	content = strings.ReplaceAll(content, "<b>", "")
	content = strings.ReplaceAll(content, "</b>", "")
	content = strings.ReplaceAll(content, "<i>", "")
	content = strings.ReplaceAll(content, "</i>", "")
	content = strings.ReplaceAll(content, "<u>", "")
	content = strings.ReplaceAll(content, "</u>", "")
	
	// Remove non-breaking spaces and other special characters
	content = strings.ReplaceAll(content, "\u00a0", " ")
	content = strings.ReplaceAll(content, "│", "")
	
	// Remove the end delimiter that might still be present
	content = strings.TrimSuffix(content, "|||END|||")
	
	// Clean up multiple newlines and whitespace
	lines := strings.Split(content, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != "|||END|||" {
			cleanLines = append(cleanLines, line)
		}
	}
	
	return strings.Join(cleanLines, "\n")
}

// ImportAll imports all Apple Notes (convenience method for CLI)
func (ni *NotesImporter) ImportAll(ctx context.Context) (*ImportResult, error) {
	config := NotesImportConfig{
		LimitCount: 0, // Import all notes
	}
	return ni.Import(config)
}