package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"yumem/internal/config"
)

var (
	aiProvider string
	aiAPIKey   string
	aiModel    string
	aiBaseURL  string
)

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "Manage AI provider configuration",
	Long:  `Configure AI providers for content analysis and context assembly.`,
}

var aiSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup an AI provider",
	Long:  `Setup an AI provider with API key and configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setupAIProvider()
	},
}

var aiListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured AI providers",
	Long:  `List all configured AI providers and their status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listAIProviders()
	},
}

func init() {
	rootCmd.AddCommand(aiCmd)
	aiCmd.AddCommand(aiSetupCmd)
	aiCmd.AddCommand(aiListCmd)

	aiSetupCmd.Flags().StringVar(&aiProvider, "provider", "", "AI provider (openai, claude, gemini, github-copilot)")
	aiSetupCmd.Flags().StringVar(&aiAPIKey, "api-key", "", "API key for the provider")
	aiSetupCmd.Flags().StringVar(&aiModel, "model", "", "Default model to use (optional)")
	aiSetupCmd.Flags().StringVar(&aiBaseURL, "base-url", "", "Custom base URL (optional)")
	aiSetupCmd.MarkFlagRequired("provider")
	
	// Only require API key for non-local providers
	aiSetupCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if aiProvider != "local" && aiAPIKey == "" {
			return fmt.Errorf("api-key is required for provider %s", aiProvider)
		}
		return nil
	}
}

func setupAIProvider() error {
	if aiProvider == "" {
		return fmt.Errorf("provider is required")
	}
	
	if aiProvider != "local" && aiAPIKey == "" {
		return fmt.Errorf("api-key is required for provider %s", aiProvider)
	}

	// Load or create config
	configPath := getConfigPath()
	cfg, err := loadOrCreateConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize AI config if needed
	if cfg.AI.Providers == nil {
		cfg.AI.Providers = make(map[string]config.ProviderConfig)
	}

	// Add/update provider
	providerConfig := config.ProviderConfig{
		Type:   aiProvider,
		APIKey: aiAPIKey,
	}

	if aiModel != "" {
		providerConfig.Model = aiModel
	}

	if aiBaseURL != "" {
		providerConfig.BaseURL = aiBaseURL
	}

	cfg.AI.Providers[aiProvider] = providerConfig

	// Set as default if it's the first provider
	if cfg.AI.DefaultProvider == "" || cfg.AI.DefaultProvider == "local" {
		cfg.AI.DefaultProvider = aiProvider
	}

	// Save config
	if err := saveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✓ AI provider '%s' configured successfully\n", aiProvider)
	if cfg.AI.DefaultProvider == aiProvider {
		fmt.Printf("✓ Set as default provider\n")
	}

	return nil
}

func listAIProviders() error {
	configPath := getConfigPath()
	cfg, err := loadOrCreateConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("🤖 AI Provider Configuration:")
	fmt.Printf("   Default Provider: %s\n", cfg.AI.DefaultProvider)
	fmt.Println()

	if len(cfg.AI.Providers) == 0 {
		fmt.Println("   No providers configured")
		fmt.Println()
		fmt.Println("💡 To configure a provider:")
		fmt.Println("   yumem ai setup --provider openai --api-key YOUR_API_KEY")
		fmt.Println("   yumem ai setup --provider claude --api-key YOUR_API_KEY")
		return nil
	}

	for name, provider := range cfg.AI.Providers {
		fmt.Printf("   ├─ %s (%s)\n", name, provider.Type)
		if provider.Model != "" {
			fmt.Printf("   │   Model: %s\n", provider.Model)
		}
		if provider.BaseURL != "" {
			fmt.Printf("   │   Base URL: %s\n", provider.BaseURL)
		}
		if provider.APIKey != "" {
			fmt.Printf("   │   API Key: %s***\n", provider.APIKey[:min(8, len(provider.APIKey))])
		}
		if name == cfg.AI.DefaultProvider {
			fmt.Printf("   │   Status: ✓ Default\n")
		}
		fmt.Println()
	}

	return nil
}

func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".yumem.yaml"
	}
	return filepath.Join(home, ".yumem.yaml")
}

func loadOrCreateConfig(configPath string) (*config.Config, error) {
	cfg := config.LoadFromFile("")
	return cfg, nil
}

func saveConfig(configPath string, cfg *config.Config) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600) // Secure permissions for API keys
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}