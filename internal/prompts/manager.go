package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"yumem/internal/workspace"
)

type PromptManager struct {
	promptsDir string
}

type PromptTemplate struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Category    string            `json:"category"`
	Priority    string            `json:"priority"`
	Variables   []VariableSpec    `json:"variables"`
	Template    string            `json:"template"`
	UsageNotes  string            `json:"usage_notes"`
	TestData    map[string]interface{} `json:"test_data"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	UsageCount  int               `json:"usage_count"`
}

type VariableSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func NewPromptManager() *PromptManager {
	config := workspace.GetConfig()
	return &PromptManager{
		promptsDir: filepath.Join(config.WorkspaceDir, "_yumem", "prompts"),
	}
}

func (pm *PromptManager) Initialize() error {
	if err := os.MkdirAll(pm.promptsDir, 0755); err != nil {
		return err
	}

	// Create category directories
	categories := []string{"context_assembly", "data_indexing", "import", "l0", "statistics", "system"}
	for _, category := range categories {
		if err := os.MkdirAll(filepath.Join(pm.promptsDir, category), 0755); err != nil {
			return err
		}
	}

	// Create default prompts if they don't exist
	return pm.createDefaultPrompts()
}

func (pm *PromptManager) createDefaultPrompts() error {
	defaultPrompts := []*PromptTemplate{
		{
			Name:        "L0 Context Formatting",
			Description: "Format L0 data into structured context for AI",
			Version:     "1.0.0",
			Category:    "context_assembly",
			Priority:    "high",
			Variables: []VariableSpec{
				{Name: "long_term_traits", Type: "object", Description: "User long-term characteristics", Required: true},
				{Name: "recent_agenda", Type: "object", Description: "User recent focus areas", Required: true},
				{Name: "timestamp", Type: "string", Description: "Current timestamp", Required: true},
			},
			Template: `Based on the following user memory data, generate structured context for AI conversation:

## User Core Information (as of {{.timestamp}})

### Long-term Characteristics
{{range $category, $traits := .long_term_traits}}
**{{$category | title}}**:
{{range $key, $value := $traits}}
- {{$key}}: {{$value.value}} (updated: {{$value.updated_at.Format "2006-01-02"}})
{{end}}
{{end}}

### Recent Focus Areas
{{range .recent_agenda.current_focus}}
- **{{.item}}** (Priority: {{.priority}}, Since: {{.since.Format "2006-01-02"}})
  {{if .context}}Context: {{.context}}{{end}}
{{end}}

Please consider this information during our conversation to provide personalized and relevant responses.`,
			UsageNotes: "Use this template for every conversation start to provide user context to AI",
			TestData: map[string]interface{}{
				"timestamp": time.Now(),
				"long_term_traits": map[string]interface{}{
					"personality": map[string]interface{}{
						"efficiency": map[string]interface{}{
							"value":      "Highly values efficiency and systematic approaches",
							"updated_at": time.Now().AddDate(0, -1, 0),
						},
					},
				},
				"recent_agenda": map[string]interface{}{
					"current_focus": []map[string]interface{}{
						{
							"item":     "Learning LLM application development",
							"priority": "high",
							"since":    time.Now().AddDate(0, 0, -30),
						},
					},
				},
			},
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			UsageCount: 0,
		},
		{
			Name:        "Content Analysis for Import",
			Description: "Analyze imported content to determine storage layer and classification",
			Version:     "1.0.0",
			Category:    "data_indexing",
			Priority:    "high",
			Variables: []VariableSpec{
				{Name: "content", Type: "string", Description: "Raw content to analyze", Required: true},
				{Name: "source", Type: "string", Description: "Content source (notes, filesystem, etc.)", Required: true},
				{Name: "l1_structure", Type: "object", Description: "Current L1 tree structure", Required: true},
			},
			Template: `Analyze the following content and determine how to store it in the YuMem system:

**Content Source**: {{.source}}
**Content**: 
{{.content}}

