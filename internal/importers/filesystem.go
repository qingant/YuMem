package importers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"yumem/internal/memory"
	"yumem/internal/workspace"
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
	AllTextFiles   bool     `json:"all_text_files"`
	Force          bool     `json:"force"`
}

var defaultIncludeExts = []string{
	".txt", ".md", ".json", ".yaml", ".yml", ".go", ".py", ".js", ".ts",
	".csv", ".xml", ".html", ".htm", ".sh", ".bash", ".zsh",
	".toml", ".ini", ".conf", ".cfg",
	".rb", ".java", ".c", ".cpp", ".h", ".hpp", ".rs", ".swift",
	".sql", ".lua", ".php", ".tex", ".log",
	".css", ".scss", ".less", ".jsx", ".tsx", ".vue", ".svelte",
	".r", ".R", ".pl", ".pm", ".kt", ".scala", ".ex", ".exs",
	".env.example", ".gitignore", ".dockerignore", ".makefile",
}

// NewFilesystemImporterL2Only creates a filesystem importer that only stores to L2 (no AI needed).
func NewFilesystemImporterL2Only(l2Manager *memory.L2Manager) *FilesystemImporter {
	return &FilesystemImporter{
		BaseImporter: &BaseImporter{
			l2Manager: l2Manager,
		},
	}
}

func (fi *FilesystemImporter) Import(cfg FilesystemImportConfig) (*ImportResult, error) {
	result := &ImportResult{
		Errors: []string{},
	}

	if _, err := os.Stat(cfg.RootPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("root path does not exist: %s", cfg.RootPath)
	}

	if len(cfg.IncludeExts) == 0 && !cfg.AllTextFiles {
		cfg.IncludeExts = defaultIncludeExts
	}
	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 1024 * 1024 // 1MB default
	}

	// Load manifest for incremental import
	manifestPath := filepath.Join(workspace.GetConfig().WorkspaceDir, "_yumem", "imports", "files_manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load manifest: %w", err)
	}
	manifest.Source = "filesystem"

	err = fi.walkDirectory(cfg.RootPath, cfg, manifest, result)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Save manifest
	if err := manifest.Save(manifestPath); err != nil {
		fmt.Printf("  ⚠️  Failed to save manifest: %v\n", err)
	}

	fmt.Printf("\n📊 %d stored to L2, %d skipped\n", result.TotalProcessed, result.Skipped)
	fmt.Println("💡 Run 'yumem index' to generate L0/L1 from imported content")

	return result, nil
}

func (fi *FilesystemImporter) walkDirectory(root string, cfg FilesystemImportConfig, manifest *ImportManifest, result *ImportResult) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error walking %s: %v", path, err))
			return nil
		}

		if info.IsDir() {
			if !cfg.Recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 && !cfg.FollowSymlinks {
			return nil
		}

		if info.Size() > cfg.MaxFileSize {
			result.Errors = append(result.Errors, fmt.Sprintf("File too large, skipping: %s (%d bytes)", path, info.Size()))
			return nil
		}

		if !fi.shouldIncludeFile(path, cfg) {
			return nil
		}

		if err := fi.processFile(path, cfg, manifest, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error processing %s: %v", path, err))
		}

		return nil
	})
}

func (fi *FilesystemImporter) shouldIncludeFile(filePath string, cfg FilesystemImportConfig) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	for _, excludeExt := range cfg.ExcludeExts {
		if ext == strings.ToLower(excludeExt) {
			return false
		}
	}

	if cfg.AllTextFiles {
		return detectTextFile(filePath)
	}

	for _, includeExt := range cfg.IncludeExts {
		if ext == strings.ToLower(includeExt) {
			return true
		}
	}

	return false
}

// detectTextFile reads the first 512 bytes and checks for null bytes to determine if a file is text.
func detectTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if n == 0 {
		return false
	}

	for _, b := range buf[:n] {
		if b == 0 {
			return false
		}
	}
	return true
}

func (fi *FilesystemImporter) processFile(filePath string, cfg FilesystemImportConfig, manifest *ImportManifest, result *ImportResult) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) == 0 {
		return nil
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	hash := ContentHash(string(content))

	// Check manifest for incremental skip
	if !cfg.Force && !manifest.NeedsProcessing(absPath, hash) {
		result.Skipped++
		return nil
	}

	// Get file modification time for ContentDate
	var contentDate time.Time
	if fileInfo, err := os.Stat(filePath); err == nil {
		contentDate = fileInfo.ModTime()
	}

	item := ImportItem{
		ID:          absPath,
		Title:       filepath.Base(filePath),
		Content:     string(content),
		Source:      "filesystem",
		ContentDate: contentDate,
	}

	fmt.Printf("[file] %s\n", item.Title)

	l2ID, err := fi.StoreItem(item, result)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Error processing '%s': %v", item.Title, err))
		fmt.Printf("  ❌ %v\n", err)
	} else {
		manifest.Record(absPath, ManifestEntry{
			Title:       item.Title,
			ContentHash: hash,
			L2ID:        l2ID,
			ImportedAt:  time.Now(),
		})
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

	if info.IsDir() {
		return nil, fmt.Errorf("expected a file, got a directory: %s", filePath)
	}

	// Single file import bypasses manifest (always force)
	cfg := FilesystemImportConfig{Force: true}
	manifest := &ImportManifest{Entries: make(map[string]ManifestEntry)}
	if err := fi.processFile(filePath, cfg, manifest, result); err != nil {
		return nil, fmt.Errorf("failed to process file: %w", err)
	}

	return result, nil
}

// ImportOptions defines options for importing files (used by CLI)
type ImportOptions struct {
	Recursive    bool
	FileTypes    []string
	MaxSize      int64
	AllTextFiles bool
	Force        bool
}

// ImportPath imports files from a given path (convenience method for CLI)
func (fi *FilesystemImporter) ImportPath(ctx context.Context, path string, options ImportOptions) (*ImportResult, error) {
	cfg := FilesystemImportConfig{
		RootPath:       path,
		Recursive:      options.Recursive,
		MaxFileSize:    options.MaxSize,
		FollowSymlinks: false,
		AllTextFiles:   options.AllTextFiles,
		Force:          options.Force,
	}

	if len(options.FileTypes) > 0 {
		for _, fileType := range options.FileTypes {
			cfg.IncludeExts = append(cfg.IncludeExts, "."+fileType)
		}
	}

	return fi.Import(cfg)
}
