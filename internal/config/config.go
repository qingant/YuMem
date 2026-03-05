package config

import (
	"os"
	"path/filepath"
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

func GetDefault(workspaceDir string) *Config {
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}

	return &Config{
		WorkspaceDir: workspaceDir,
		L0Dir:        filepath.Join(workspaceDir, ".yumem", "l0"),
		L1Dir:        filepath.Join(workspaceDir, ".yumem", "l1"),
		L2Dir:        filepath.Join(workspaceDir, ".yumem", "l2"),
		LogFile:      filepath.Join(workspaceDir, "request_log.jsonl"),
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