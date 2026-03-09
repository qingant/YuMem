package workspace

import (
	"os"
	"path/filepath"
	"yumem/internal/config"
)

var globalConfig *config.Config

func Initialize(workspaceDir string) error {
	globalConfig = config.GetDefault(workspaceDir)

	// Create necessary directories
	dirs := []string{
		globalConfig.L0Dir,
		filepath.Join(globalConfig.L0Dir, "current"),
		filepath.Join(globalConfig.L0Dir, "history"),
		globalConfig.L1Dir,
		filepath.Join(globalConfig.L1Dir, "nodes"),
		globalConfig.L2Dir,
		filepath.Join(globalConfig.L2Dir, "content"),
		filepath.Join(globalConfig.L2Dir, "entities"),
		filepath.Join(globalConfig.L2Dir, "conversations"),
		filepath.Dir(globalConfig.LogFile),
		filepath.Join(globalConfig.WorkspaceDir, "_yumem", "versions"),
		filepath.Join(globalConfig.WorkspaceDir, "_yumem", "imports"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

func GetConfig() *config.Config {
	return globalConfig
}

func EnsureInitialized() error {
	if globalConfig == nil {
		return Initialize("")
	}
	return nil
}