**Current L1 Structure**:
{{range $path, $summary := .l1_structure}}
- {{$path}}: {{$summary}}
{{end}}

Please determine:

1. **Storage Layer**: Should this content be stored in L0, L1, or L2?
   - L0: Core user characteristics that define personality, skills, or long-term preferences
   - L1: Topical information that can be categorized and summarized
   - L2: Raw content, conversations, or detailed reference material

2. **Classification**: If L1, what path should it be stored under?
   - Use existing paths when possible
   - Suggest new path if no existing category fits
   - Follow the pattern: category/subcategory/topic

3. **Summary**: If L1, provide a concise summary (1-2 sentences)

4. **Keywords**: Extract 3-5 relevant keywords

5. **Importance**: Rate importance (high/medium/low) for future reference

Format your response as JSON:
{
  "storage_layer": "l0|l1|l2",
  "path": "path/for/l1/storage",
  "summary": "Brief summary of content",
  "keywords": ["keyword1", "keyword2", "keyword3"],
  "importance": "high|medium|low",
  "reasoning": "Explanation of classification decision"
}`,
			UsageNotes: "Use this when importing external content to automatically classify and store",
			TestData: map[string]interface{}{
				"content": "I've been really interested in learning more about neural networks lately. Started reading the Deep Learning book by Goodfellow.",
				"source":  "notes",
				"l1_structure": map[string]string{
					"learning/topics":     "Current learning subjects",
					"personal/interests":  "Personal hobbies and interests",
				},
			},
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			UsageCount: 0,
		},
		{
			Name:        "Memory Statistics Analysis",
			Description: "Analyze memory usage and evolution patterns",
			Version:     "1.0.0",
			Category:    "statistics",
			Priority:    "medium",
			Variables: []VariableSpec{
				{Name: "memory_stats", Type: "object", Description: "Current memory statistics", Required: true},
				{Name: "time_period", Type: "string", Description: "Analysis time period", Required: true},
			},
			Template: `Analyze the memory usage and evolution patterns for YuMem:

**Analysis Period**: {{.time_period}}
**Memory Statistics**:
- L0 Size: {{.memory_stats.l0_size}}
- L1 Nodes: {{.memory_stats.l1_nodes}}
- L2 Entries: {{.memory_stats.l2_entries}}
- Total Storage: {{.memory_stats.total_storage}}

Please provide insights on:

1. **Growth Patterns**: How has memory usage changed over time?
2. **Layer Distribution**: Is the balance between L0/L1/L2 appropriate?
3. **Usage Trends**: What topics or categories are growing most?
4. **Optimization Recommendations**: Any suggestions for better organization?
5. **Health Indicators**: Any concerning patterns or potential issues?

Provide actionable insights that can help improve the memory management system.`,
			UsageNotes: "Generate periodic reports on memory system health and usage patterns",
			TestData: map[string]interface{}{
				"time_period": "Last 30 days",
				"memory_stats": map[string]interface{}{
					"l0_size":       "8.5KB",
					"l1_nodes":      156,
					"l2_entries":    1247,
					"total_storage": "245MB",
				},
			},
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			UsageCount: 0,
		},
	}

	for _, prompt := range defaultPrompts {
		if err := pm.SavePrompt(prompt); err != nil {
			return err
		}
	}

	return nil
}

func (pm *PromptManager) LoadPrompt(category, name string) (*PromptTemplate, error) {
	filename := pm.sanitizeFilename(name) + ".json"
	path := filepath.Join(pm.promptsDir, category, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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

// LoadTemplateFile loads a .md template file from _yumem/prompts/{category}/{name}.md
func (pm *PromptManager) LoadTemplateFile(category, name string) (string, error) {
	path := filepath.Join(pm.promptsDir, category, name+".md")
	data, err := os.ReadFile(path)
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

// WriteTemplateFile writes a template file to _yumem/prompts/{category}/{name}.md
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