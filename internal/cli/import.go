package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"yumem/internal/importers"
	"yumem/internal/memory"
)

var (
	importAll      bool
	importPath     string
	importRecursive bool
	importTypes    []string
	importLimit    int
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

	// Files import flags
	importFilesCmd.Flags().StringVar(&importPath, "path", "", "Path to import from")
	importFilesCmd.Flags().BoolVar(&importRecursive, "recursive", true, "Import recursively")
	importFilesCmd.Flags().StringSliceVar(&importTypes, "types", []string{}, "File types to import (e.g., txt,md,go)")
}

func importAppleNotes() error {
	fmt.Println("🍎 Importing Apple Notes...")
	
	// Check if we have AI providers configured
	if err := validateAIConfiguration(); err != nil {
		return err
	}
	
	// Initialize workspace if needed
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}
	
	// Initialize memory managers
	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()
	
	// Create Apple Notes importer
	importer := importers.NewAppleNotesImporter(l0Manager, l1Manager, l2Manager)
	
	fmt.Println("📋 Checking Apple Notes availability...")
	
	// Add a timeout for the entire import process
	ctx, cancel := context.WithTimeout(context.Background(), 5*60*time.Second) // 5 minute timeout
	defer cancel()
	
	fmt.Printf("⏱️  Starting import with 5-minute timeout...\n")
	if importLimit > 0 {
		fmt.Printf("🔢 Limiting import to %d notes\n", importLimit)
	}
	
	// Use ImportWithLimit if limit is specified
	var results *importers.ImportResult
	var err error
	
	if importLimit > 0 {
		config := importers.NotesImportConfig{
			LimitCount: importLimit,
		}
		results, err = importer.Import(config)
	} else {
		results, err = importer.ImportAll(ctx)
	}
	if err != nil {
		if err.Error() == "Apple Notes.app is not available on this system" {
			fmt.Printf("❌ Apple Notes is not available on this system\n")
			fmt.Printf("💡 This feature requires macOS with Apple Notes.app installed\n")
			return nil
		}
		return fmt.Errorf("failed to import Apple Notes: %w", err)
	}
	
	fmt.Printf("✅ Import completed successfully!\n")
	fmt.Printf("   📝 Notes processed: %d\n", results.TotalProcessed)
	fmt.Printf("   🧠 L1 nodes created: %d\n", results.L1Created)
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2Created)
	
	if len(results.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors encountered: %d\n", len(results.Errors))
		fmt.Println("\n📋 Error details:")
		for i, errorMsg := range results.Errors {
			if i < 5 { // Show first 5 errors
				fmt.Printf("   - %s\n", errorMsg)
			}
		}
		if len(results.Errors) > 5 {
			fmt.Printf("   ... and %d more errors\n", len(results.Errors)-5)
		}
	}
	
	if results.L0Updates > 0 {
		fmt.Printf("   🧠 L0 traits updated: %d\n", results.L0Updates)
	}

	return nil
}

func importFiles(path string) error {
	fmt.Printf("📁 Importing files from: %s\n", path)
	
	// Check if we have AI providers configured
	if err := validateAIConfiguration(); err != nil {
		return err
	}
	
	// Check if path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", path)
	}
	
	// Initialize workspace if needed
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}
	
	// Initialize memory managers
	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()
	
	// Create filesystem importer
	importer := importers.NewFilesystemImporter(l0Manager, l1Manager, l2Manager)
	
	// Configure import options
	options := importers.ImportOptions{
		Recursive:  importRecursive,
		FileTypes:  importTypes,
		MaxSize:    10 * 1024 * 1024, // 10MB max file size
	}
	
	fmt.Printf("🔍 Scanning for files...")
	if importRecursive {
		fmt.Printf(" (recursive)")
	}
	if len(importTypes) > 0 {
		fmt.Printf(" (types: %v)", importTypes)
	}
	fmt.Println()
	
	ctx := context.Background()
	results, err := importer.ImportPath(ctx, path, options)
	if err != nil {
		return fmt.Errorf("failed to import files: %w", err)
	}

	fmt.Printf("✅ Import completed successfully!\n")
	fmt.Printf("   📂 Files processed: %d\n", results.TotalProcessed)
	fmt.Printf("   🧠 L1 nodes created: %d\n", results.L1Created)
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2Created)

	if results.L0Updates > 0 {
		fmt.Printf("   🧠 L0 traits updated: %d\n", results.L0Updates)
	}

	if len(results.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors: %d\n", len(results.Errors))
	}

	return nil
}

// validateAIConfiguration checks if AI providers are configured
func validateAIConfiguration() error {
	fmt.Println("🤖 Checking AI configuration...")
	
	// Load configuration
	configPath := getConfigPath()
	cfg, err := loadOrCreateConfig(configPath)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not load AI configuration (%v)\n", err)
		fmt.Printf("💡 Using local heuristics for content analysis\n")
		return nil
	}
	
	// Check if we have any configured providers
	hasConfiguredProviders := false
	for _, provider := range cfg.AI.Providers {
		if provider.Type != "local" && provider.APIKey != "" {
			hasConfiguredProviders = true
			break
		}
		if provider.Type == "github-copilot" {
			// For GitHub Copilot, we might have OAuth tokens instead of API key
			hasConfiguredProviders = true
			break
		}
	}
	
	if !hasConfiguredProviders {
		fmt.Printf("⚠️  No AI providers configured\n")
		fmt.Printf("💡 Content will be processed using basic heuristics\n")
		fmt.Printf("🔧 To configure AI providers:\n")
		fmt.Printf("   - Web dashboard: %s/ai-config\n", "http://localhost:3000")
		fmt.Printf("   - CLI: yumem ai setup --provider gemini --api-key YOUR_KEY\n")
		fmt.Printf("\n⏳ Continuing with local processing...\n")
	} else {
		fmt.Printf("✅ AI providers configured - using intelligent analysis\n")
	}
	
	return nil
}