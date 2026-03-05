package importers

import (
	"fmt"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type ImportResult struct {
	TotalProcessed int                    `json:"total_processed"`
	L0Updates      int                    `json:"l0_updates"`
	L1Created      int                    `json:"l1_created"`
	L2Created      int                    `json:"l2_created"`
	Errors         []string               `json:"errors"`
	Details        map[string]interface{} `json:"details"`
}

type BaseImporter struct {
	l0Manager     *memory.L0Manager
	l1Manager     *memory.L1Manager
	l2Manager     *memory.L2Manager
	promptManager *prompts.PromptManager
}

type ImportItem struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Content     string            `json:"content"`
	Source      string            `json:"source"`
	CreatedAt   string            `json:"created_at"`
	ModifiedAt  string            `json:"modified_at"`
	Metadata    map[string]string `json:"metadata"`
}

type ContentAnalysisResult struct {
	StorageLayer string   `json:"storage_layer"`
	Path         string   `json:"path"`
	Summary      string   `json:"summary"`
	Keywords     []string `json:"keywords"`
	Importance   string   `json:"importance"`
	Reasoning    string   `json:"reasoning"`
}

func NewBaseImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager) *BaseImporter {
	return &BaseImporter{
		l0Manager:     l0Manager,
		l1Manager:     l1Manager,
		l2Manager:     l2Manager,
		promptManager: promptManager,
	}
}

