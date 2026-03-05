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
		globalConfig.L1Dir, 
		globalConfig.L2Dir,
		filepath.Dir(globalConfig.LogFile),
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