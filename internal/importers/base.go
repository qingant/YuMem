package importers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"yumem/internal/ai"
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
	aiManager     *ai.Manager
}

type ImportItem struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Content     string            `json:"content"`
	Type        string            `json:"type"`
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

func NewBaseImporter(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager) *BaseImporter {
	return &BaseImporter{
		l0Manager:     l0Manager,
		l1Manager:     l1Manager,
		l2Manager:     l2Manager,
		promptManager: promptManager,
		aiManager:     aiManager,
	}
}

func (bi *BaseImporter) AnalyzeContent(item ImportItem) (*ContentAnalysisResult, error) {
	// Load content analysis prompt
	fmt.Printf("🔍 Loading analysis prompt...\n")
	prompt, err := bi.promptManager.LoadPrompt("data_indexing", "Content Analysis for Import")
	if err != nil {
		fmt.Printf("❌ Failed to load analysis prompt: %v\n", err)
		return nil, fmt.Errorf("failed to load analysis prompt: %w", err)
	}
	fmt.Printf("✅ Prompt loaded: %s\n", prompt.Name)

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
	analysisPrompt, err := bi.promptManager.RenderPrompt(prompt, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render analysis prompt: %w", err)
	}

	// Call AI provider to analyze content
	ctx := context.Background()
	fmt.Printf("🔍 Attempting AI analysis with prompt length: %d chars\n", len(analysisPrompt))
	fmt.Printf("🔍 Available providers: %v\n", bi.aiManager.ListProviders())
	fmt.Printf("🔍 Actual prompt being sent:\n%s\n", analysisPrompt)
	completion, err := bi.aiManager.Complete(ctx, analysisPrompt, ai.CompletionOptions{
		MaxTokens:   500,
		Temperature: 0.3,
	})
	if err != nil {
		// If AI call fails, fall back to heuristic analysis
		fmt.Printf("🤖 AI analysis failed (error: %v), using heuristic analysis\n", err)
		return bi.performHeuristicAnalysis(item), nil
	}
	
	fmt.Printf("🤖 AI response received: %d chars\n", len(completion.Content))
	fmt.Printf("🤖 Provider used: %s\n", completion.ProviderName)
	fmt.Printf("🔍 Raw AI response: %s\n", completion.Content)

	// Clean AI response (remove markdown code blocks if present)
	content := completion.Content
	if strings.HasPrefix(content, "```json") && strings.HasSuffix(content, "```") {
		// Remove ```json and ``` wrappers
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	
	// Try to parse AI response as JSON
	var analysis ContentAnalysisResult
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		// If parsing fails, fall back to heuristic analysis
		fmt.Printf("🤖 AI response parsing failed (%v), using heuristic analysis\n", err)
		fmt.Printf("🔍 AI response was: %s\n", completion.Content)
		return bi.performHeuristicAnalysis(item), nil
	}

	// Check if AI response is actually useful
	if analysis.StorageLayer == "" {
		fmt.Printf("🤖 AI returned empty analysis, using heuristic analysis\n")
		fmt.Printf("🔍 Parsed AI response: StorageLayer='%s', Path='%s', Summary='%s'\n", 
			analysis.StorageLayer, analysis.Path, analysis.Summary)
		return bi.performHeuristicAnalysis(item), nil
	}

	fmt.Printf("✅ AI analysis successful: %s -> %s\n", analysis.StorageLayer, analysis.Path)
	return &analysis, nil
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
	// Enhanced heuristic analysis based on content patterns
	content := strings.ToLower(item.Content)
	title := strings.ToLower(item.Title)
	
	fmt.Printf("🔍 Performing heuristic analysis for: %s\n", item.Title)
	
	// Check for personal traits/characteristics
	if bi.containsPersonalTraits(content) || bi.containsPersonalTraits(title) {
		return &ContentAnalysisResult{
			StorageLayer: "l0",
			Path:         "",
			Summary:      fmt.Sprintf("Personal information: %s", bi.truncateText(item.Content, 100)),
			Keywords:     bi.extractSimpleKeywords(item.Content),
			Importance:   "high",
			Reasoning:    "Contains personal traits or long-term characteristics",
		}
	}

	// Check for technical/work content that should go to L1
	if bi.isTechnicalContent(content) || bi.isTechnicalContent(title) {
		path := bi.determineL1Path(item.Content)
		return &ContentAnalysisResult{
			StorageLayer: "l1",
			Path:         path,
			Summary:      bi.generateSimpleSummary(item.Content),
			Keywords:     bi.extractSimpleKeywords(item.Content),
			Importance:   "medium",
			Reasoning:    fmt.Sprintf("Technical/work content suitable for L1 at path: %s", path),
		}
	}

	// Default: most Apple Notes content goes to L2 for later processing
	return &ContentAnalysisResult{
		StorageLayer: "l2",
		Path:         fmt.Sprintf("imported/%s", item.Source),
		Summary:      bi.generateSimpleSummary(item.Content),
		Keywords:     bi.extractSimpleKeywords(item.Content),
		Importance:   "low",
		Reasoning:    "General content stored in L2 for comprehensive indexing",
	}
}