func (bi *BaseImporter) AnalyzeContent(item ImportItem) (*ContentAnalysisResult, error) {
	// Load content analysis prompt
	prompt, err := bi.promptManager.LoadPrompt("data_indexing", "Content Analysis for Import")
	if err != nil {
		return nil, fmt.Errorf("failed to load analysis prompt: %w", err)
	}

	// Get current L1 structure for context
	l1Structure, err := bi.getL1Structure()
	if err != nil {
		return nil, fmt.Errorf("failed to get L1 structure: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"content":      item.Content,
		"source":       item.Source,
		"l1_structure": l1Structure,
	}

	// Render analysis prompt
	_, err = bi.promptManager.RenderPrompt(prompt, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render analysis prompt: %w", err)
	}

	// TODO: Call actual LLM here
	// For now, return a simple heuristic-based analysis
	return bi.performHeuristicAnalysis(item), nil
}

func (bi *BaseImporter) getL1Structure() (map[string]string, error) {
	nodes, err := bi.l1Manager.GetTree()
	if err != nil {
		return nil, err
	}

	structure := make(map[string]string)
	for _, node := range nodes {
		structure[node.Path] = node.Summary
	}

	return structure, nil
}

func (bi *BaseImporter) performHeuristicAnalysis(item ImportItem) *ContentAnalysisResult {
	// Simple heuristic analysis based on content patterns
	content := item.Content
	
	// Check for personal traits/characteristics
	if bi.containsPersonalTraits(content) {
		return &ContentAnalysisResult{
			StorageLayer: "l0",
			Path:         "",
			Summary:      "Personal characteristic or preference",
			Keywords:     bi.extractSimpleKeywords(content),
			Importance:   "high",
			Reasoning:    "Contains personal traits or long-term characteristics",
		}
	}

	// Check for learning/work content
	path := bi.determineL1Path(content)
	
	return &ContentAnalysisResult{
		StorageLayer: "l1",
		Path:         path,
		Summary:      bi.generateSimpleSummary(content),
		Keywords:     bi.extractSimpleKeywords(content),
		Importance:   "medium",
		Reasoning:    fmt.Sprintf("Topical content suitable for L1 storage at path: %s", path),
	}
}

func (bi *BaseImporter) containsPersonalTraits(content string) bool {
	traitIndicators := []string{
		"i am", "i'm", "my personality", "i prefer", "i like", "i dislike",
		"my background", "my experience", "i believe", "my philosophy",
	}

	contentLower := content
	for _, indicator := range traitIndicators {
		if len(content) > 0 && len(indicator) > 0 {
			// Simple contains check
			if len(contentLower) >= len(indicator) {
				for i := 0; i <= len(contentLower)-len(indicator); i++ {
					if contentLower[i:i+len(indicator)] == indicator {
						return true
					}
				}
			}
		}
	}
	return false
}

func (bi *BaseImporter) determineL1Path(content string) string {
	// Simple keyword-based path determination
	if bi.containsKeywords(content, []string{"learn", "study", "course", "book"}) {
		return "learning/topics"
	}
	if bi.containsKeywords(content, []string{"work", "project", "job", "career"}) {
		return "work/projects"
	}
	if bi.containsKeywords(content, []string{"hobby", "interest", "enjoy", "fun"}) {
		return "personal/interests"
	}
	if bi.containsKeywords(content, []string{"goal", "plan", "want", "achieve"}) {
		return "personal/goals"
	}

	return "general/notes"
}

func (bi *BaseImporter) containsKeywords(content string, keywords []string) bool {
	contentLower := content
	for _, keyword := range keywords {
		if len(contentLower) >= len(keyword) {
			for i := 0; i <= len(contentLower)-len(keyword); i++ {
				if contentLower[i:i+len(keyword)] == keyword {
					return true
				}
			}
		}
	}
	return false
}

func (bi *BaseImporter) generateSimpleSummary(content string) string {
	// Simple summary: first sentence or first 100 characters
	if len(content) <= 100 {
		return content
	}
	
	// Try to find first sentence
	for i, char := range content {
		if char == '.' && i < 200 {
			return content[:i+1]
		}
	}
	
	// Fallback to first 100 characters
	return content[:100] + "..."
}

func (bi *BaseImporter) extractSimpleKeywords(content string) []string {
	// Very simple keyword extraction
	words := []string{}
	if len(content) > 0 {
		// Split by common delimiters and take first few meaningful words
		wordStart := 0
		for i, char := range content {
			if char == ' ' || char == '\n' || char == ',' || char == '.' {
				if i > wordStart {
					word := content[wordStart:i]
					if len(word) > 3 && len(words) < 5 { // Only words longer than 3 chars
						words = append(words, word)
					}
				}
				wordStart = i + 1
			}
		}
		// Don't forget the last word
		if wordStart < len(content) {
			word := content[wordStart:]
			if len(word) > 3 && len(words) < 5 {
				words = append(words, word)
			}
		}
	}
	
	return words
}

func (bi *BaseImporter) ProcessAnalysisResult(item ImportItem, analysis *ContentAnalysisResult, result *ImportResult) error {
	switch analysis.StorageLayer {
	case "l0":
		return bi.processL0Item(item, analysis, result)
	case "l1":
		return bi.processL1Item(item, analysis, result)
	case "l2":
		return bi.processL2Item(item, analysis, result)
	default:
		return fmt.Errorf("unknown storage layer: %s", analysis.StorageLayer)
	}
}

func (bi *BaseImporter) processL0Item(item ImportItem, analysis *ContentAnalysisResult, result *ImportResult) error {
	// TODO: Update L0 data based on analysis
	// This would involve parsing the content and updating appropriate L0 fields
	result.L0Updates++
	return nil
}

func (bi *BaseImporter) processL1Item(item ImportItem, analysis *ContentAnalysisResult, result *ImportResult) error {
	// Create L1 node
	node, err := bi.l1Manager.CreateNode(
		analysis.Path,
		item.Title,
		analysis.Summary,
		analysis.Keywords,
		[]string{}, // L2 refs will be added if we also store in L2
	)
	if err != nil {
		return fmt.Errorf("failed to create L1 node: %w", err)
	}

	// Also store original content in L2
	_, err = bi.l2Manager.AddFile("", []string{"imported", item.Source})
	if err == nil {
		// Update L1 node with L2 reference
		bi.l1Manager.UpdateNode(node.ID, analysis.Summary, analysis.Keywords)
	}

	result.L1Created++
	if err == nil {
		result.L2Created++
	}

	return nil
}

func (bi *BaseImporter) processL2Item(item ImportItem, analysis *ContentAnalysisResult, result *ImportResult) error {
	// Store directly in L2
	_, err := bi.l2Manager.AddFile("", []string{"imported", item.Source})
	if err != nil {
		return fmt.Errorf("failed to add L2 entry: %w", err)
	}

	result.L2Created++
	return nil
}