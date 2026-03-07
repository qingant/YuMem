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

		// Print profile
		fmt.Println(result.Profile)

		// Print topics
		if len(result.RelevantTopics) == 0 {
			fmt.Println("\nNo relevant topics found.")
		} else {
			fmt.Printf("\n## Relevant Topics (%d found)\n\n", len(result.RelevantTopics))
			for i, topic := range result.RelevantTopics {
				fmt.Printf("### %d. %s\n", i+1, topic.Title)
				fmt.Printf("**Path**: %s\n", topic.Path)
				if topic.Summary != "" {
					fmt.Printf("**Summary**: %s\n", topic.Summary)
				}
				if topic.Content != "" {
					fmt.Printf("\n%s\n", topic.Content)
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
