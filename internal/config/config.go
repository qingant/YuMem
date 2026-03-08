package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	WorkspaceDir string    `yaml:"workspace_dir"`
	L0Dir        string    `yaml:"l0_dir"`
	L1Dir        string    `yaml:"l1_dir"`
	L2Dir        string    `yaml:"l2_dir"`
	LogFile      string    `yaml:"log_file"`
	AI           AIConfig  `yaml:"ai"`
}

type AIConfig struct {
	DefaultProvider string                    `yaml:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Type    string `yaml:"type"`    // "openai", "claude", "local"
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url,omitempty"`
	Model   string `yaml:"model,omitempty"`
}

// LoadFromFile loads configuration from ~/.yumem.yaml using Viper's manual
// extraction (Viper's Unmarshal doesn't work well with nested AI config structs).
// Falls back to defaults if the config file doesn't exist.
func LoadFromFile(workspaceDir string) *Config {
	cfg := GetDefault(workspaceDir)

	v := viper.New()
	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}

	v.AddConfigPath(home)
	v.SetConfigType("yaml")
	v.SetConfigName(".yumem")

	if err := v.ReadInConfig(); err != nil {
		return cfg
	}

	if dp := v.GetString("ai.default_provider"); dp != "" {
		cfg.AI.DefaultProvider = dp
	}

	providers := v.GetStringMapString("ai.providers")
	if len(providers) > 0 {
		cfg.AI.Providers = make(map[string]ProviderConfig)
		for name := range providers {
			cfg.AI.Providers[name] = ProviderConfig{
				Type:    v.GetString(fmt.Sprintf("ai.providers.%s.type", name)),
				APIKey:  v.GetString(fmt.Sprintf("ai.providers.%s.api_key", name)),
				BaseURL: v.GetString(fmt.Sprintf("ai.providers.%s.base_url", name)),
				Model:   v.GetString(fmt.Sprintf("ai.providers.%s.model", name)),
			}
		}
	}

	if ws := v.GetString("workspace_dir"); ws != "" {
		cfg.WorkspaceDir = ws
		cfg.L0Dir = filepath.Join(ws, "_yumem", "l0")
		cfg.L1Dir = filepath.Join(ws, "_yumem", "l1")
		cfg.L2Dir = filepath.Join(ws, "_yumem", "l2")
		cfg.LogFile = filepath.Join(ws, "_yumem", "logs", "yumem.log")
	}

	return cfg
}

func GetDefault(workspaceDir string) *Config {
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}

	return &Config{
		WorkspaceDir: workspaceDir,
		L0Dir:        filepath.Join(workspaceDir, "_yumem", "l0"),
		L1Dir:        filepath.Join(workspaceDir, "_yumem", "l1"),
		L2Dir:        filepath.Join(workspaceDir, "_yumem", "l2"),
		LogFile:      filepath.Join(workspaceDir, "_yumem", "logs", "yumem.log"),
		AI: AIConfig{
			DefaultProvider: "gemini",
			Providers: map[string]ProviderConfig{
				"local": {
					Type: "local",
				},
				"gemini": {
					Type:  "gemini",
					Model: "gemini-1.5-flash",
				},
			},
		},
	}
}