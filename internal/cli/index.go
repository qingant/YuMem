package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/importers"
	"yumem/internal/logging"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

var indexForce bool

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Generate L0/L1 from imported L2 content",
	Long: `Analyze L2 entries with AI and generate L0 facts and L1 semantic index nodes.

By default, only processes L2 entries that haven't been indexed yet.
Use --force to reindex all entries (clears old L0/L1 data first).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runIndex()
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().BoolVar(&indexForce, "force", false, "Reindex all L2 entries (clears old L0/L1 data first)")
}

func runIndex() error {
	logging.Init(2000)
	log := logging.Get()

	// Initialize workspace
	if err := initializeWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	// Initialize all managers
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

	// Create a full BaseImporter (with AI) for AnalyzeAndApply
	bi := importers.NewBaseImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager)

	// Load all L2 entries
	entries, err := l2Manager.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load L2 index: %w", err)
	}

	// Filter entries to process
	type indexEntry struct {
		id    string
		entry *memory.L2Entry
	}
	var toProcess []indexEntry

	for id, entry := range entries {
		if !indexForce && entry.Metadata != nil && entry.Metadata["indexed"] == "true" {
			continue
		}
		toProcess = append(toProcess, indexEntry{id: id, entry: entry})
	}

	if len(toProcess) == 0 {
		fmt.Println("✅ No L2 entries to index.")
		return nil
	}

	fmt.Printf("📊 Found %d L2 entries to index", len(toProcess))
	if indexForce {
		fmt.Printf(" (force mode)")
	}
	fmt.Println()

	result := &importers.ImportResult{
		Errors: []string{},
	}

	for i, item := range toProcess {
		entry := item.entry
		title := entry.Metadata["title"]
		source := entry.Metadata["source"]
		if title == "" {
			title = entry.FilePath
		}

		fmt.Printf("[%d/%d] %s\n", i+1, len(toProcess), title)

		// In force mode, clean old L0/L1 data for this entry
		if indexForce {
			if n, err := l0Manager.RemoveFactsBySource(item.id); err != nil {
				log.Warn("index", fmt.Sprintf("failed to remove L0 facts for %s: %v", item.id, err))
			} else if n > 0 {
				fmt.Printf("  🗑️  Removed %d old L0 facts\n", n)
			}
			if n, err := l1Manager.RemoveNodesByL2Ref(item.id); err != nil {
				log.Warn("index", fmt.Sprintf("failed to remove L1 nodes for %s: %v", item.id, err))
			} else if n > 0 {
				fmt.Printf("  🗑️  Removed %d old L1 nodes\n", n)
			}
		}

		// Read content
		content, err := l2Manager.GetContent(item.id)
		if err != nil {
			errMsg := fmt.Sprintf("failed to read content for %s: %v", item.id, err)
			result.Errors = append(result.Errors, errMsg)
			fmt.Printf("  ❌ %s\n", errMsg)
			continue
		}

		// Recover content_date from metadata
		var contentDate time.Time
		if dateStr := entry.Metadata["content_date"]; dateStr != "" {
			if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
				contentDate = parsed
			}
		}

		// Run AI analysis and apply L0/L1
		if _, err := bi.AnalyzeAndApply(item.id, title, string(content), source, contentDate, result); err != nil {
			errMsg := fmt.Sprintf("indexing failed for %s: %v", title, err)
			result.Errors = append(result.Errors, errMsg)
			fmt.Printf("  ❌ %s\n", errMsg)
		}

		result.TotalProcessed++
		fmt.Println()
	}

	// Post-index consolidation
	if result.L0Updates > 0 || result.TotalProcessed > 0 {
		fmt.Println("🔄 Running L0 consolidation...")
		if cr, err := bi.RunConsolidation(); err != nil {
			fmt.Printf("  ⚠️  L0 consolidation failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ Consolidated: facts %d→%d\n", cr.FactsBefore, cr.FactsAfter)
			if cr.ChangesSummary != "" {
				fmt.Printf("  📝 %s\n", cr.ChangesSummary)
			}
		}
	}

	// Print summary
	fmt.Println()
	fmt.Printf("📊 Index complete: %d processed, %d L0 facts, %d L1 nodes\n",
		result.TotalProcessed, result.L0Updates, result.L1Created)
	if len(result.Errors) > 0 {
		fmt.Printf("   ⚠️  Errors: %d\n", len(result.Errors))
	}

	return nil
}
