package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/importers"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

var (
	importAll              bool
	importPath             string
	importRecursive        bool
	importTypes            []string
	importLimit            int
	importForce            bool
	importAllText          bool
	importAsConversation   bool
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import content from various sources",
	Long:  `Import content from Apple Notes, files, or directories into YuMem memory layers.`,
}

var importNotesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Import Apple Notes",
	Long:  `Import notes from Apple Notes application into YuMem memory layers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return importAppleNotes()
	},
}

var importFilesCmd = &cobra.Command{
	Use:   "files [path]",
	Short: "Import files and directories",
	Long:  `Import files and directories into YuMem memory layers with AI-powered analysis.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := importPath
		if len(args) > 0 {
			path = args[0]
		}
		if path == "" {
			path = "."
		}
		return importFiles(path)
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.AddCommand(importNotesCmd)
	importCmd.AddCommand(importFilesCmd)

	// Notes import flags
	importNotesCmd.Flags().BoolVar(&importAll, "all", false, "Import all notes")
	importNotesCmd.Flags().IntVar(&importLimit, "limit", 0, "Limit number of notes to import (0 = no limit)")
	importNotesCmd.Flags().BoolVar(&importForce, "force", false, "Force full re-import, skip incremental check")

	// Files import flags
	importFilesCmd.Flags().StringVar(&importPath, "path", "", "Path to import from")
	importFilesCmd.Flags().BoolVar(&importRecursive, "recursive", true, "Import recursively")
	importFilesCmd.Flags().StringSliceVar(&importTypes, "types", []string{}, "File types to import (e.g., txt,md,go)")
	importFilesCmd.Flags().BoolVar(&importForce, "force", false, "Force full re-import, skip incremental check")
	importFilesCmd.Flags().BoolVar(&importAllText, "all-text", false, "Import all text files (detect by content, not extension)")
	importFilesCmd.Flags().BoolVar(&importAsConversation, "as-conversation", false, "Import file as a conversation (AI parses message structure)")
}

func importAppleNotes() error {
	fmt.Println("🍎 Importing Apple Notes (L2 storage only)...")

	// Initialize workspace if needed
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	l2Manager := memory.NewL2Manager()
	importer := importers.NewAppleNotesImporterL2Only(l2Manager)

	fmt.Println("📋 Checking Apple Notes availability...")

	if importLimit > 0 {
		fmt.Printf("🔢 Limiting import to %d notes\n", importLimit)
	}
	if importForce {
		fmt.Printf("🔄 Force mode: re-importing all notes\n")
	}

	notesConfig := importers.NotesImportConfig{
		LimitCount: importLimit,
		Force:      importForce,
	}
	results, err := importer.Import(notesConfig)
	if err != nil {
		if err.Error() == "Apple Notes.app is not available on this system" {
			fmt.Printf("❌ Apple Notes is not available on this system\n")
			fmt.Printf("💡 This feature requires macOS with Apple Notes.app installed\n")
			return nil
		}
		return fmt.Errorf("failed to import Apple Notes: %w", err)
	}

	fmt.Printf("✅ Import completed!\n")
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2Created)
	if results.Skipped > 0 {
		fmt.Printf("   ⏭️  Skipped (unchanged): %d\n", results.Skipped)
	}

	if len(results.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors: %d\n", len(results.Errors))
		for i, errorMsg := range results.Errors {
			if i < 5 {
				fmt.Printf("   - %s\n", errorMsg)
			}
		}
		if len(results.Errors) > 5 {
			fmt.Printf("   ... and %d more errors\n", len(results.Errors)-5)
		}
	}

	return nil
}

func importFiles(path string) error {
	// Check if path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", path)
	}

	// Initialize workspace if needed
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	if importAsConversation {
		return importFileAsConversation(path)
	}

	fmt.Printf("📁 Importing files from: %s (L2 storage only)\n", path)

	l2Manager := memory.NewL2Manager()
	importer := importers.NewFilesystemImporterL2Only(l2Manager)

	options := importers.ImportOptions{
		Recursive:    importRecursive,
		FileTypes:    importTypes,
		MaxSize:      10 * 1024 * 1024,
		AllTextFiles: importAllText,
		Force:        importForce,
	}

	fmt.Printf("🔍 Scanning for files...")
	if importRecursive {
		fmt.Printf(" (recursive)")
	}
	if importAllText {
		fmt.Printf(" (all text files)")
	} else if len(importTypes) > 0 {
		fmt.Printf(" (types: %v)", importTypes)
	}
	fmt.Println()

	ctx := context.Background()
	results, err := importer.ImportPath(ctx, path, options)
	if err != nil {
		return fmt.Errorf("failed to import files: %w", err)
	}

	fmt.Printf("✅ Import completed!\n")
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2Created)
	if results.Skipped > 0 {
		fmt.Printf("   ⏭️  Skipped (unchanged): %d\n", results.Skipped)
	}

	if len(results.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors: %d\n", len(results.Errors))
	}

	return nil
}

func importFileAsConversation(path string) error {
	fmt.Printf("💬 Importing as conversation: %s\n", path)

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()

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

	bi := importers.NewBaseImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager)

	item := importers.ImportItem{
		Title:   filepath.Base(path),
		Content: string(content),
		Source:  "filesystem",
	}
	result := &importers.ImportResult{Errors: []string{}}

	l2ID, err := bi.StoreAsConversation(item, result)
	if err != nil {
		return fmt.Errorf("failed to import as conversation: %w", err)
	}

	fmt.Printf("✅ Conversation imported: %s\n", l2ID)
	fmt.Printf("   💡 Run 'yumem index' to generate L0/L1 from conversation\n")

	return nil
}

