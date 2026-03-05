package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"yumem/internal/importers"
	"yumem/internal/memory"
)

var (
	importAll      bool
	importPath     string
	importRecursive bool
	importTypes    []string
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

	// Files import flags
	importFilesCmd.Flags().StringVar(&importPath, "path", "", "Path to import from")
	importFilesCmd.Flags().BoolVar(&importRecursive, "recursive", true, "Import recursively")
	importFilesCmd.Flags().StringSliceVar(&importTypes, "types", []string{}, "File types to import (e.g., txt,md,go)")
}

func importAppleNotes() error {
	fmt.Println("🍎 Importing Apple Notes...")
	
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
	
	ctx := context.Background()
	results, err := importer.ImportAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to import Apple Notes: %w", err)
	}
	
	fmt.Printf("✅ Import completed successfully!\n")
	fmt.Printf("   📝 Notes processed: %d\n", results.TotalProcessed)
	fmt.Printf("   🧠 L1 nodes created: %d\n", results.L1Created)
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2Created)
	if len(results.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors encountered: %d\n", len(results.Errors))
	}
	
	return nil
}

func importFiles(path string) error {
	fmt.Printf("📁 Importing files from: %s\n", path)
	
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
	
	ctx := context.Background()
	results, err := importer.ImportPath(ctx, path, options)
	if err != nil {
		return fmt.Errorf("failed to import files: %w", err)
	}
	
	fmt.Printf("✅ Import completed successfully!\n")
	fmt.Printf("   📂 Files processed: %d\n", len(results.Items))
	fmt.Printf("   🧠 L1 nodes created: %d\n", results.L1NodesCreated)
	fmt.Printf("   📄 L2 entries created: %d\n", results.L2EntriesCreated)
	fmt.Printf("   ⚠️  Files skipped: %d\n", results.SkippedFiles)
	
	// Show some examples if verbose
	if len(results.Items) > 0 {
		fmt.Println("\n📋 Sample imported content:")
		for i, item := range results.Items {
			if i >= 3 { // Show max 3 examples
				break
			}
			fmt.Printf("   - %s (%s)\n", item.Title, item.Type)
		}
		if len(results.Items) > 3 {
			fmt.Printf("   ... and %d more\n", len(results.Items)-3)
		}
	}
	
	return nil
}