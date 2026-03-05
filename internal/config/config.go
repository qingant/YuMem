package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	WorkspaceDir string `yaml:"workspace_dir"`
	L0Dir        string `yaml:"l0_dir"`
	L1Dir        string `yaml:"l1_dir"`
	L2Dir        string `yaml:"l2_dir"`
	LogFile      string `yaml:"log_file"`
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
	}
}