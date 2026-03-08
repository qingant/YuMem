package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"yumem/internal/ai"
	"yumem/internal/config"
	"yumem/internal/importers"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

var l0Cmd = &cobra.Command{
	Use:   "l0",
	Short: "Manage L0 (core user information)",
	Long:  `Manage L0 layer which contains core user information that's always included in conversations.`,
}

var l0ShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current L0 context",
	RunE: func(cmd *cobra.Command, args []string) error {
		l0Manager := memory.NewL0Manager()
		context, err := l0Manager.GetContext()
		if err != nil {
			return err
		}
		fmt.Print(context)
		return nil
	},
}

var l0SetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set L0 information",
	Long:  `Set user information in L0 layer. Use flags to specify what to update.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l0Manager := memory.NewL0Manager()
		
		userID, _ := cmd.Flags().GetString("user-id")
		name, _ := cmd.Flags().GetString("name")
		context, _ := cmd.Flags().GetString("context")
		prefStrings, _ := cmd.Flags().GetStringSlice("preference")
		
		// Parse preferences
		var preferences map[string]string
		if len(prefStrings) > 0 {
			preferences = make(map[string]string)
			for _, pref := range prefStrings {
				parts := strings.SplitN(pref, "=", 2)
				if len(parts) == 2 {
					preferences[parts[0]] = parts[1]
				}
			}
		}
		
		err := l0Manager.Update(userID, name, context, preferences)
		if err != nil {
			return err
		}
		
		fmt.Println("L0 information updated successfully")
		return nil
	},
}

var l0ConsolidateCmd = &cobra.Command{
	Use:   "consolidate",
	Short: "Consolidate L0 data (deduplicate facts, mark expired, clean up)",
	Long: `Run AI-driven consolidation on L0 facts:
- Merge duplicate facts
- Mark expired or outdated facts
- Remove sensitive data
- Rewrite judgmental language into factual descriptions`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l0Manager := memory.NewL0Manager()
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

		fmt.Println("🔄 Running L0 consolidation...")

		result, err := importers.ConsolidateL0(l0Manager, promptManager, aiManager)
		if err != nil {
			return fmt.Errorf("consolidation failed: %w", err)
		}

		fmt.Printf("\n✅ Consolidation complete:\n")
		fmt.Printf("   Facts: %d → %d\n", result.FactsBefore, result.FactsAfter)
		if result.ChangesSummary != "" {
			fmt.Printf("\n📝 Changes: %s\n", result.ChangesSummary)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(l0Cmd)
	l0Cmd.AddCommand(l0ShowCmd)
	l0Cmd.AddCommand(l0SetCmd)
	l0Cmd.AddCommand(l0ConsolidateCmd)

	l0SetCmd.Flags().String("user-id", "", "User ID")
	l0SetCmd.Flags().String("name", "", "User name")
	l0SetCmd.Flags().String("context", "", "User context")
	l0SetCmd.Flags().StringSlice("preference", []string{}, "User preferences (key=value format)")
}