package importers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"yumem/internal/ai"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type FilesystemImporter struct {
	*BaseImporter
}

type FilesystemImportConfig struct {
	RootPath       string   `json:"root_path"`
	IncludeExts    []string `json:"include_extensions"`
	ExcludeExts    []string `json:"exclude_extensions"`
	MaxFileSize    int64    `json:"max_file_size_bytes"`
	FollowSymlinks bool     `json:"follow_symlinks"`
	Recursive      bool     `json:"recursive"`
}

func NewFilesystemImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager) *FilesystemImporter {
	promptManager := prompts.NewPromptManager()
	promptManager.Initialize()

	aiManager := ai.NewManager()
	cfg := loadConfigFromFile()
	initializeAIProviders(aiManager, cfg)

	return &FilesystemImporter{
		BaseImporter: NewBaseImporter(l0Manager, l1Manager, l2Manager, promptManager, aiManager),
	}
}

func (fi *FilesystemImporter) Import(config FilesystemImportConfig) (*ImportResult, error) {
	result := &ImportResult{
		Errors: []string{},
	}

	if _, err := os.Stat(config.RootPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("root path does not exist: %s", config.RootPath)
	}

	if len(config.IncludeExts) == 0 {
		config.IncludeExts = []string{".txt", ".md", ".json", ".yaml", ".yml", ".go", ".py", ".js", ".ts"}
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 1024 * 1024 // 1MB default
	}

	err := fi.walkDirectory(config.RootPath, config, result)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return result, nil
}

func (fi *FilesystemImporter) walkDirectory(root string, config FilesystemImportConfig, result *ImportResult) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error walking %s: %v", path, err))
			return nil
		}

		if info.IsDir() {
			if !config.Recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 && !config.FollowSymlinks {
			return nil
		}

		if info.Size() > config.MaxFileSize {
			result.Errors = append(result.Errors, fmt.Sprintf("File too large, skipping: %s (%d bytes)", path, info.Size()))
			return nil
		}

		if !fi.shouldIncludeFile(path, config) {
			return nil
		}

		if err := fi.processFile(path, info, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error processing %s: %v", path, err))
		}

		return nil
	})
}

func (fi *FilesystemImporter) shouldIncludeFile(filePath string, config FilesystemImportConfig) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	for _, excludeExt := range config.ExcludeExts {
		if ext == strings.ToLower(excludeExt) {
			return false
		}
	}

	for _, includeExt := range config.IncludeExts {
		if ext == strings.ToLower(includeExt) {
			return true
		}
	}

	return false
}

func (fi *FilesystemImporter) processFile(filePath string, info os.FileInfo, result *ImportResult) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) == 0 {
		return nil
	}

	item := ImportItem{
		ID:      filePath,
		Title:   filepath.Base(filePath),
		Content: string(content),
		Source:  "filesystem",
	}

	fmt.Printf("[%s] %s\n", "file", item.Title)

	if err := fi.ProcessItem(item, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Error processing '%s': %v", item.Title, err))
		fmt.Printf("  ❌ %v\n", err)
	}
	result.TotalProcessed++
	fmt.Println()

	return nil
}

func (fi *FilesystemImporter) ImportSingleFile(filePath string) (*ImportResult, error) {
	result := &ImportResult{
		Errors: []string{},
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if err := fi.processFile(filePath, info, result); err != nil {
		return nil, fmt.Errorf("failed to process file: %w", err)
	}

	return result, nil
}

// ImportOptions defines options for importing files (used by CLI)
type ImportOptions struct {
	Recursive bool
	FileTypes []string
	MaxSize   int64
}

// ImportPath imports files from a given path (convenience method for CLI)
func (fi *FilesystemImporter) ImportPath(ctx context.Context, path string, options ImportOptions) (*ImportResult, error) {
	config := FilesystemImportConfig{
		RootPath:       path,
		Recursive:      options.Recursive,
		MaxFileSize:    options.MaxSize,
		FollowSymlinks: false,
	}

	if len(options.FileTypes) > 0 {
		for _, fileType := range options.FileTypes {
			config.IncludeExts = append(config.IncludeExts, "."+fileType)
		}
	}

	return fi.Import(config)
}
