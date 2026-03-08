package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type PromptManager struct {
	promptsDir string
}

type PromptTemplate struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Version     string                 `json:"version"`
	Category    string                 `json:"category"`
	Priority    string                 `json:"priority"`
	Variables   []VariableSpec         `json:"variables"`
	Template    string                 `json:"template"`
	UsageNotes  string                 `json:"usage_notes"`
	TestData    map[string]interface{} `json:"test_data"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	UsageCount  int                    `json:"usage_count"`
}

type VariableSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func globalPromptsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".yumem", "prompts")
}

func NewPromptManager() *PromptManager {
	return &PromptManager{
		promptsDir: globalPromptsDir(),
	}
}

func (pm *PromptManager) Initialize() error {
	// Create prompts dir for potential user overrides
	return os.MkdirAll(pm.promptsDir, 0755)
}

func (pm *PromptManager) LoadPrompt(category, name string) (*PromptTemplate, error) {
	filename := pm.sanitizeFilename(name) + ".json"

	// Try user override first
	path := filepath.Join(pm.promptsDir, category, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		// Fall back to embedded defaults
		embedPath := "defaults/" + category + "/" + filename
		data, err = defaultPrompts.ReadFile(embedPath)
		if err != nil {
			return nil, err
		}
	}

	var prompt PromptTemplate
	if err := json.Unmarshal(data, &prompt); err != nil {
		return nil, err
	}

	return &prompt, nil
}

func (pm *PromptManager) SavePrompt(prompt *PromptTemplate) error {
	prompt.UpdatedAt = time.Now()
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = time.Now()
	}

	filename := pm.sanitizeFilename(prompt.Name) + ".json"
	path := filepath.Join(pm.promptsDir, prompt.Category, filename)

	data, err := json.MarshalIndent(prompt, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (pm *PromptManager) ListPrompts(category string) ([]*PromptTemplate, error) {
	var prompts []*PromptTemplate

	categoryPath := pm.promptsDir
	if category != "" {
		categoryPath = filepath.Join(pm.promptsDir, category)
	}

	err := filepath.Walk(categoryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			var prompt PromptTemplate
			if err := json.Unmarshal(data, &prompt); err != nil {
				return err
			}

			prompts = append(prompts, &prompt)
		}

		return nil
	})

	return prompts, err
}

func (pm *PromptManager) RenderPrompt(prompt *PromptTemplate, data interface{}) (string, error) {
	tmpl, err := template.New(prompt.Name).Parse(prompt.Template)
	if err != nil {
		return "", err
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", err
	}

	// Update usage count
	prompt.UsageCount++
	pm.SavePrompt(prompt)

	return result.String(), nil
}

func (pm *PromptManager) TestPrompt(prompt *PromptTemplate) (string, error) {
	return pm.RenderPrompt(prompt, prompt.TestData)
}

func (pm *PromptManager) sanitizeFilename(name string) string {
	// Replace spaces and special characters with underscores
	sanitized := strings.ReplaceAll(name, " ", "_")
	sanitized = strings.ReplaceAll(sanitized, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	return strings.ToLower(sanitized)
}

func (pm *PromptManager) GetCategories() ([]string, error) {
	entries, err := os.ReadDir(pm.promptsDir)
	if err != nil {
		return nil, err
	}

	var categories []string
	for _, entry := range entries {
		if entry.IsDir() {
			categories = append(categories, entry.Name())
		}
	}

	return categories, nil
}

// LoadTemplateFile loads a .md template file.
// Priority: user override at ~/.yumem/prompts/ > embedded defaults in binary.
func (pm *PromptManager) LoadTemplateFile(category, name string) (string, error) {
	// Try user override first
	path := filepath.Join(pm.promptsDir, category, name+".md")
	if data, err := os.ReadFile(path); err == nil {
		return string(data), nil
	}

	// Fall back to embedded defaults
	embedPath := "defaults/" + category + "/" + name + ".md"
	data, err := defaultPrompts.ReadFile(embedPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RenderTemplate renders a Go template string with the provided data.
func (pm *PromptManager) RenderTemplate(templateStr string, data interface{}) (string, error) {
	tmpl, err := template.New("prompt").Parse(templateStr)
	if err != nil {
		return "", err
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", err
	}

	return result.String(), nil
}

// WriteTemplateFile writes a template file to ~/.yumemory/prompts/{category}/{name}.md
// Used during workspace initialization to create default templates.
func (pm *PromptManager) WriteTemplateFile(category, name, content string) error {
	dir := filepath.Join(pm.promptsDir, category)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".md")
	// Only write if file doesn't exist (don't overwrite user edits)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}