// isTechnicalContent checks if content contains technical/work-related information
func (bi *BaseImporter) isTechnicalContent(content string) bool {
	techIndicators := []string{
		"system", "api", "server", "database", "cli", "tool", "framework",
		"implementation", "architecture", "design", "code", "programming",
		"development", "project", "mcp", "workspace", "memory management",
		"algorithm", "data structure", "interface", "protocol", "技术", "系统",
		"开发", "项目", "设计", "实现", "架构", "程序", "代码", "工具",
	}
	
	content = strings.ToLower(content)
	matchCount := 0
	for _, indicator := range techIndicators {
		if strings.Contains(content, indicator) {
			matchCount++
		}
	}
	
	// Require at least 2 technical terms for classification
	return matchCount >= 2
}

// truncateText truncates text to specified length
func (bi *BaseImporter) truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
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
	// Enhanced keyword-based path determination supporting Chinese and English
	contentLower := strings.ToLower(content)
	
	// Technical/Development content
	if bi.containsKeywords(contentLower, []string{
		"system", "api", "server", "database", "programming", "development",
		"系统", "开发", "程序", "技术", "架构", "设计", "cli", "tool", "mcp",
	}) {
		return "work/technology"
	}
	
	// Learning content
	if bi.containsKeywords(contentLower, []string{
		"learn", "study", "course", "book", "tutorial", "knowledge",
		"学习", "课程", "教程", "知识", "研究", "文档",
	}) {
		return "learning/knowledge"
	}
	
	// Work/Projects
	if bi.containsKeywords(contentLower, []string{
		"work", "project", "job", "career", "business", "meeting",
		"工作", "项目", "会议", "业务", "职业", "公司",
	}) {
		return "work/projects"
	}
	
	// Personal interests
	if bi.containsKeywords(contentLower, []string{
		"hobby", "interest", "enjoy", "fun", "like", "favorite",
		"爱好", "兴趣", "喜欢", "娱乐", "休闲",
	}) {
		return "personal/interests"
	}
	
	// Goals and planning
	if bi.containsKeywords(contentLower, []string{
		"goal", "plan", "want", "achieve", "future", "dream",
		"目标", "计划", "想要", "梦想", "未来", "希望",
	}) {
		return "personal/goals"
	}
	
	// Ideas and thoughts
	if bi.containsKeywords(contentLower, []string{
		"idea", "thought", "think", "opinion", "reflection",
		"想法", "思考", "意见", "反思", "观点",
	}) {
		return "personal/thoughts"
	}

	return "imported/general"
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
	if analysis == nil {
		return fmt.Errorf("analysis result is nil")
	}
	
	if analysis.StorageLayer == "" {
		// If no layer specified, default to L2 for Apple Notes
		analysis.StorageLayer = "l2"
		analysis.Path = "imported/apple_notes"
		analysis.Summary = "Imported from Apple Notes"
		analysis.Keywords = []string{"notes", "imported"}
		fmt.Printf("🔧 Fixed empty storage layer, defaulting to L2\n")
	}
	
	fmt.Printf("📋 Processing item '%s' -> %s layer\n", item.Title, analysis.StorageLayer)
	
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
	l2Tags := []string{"imported", item.Source}
	if len(analysis.Keywords) > 0 {
		l2Tags = append(l2Tags, analysis.Keywords...)
	}
	_, err = bi.l2Manager.AddEntry(item.Title, item.Content, "imported_content", item.Source, l2Tags)
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
	// Store content directly in L2
	tags := []string{"imported", item.Source}
	if len(analysis.Keywords) > 0 {
		tags = append(tags, analysis.Keywords...)
	}
	
	_, err := bi.l2Manager.AddEntry(item.Title, item.Content, "imported_content", item.Source, tags)
	if err != nil {
		return fmt.Errorf("failed to add L2 entry: %w", err)
	}

	result.L2Created++
	return nil
}