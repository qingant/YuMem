package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/memory"
	"yumem/internal/prompts"
	"yumem/internal/retrieval"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "High-level memory operations (core memory, recall)",
	Long:  `Access the high-level memory interface used by chatbots.`,
}

var coreMemoryCmd = &cobra.Command{
	Use:   "core",
	Short: "Show core memory (what a chatbot sees at conversation start)",
	RunE: func(cmd *cobra.Command, args []string) error {
		engine := buildRetrievalEngine()
		result, err := engine.GetCoreMemory()
		if err != nil {
			return fmt.Errorf("failed to get core memory: %w", err)
		}
		fmt.Print(result)
		return nil
	},
}

var recallMemoryCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Recall memories related to a query (AI semantic search)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		maxTopics, _ := cmd.Flags().GetInt("max-topics")

		engine := buildRetrievalEngine()
		result, err := engine.RecallMemory(query, maxTopics)
		if err != nil {
			return fmt.Errorf("recall failed: %w", err)
		}

		if len(result.Entries) == 0 {
			fmt.Println("No relevant entries found.")
		} else {
			fmt.Printf("## Recall Results (%d entries)\n\n", len(result.Entries))
			for i, entry := range result.Entries {
				fmt.Printf("### %d. %s\n", i+1, entry.Title)
				fmt.Printf("**Path**: %s\n", entry.Path)
				if entry.Summary != "" {
					fmt.Printf("**Summary**: %s\n", entry.Summary)
				}
				if entry.Content != "" {
					fmt.Printf("\n%s\n", entry.Content)
				}
				fmt.Println()
			}
		}

		return nil
	},
}

func buildRetrievalEngine() *retrieval.RetrievalEngine {
	l0Manager := memory.NewL0Manager()
	l1Manager := memory.NewL1Manager()
	l2Manager := memory.NewL2Manager()
	promptManager := prompts.NewPromptManager()

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

	return retrieval.NewRetrievalEngine(l0Manager, l1Manager, l2Manager, promptManager, aiManager)
}

func init() {
	rootCmd.AddCommand(memoryCmd)
	memoryCmd.AddCommand(coreMemoryCmd)
	memoryCmd.AddCommand(recallMemoryCmd)

	recallMemoryCmd.Flags().Int("max-topics", 5, "Maximum number of topics to return")
}
