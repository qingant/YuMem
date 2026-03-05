package importers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type FilesystemImporter struct {
	*BaseImporter
}

type FilesystemImportConfig struct {
	RootPath      string   `json:"root_path"`
	IncludeExts   []string `json:"include_extensions"`
	ExcludeExts   []string `json:"exclude_extensions"`
	MaxFileSize   int64    `json:"max_file_size_bytes"`
	FollowSymlinks bool    `json:"follow_symlinks"`
	Recursive     bool     `json:"recursive"`
}

func NewFilesystemImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager) *FilesystemImporter {
	return &FilesystemImporter{
		BaseImporter: NewBaseImporter(l0Manager, l1Manager, l2Manager, promptManager),
	}
}

func (fi *FilesystemImporter) Import(config FilesystemImportConfig) (*ImportResult, error) {
	result := &ImportResult{
		TotalProcessed: 0,
		L0Updates:      0,
		L1Created:      0,
		L2Created:      0,
		Errors:         []string{},
		Details:        make(map[string]interface{}),
	}

	// Validate root path
	if _, err := os.Stat(config.RootPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("root path does not exist: %s", config.RootPath)
	}

	// Set defaults
	if len(config.IncludeExts) == 0 {
		config.IncludeExts = []string{".txt", ".md", ".json", ".yaml", ".yml", ".go", ".py", ".js", ".ts"}
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 1024 * 1024 // 1MB default
	}

	// Walk the directory
	err := fi.walkDirectory(config.RootPath, config, result)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	result.Details["config"] = config
	result.Details["completed_at"] = time.Now()

	return result, nil
}

func (fi *FilesystemImporter) walkDirectory(root string, config FilesystemImportConfig, result *ImportResult) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error walking %s: %v", path, err))
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			// If not recursive, skip subdirectories
			if !config.Recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks if not following them
		if info.Mode()&os.ModeSymlink != 0 && !config.FollowSymlinks {
			return nil
		}

		// Check file size
		if info.Size() > config.MaxFileSize {
			result.Errors = append(result.Errors, fmt.Sprintf("File too large, skipping: %s (%d bytes)", path, info.Size()))
			return nil
		}

		// Check file extension
		if !fi.shouldIncludeFile(path, config) {
			return nil
		}

		// Process the file
		if err := fi.processFile(path, info, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error processing %s: %v", path, err))
		}

		return nil
	})
}

func (fi *FilesystemImporter) shouldIncludeFile(filePath string, config FilesystemImportConfig) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Check exclude list first
	for _, excludeExt := range config.ExcludeExts {
		if ext == strings.ToLower(excludeExt) {
			return false
		}
	}

	// Check include list
	for _, includeExt := range config.IncludeExts {
		if ext == strings.ToLower(includeExt) {
			return true
		}
	}

	return false
}

func (fi *FilesystemImporter) processFile(filePath string, info os.FileInfo, result *ImportResult) error {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Skip empty files
	if len(content) == 0 {
		return nil
	}

	// Convert to ImportItem
	item := ImportItem{
		ID:         filePath,
		Title:      filepath.Base(filePath),
		Content:    string(content),
		Source:     "filesystem",
		CreatedAt:  info.ModTime().Format(time.RFC3339),
		ModifiedAt: info.ModTime().Format(time.RFC3339),
		Metadata: map[string]string{
			"file_path": filePath,
			"file_size": fmt.Sprintf("%d", info.Size()),
			"file_ext":  filepath.Ext(filePath),
		},
	}

	// Analyze content
	analysis, err := fi.AnalyzeContent(item)
	if err != nil {
		return fmt.Errorf("failed to analyze content: %w", err)
	}

	// Process based on analysis
	if err := fi.ProcessAnalysisResult(item, analysis, result); err != nil {
		return fmt.Errorf("failed to process analysis result: %w", err)
	}

	result.TotalProcessed++
	return nil
}

func (fi *FilesystemImporter) ImportSingleFile(filePath string) (*ImportResult, error) {
	result := &ImportResult{
		TotalProcessed: 0,
		L0Updates:      0,
		L1Created:      0,
		L2Created:      0,
		Errors:         []string{},
		Details:        make(map[string]interface{}),
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if err := fi.processFile(filePath, info, result); err != nil {
		return nil, fmt.Errorf("failed to process file: %w", err)
	}

	result.Details["file_path"] = filePath
	result.Details["completed_at"] = time.Now()

	return result, nil
}