package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"yumem/internal/ai"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type RetrievalEngine struct {
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
	aiManager       *ai.Manager
}

type ContextRequest struct {
	Query struct {
		Type        string   `json:"type"`
		Keywords    []string `json:"keywords"`
		TimeRange   TimeRange `json:"time_range"`
		Scope       []string `json:"scope"`
		MaxItems    int      `json:"max_items"`
	} `json:"query"`

	Requirements struct {
		IncludeL0Structure bool   `json:"include_l0_structure"`
		IncludeRecency     bool   `json:"include_recency"`
		Summarize          bool   `json:"summarize"`
		TargetLength       string `json:"target_length"`
	} `json:"context_requirements"`
}

type TimeRange struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Priority string `json:"priority"`
}

type ContextResponse struct {
	Context struct {
		L0Structured     *L0StructuredContext `json:"l0_structured"`
		RelevantMemories []ScoredMemory       `json:"relevant_memories"`
		AssembledContext string               `json:"assembled_context"`
	} `json:"context"`
}

type L0StructuredContext struct {
	Facts []FactInfo `json:"facts"`
}

type FactInfo struct {
	Text       string `json:"text"`
	ObservedAt string `json:"observed_at"`
	SourceName string `json:"source_name,omitempty"`
}

type ScoredMemory struct {
	Layer          string    `json:"layer"`
	Path           string    `json:"path,omitempty"`
	Title          string    `json:"title,omitempty"`
	Summary        string    `json:"summary"`
	Content        string    `json:"content,omitempty"`
	RelevanceScore float64   `json:"relevance_score"`
	RecencyScore   float64   `json:"recency_score"`
	Timestamp      time.Time `json:"timestamp"`
	L2Refs         []string  `json:"l2_refs,omitempty"`
}

func NewRetrievalEngine(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager) *RetrievalEngine {
	return &RetrievalEngine{
		l0Manager:     l0Manager,
		l1Manager:     l1Manager,
		l2Manager:     l2Manager,
		promptManager: promptManager,
		aiManager:     aiManager,
	}
}

// GetCoreMemory returns a well-formatted core memory string with recency-aware information.
func (re *RetrievalEngine) GetCoreMemory() (string, error) {
	l0Data, err := re.l0Manager.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load L0 data: %w", err)
	}

	now := time.Now()
	var sb strings.Builder

	sb.WriteString("# Core Memory\n\n")
	sb.WriteString(fmt.Sprintf("*Generated: %s*\n\n", now.Format("2006-01-02 15:04")))

	// User identity
	if l0Data.UserID != "" && l0Data.UserID != "default" {
		sb.WriteString(fmt.Sprintf("**User**: %s\n\n", l0Data.UserID))
	}

	// Facts — filter expired, annotate with date
	var activeFacts []memory.Fact
	for _, f := range l0Data.Facts {
		if !f.Expired {
			activeFacts = append(activeFacts, f)
		}
	}

	if len(activeFacts) > 0 {
		for _, f := range activeFacts {
			recency := formatRecency(f.ObservedAt, now)
			sb.WriteString(fmt.Sprintf("- %s", f.Text))
			if recency != "" {
				sb.WriteString(fmt.Sprintf(" _%s_", recency))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// L0 meta
	sb.WriteString(fmt.Sprintf("---\n*Last updated: %s*\n", l0Data.Meta.LastUpdated.Format("2006-01-02 15:04")))

	result := sb.String()

	// If core memory exceeds L0MaxSizeBytes, try AI compression
	if len([]byte(result)) > memory.L0MaxSizeBytes {
		compressed, err := re.compressCoreMemory(result)
		if err == nil && compressed != "" {
			return compressed, nil
		}
		// Fallback: return uncompressed
	}

	return result, nil
}

// compressCoreMemory uses AI to compress core memory text within L0MaxSizeBytes.
func (re *RetrievalEngine) compressCoreMemory(coreMemory string) (string, error) {
	if re.aiManager == nil {
		return "", fmt.Errorf("AI manager not available")
	}

	templateStr, err := re.promptManager.LoadTemplateFile("retrieval", "compress_core_memory")
	if err != nil {
		return "", fmt.Errorf("failed to load compression prompt: %w", err)
	}

	templateData := map[string]interface{}{
		"core_memory": coreMemory,
		"max_bytes":   memory.L0MaxSizeBytes,
	}

	prompt, err := re.promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return "", fmt.Errorf("failed to render compression prompt: %w", err)
	}

	ctx := context.Background()
	completion, err := re.aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   4000,
		Temperature: 0.3,
		Purpose:     "retrieval",
	})
	if err != nil {
		return "", fmt.Errorf("AI compression failed: %w", err)
	}

	compressed := strings.TrimSpace(completion.Content)
	// Strip markdown code block wrapper if present
	if strings.HasPrefix(compressed, "```") {
		if idx := strings.Index(compressed, "\n"); idx != -1 {
			compressed = compressed[idx+1:]
		}
		if strings.HasSuffix(compressed, "```") {
			compressed = compressed[:len(compressed)-3]
		}
		compressed = strings.TrimSpace(compressed)
	}

	return compressed, nil
}

// formatRecency returns the date string as an absolute date label.
func formatRecency(dateStr string, now time.Time) string {
	if dateStr == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		return ""
	}
	return fmt.Sprintf("(%s)", dateStr)
}

func (re *RetrievalEngine) RetrieveContext(request ContextRequest) (*ContextResponse, error) {
	response := &ContextResponse{}

	// 1. Always include L0 structured context if requested
	if request.Requirements.IncludeL0Structure {
		l0Context, err := re.buildL0StructuredContext()
		if err != nil {
			return nil, fmt.Errorf("failed to build L0 context: %w", err)
		}
		response.Context.L0Structured = l0Context
	}

	// 2. Search relevant memories
	relevantMemories, err := re.searchRelevantMemories(request)
	if err != nil {
		return nil, fmt.Errorf("failed to search memories: %w", err)
	}
	response.Context.RelevantMemories = relevantMemories

	// 3. Assemble context using LLM
	if request.Requirements.Summarize {
		assembledContext, err := re.assembleContextWithLLM(response.Context, request)
		if err != nil {
			return nil, fmt.Errorf("failed to assemble context: %w", err)
		}
		response.Context.AssembledContext = assembledContext
	}

	return response, nil
}

func (re *RetrievalEngine) buildL0StructuredContext() (*L0StructuredContext, error) {
	l0Data, err := re.l0Manager.Load()
	if err != nil {
		return nil, err
	}

	ctx := &L0StructuredContext{
		Facts: []FactInfo{},
	}

	for _, f := range l0Data.Facts {
		if f.Expired {
			continue
		}
		ctx.Facts = append(ctx.Facts, FactInfo{
			Text:       f.Text,
			ObservedAt: f.ObservedAt,
			SourceName: f.SourceName,
		})
	}

	return ctx, nil
}

func (re *RetrievalEngine) searchRelevantMemories(request ContextRequest) ([]ScoredMemory, error) {
	var memories []ScoredMemory

	if re.shouldSearchLayer("l1", request.Query.Scope) {
		l1Memories, err := re.searchL1Memories(request)
		if err != nil {
			return nil, err
		}
		memories = append(memories, l1Memories...)
	}

	if re.shouldSearchLayer("l2", request.Query.Scope) {
		l2Memories, err := re.searchL2Memories(request)
		if err != nil {
			return nil, err
		}
		memories = append(memories, l2Memories...)
	}

	sort.Slice(memories, func(i, j int) bool {
		scoreI := memories[i].RelevanceScore*0.7 + memories[i].RecencyScore*0.3
		scoreJ := memories[j].RelevanceScore*0.7 + memories[j].RecencyScore*0.3
		return scoreI > scoreJ
	})

	if request.Query.MaxItems > 0 && len(memories) > request.Query.MaxItems {
		memories = memories[:request.Query.MaxItems]
	}

	return memories, nil
}

func (re *RetrievalEngine) searchL1Memories(request ContextRequest) ([]ScoredMemory, error) {
	var memories []ScoredMemory

	nodes, err := re.l1Manager.GetTree()
	if err != nil {
		return nil, err
	}

	queryStr := strings.Join(request.Query.Keywords, " ")

	for _, node := range nodes {
		relevanceScore := re.calculateL1Relevance(node, request.Query.Keywords, queryStr)
		if relevanceScore > 0.3 {
			recencyScore := re.calculateRecencyScore(node.UpdatedAt)

			memories = append(memories, ScoredMemory{
				Layer:          "l1",
				Path:           node.Path,
				Title:          node.Title,
				Summary:        node.Summary,
				RelevanceScore: relevanceScore,
				RecencyScore:   recencyScore,
				Timestamp:      node.UpdatedAt,
				L2Refs:         node.L2Refs,
			})
		}
	}

	return memories, nil
}

func (re *RetrievalEngine) searchL2Memories(request ContextRequest) ([]ScoredMemory, error) {
	var memories []ScoredMemory

	entries, err := re.l2Manager.SearchEntries(strings.Join(request.Query.Keywords, " "), []string{})
	if err != nil {
		return nil, err
	}

	const maxContentLen = 2000

	for _, entry := range entries {
		relevanceScore := re.calculateL2Relevance(entry, request.Query.Keywords)
		recencyScore := re.calculateRecencyScore(entry.UpdatedAt)

		title := entry.FilePath
		if t, ok := entry.Metadata["title"]; ok && t != "" {
			title = t
		}

		var contentStr string
		if contentBytes, err := re.l2Manager.GetContent(entry.ID); err == nil {
			contentStr = string(contentBytes)
			if len(contentStr) > maxContentLen {
				contentStr = contentStr[:maxContentLen] + "...[truncated]"
			}
		}

		memories = append(memories, ScoredMemory{
			Layer:          "l2",
			Path:           entry.FilePath,
			Title:          title,
			Summary:        fmt.Sprintf("Document: %s (%s)", entry.FilePath, entry.MimeType),
			Content:        contentStr,
			RelevanceScore: relevanceScore,
			RecencyScore:   recencyScore,
			Timestamp:      entry.UpdatedAt,
		})
	}

	return memories, nil
}

func (re *RetrievalEngine) calculateL1Relevance(node *memory.L1Node, keywords []string, queryStr string) float64 {
	var scores []float64

	if re.containsKeywords(node.Title, keywords) {
		scores = append(scores, 1.0)
	}
	if re.containsKeywords(node.Summary, keywords) {
		scores = append(scores, 0.8)
	}
	for _, keyword := range node.Keywords {
		if re.containsKeywords(keyword, keywords) {
			scores = append(scores, 0.6)
		}
	}
	if re.containsKeywords(node.Path, keywords) {
		scores = append(scores, 0.4)
	}

	if len(scores) == 0 {
		return 0
	}

	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}

	return maxScore
}

func (re *RetrievalEngine) calculateL2Relevance(entry *memory.L2Entry, keywords []string) float64 {
	score := 0.0
	if re.containsKeywords(entry.FilePath, keywords) {
		score += 0.7
	}
	for _, tag := range entry.Tags {
		if re.containsKeywords(tag, keywords) {
			score += 0.5
		}
	}
	return score
}

func (re *RetrievalEngine) calculateRecencyScore(timestamp time.Time) float64 {
	now := time.Now()
	daysSince := now.Sub(timestamp).Hours() / 24
	return 1.0 / (1.0 + daysSince/30.0)
}

func (re *RetrievalEngine) containsKeywords(text string, keywords []string) bool {
	textLower := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(textLower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func (re *RetrievalEngine) shouldSearchLayer(layer string, scope []string) bool {
	if len(scope) == 0 {
		return true
	}
	for _, s := range scope {
		if s == layer {
			return true
		}
	}
	return false
}

func (re *RetrievalEngine) assembleContextWithLLM(contextData struct {
	L0Structured     *L0StructuredContext `json:"l0_structured"`
	RelevantMemories []ScoredMemory       `json:"relevant_memories"`
	AssembledContext string               `json:"assembled_context"`
}, request ContextRequest) (string, error) {

	prompt, err := re.promptManager.LoadPrompt("context_assembly", "L0 Context Formatting")
	if err != nil {
		return "", err
	}

	templateData := map[string]interface{}{
		"timestamp":         time.Now(),
		"facts":             contextData.L0Structured.Facts,
		"relevant_memories": contextData.RelevantMemories,
		"target_length":     request.Requirements.TargetLength,
	}

	assembledPrompt, err := re.promptManager.RenderPrompt(prompt, templateData)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	completion, err := re.aiManager.Complete(ctx, assembledPrompt, ai.CompletionOptions{
		MaxTokens:   1000,
		Temperature: 0.3,
		Purpose:     "retrieval",
	})
	if err != nil {
		return assembledPrompt, nil
	}

	return completion.Content, nil
}

// RecallResponse is the result of a RecallMemory query.
type RecallResponse struct {
	Summary string        `json:"summary"`
	Entries []RecallEntry `json:"entries"`
}

type recallAIResponse struct {
	Paths   []string `json:"paths"`
	Summary string   `json:"summary"`
}

type RecallEntry struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
}

func (re *RetrievalEngine) RecallMemory(query string, maxTopics int) (*RecallResponse, error) {
	if maxTopics <= 0 {
		maxTopics = 5
	}

	nodes, err := re.l1Manager.GetTree()
	if err != nil {
		return nil, fmt.Errorf("failed to load L1 tree: %w", err)
	}

	if len(nodes) == 0 {
		return &RecallResponse{Summary: "No memories stored yet."}, nil
	}

	// Load L0 core facts to provide user context to AI
	coreFacts, _ := re.l0Manager.GetFactsJSON()

	var aiResp *recallAIResponse
	if len(nodes) <= 50 {
		aiResp, err = re.recallSinglePass(query, nodes, maxTopics, coreFacts)
	} else {
		aiResp, err = re.recallTwoPass(query, nodes, maxTopics, coreFacts)
	}
	if err != nil {
		return nil, fmt.Errorf("AI recall search failed: %w", err)
	}

	pathToNode := make(map[string]*memory.L1Node)
	for _, node := range nodes {
		pathToNode[node.Path] = node
	}

	const maxContentLen = 2000
	var entries []RecallEntry
	for _, path := range aiResp.Paths {
		node, ok := pathToNode[path]
		if !ok {
			continue
		}

		entry := RecallEntry{
			Path:    node.Path,
			Title:   node.Title,
			Summary: node.Summary,
		}

		if len(node.L2Refs) > 0 {
			if contentBytes, err := re.l2Manager.GetContent(node.L2Refs[0]); err == nil {
				content := string(contentBytes)
				if len(content) > maxContentLen {
					content = content[:maxContentLen] + "...[truncated]"
				}
				entry.Content = content
			}
		}

		entries = append(entries, entry)
	}

	return &RecallResponse{
		Summary: aiResp.Summary,
		Entries: entries,
	}, nil
}

func (re *RetrievalEngine) recallSinglePass(query string, nodes map[string]*memory.L1Node, maxTopics int, coreFacts string) (*recallAIResponse, error) {
	treeSummary := re.buildTreeSummary(nodes, "")
	return re.callRecallAI(query, treeSummary, maxTopics, coreFacts)
}

func (re *RetrievalEngine) recallTwoPass(query string, nodes map[string]*memory.L1Node, maxTopics int, coreFacts string) (*recallAIResponse, error) {
	topSummary := re.buildTreeSummary(nodes, "")
	var topLines []string
	for _, line := range strings.Split(topSummary, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) < 1 {
			continue
		}
		pathDepth := len(strings.Split(strings.TrimSpace(parts[0]), "/"))
		if pathDepth <= 2 {
			topLines = append(topLines, line)
		}
	}

	pass1Resp, err := re.callRecallAI(query, strings.Join(topLines, "\n"), 3, coreFacts)
	if err != nil {
		return nil, err
	}

	if len(pass1Resp.Paths) == 0 {
		return pass1Resp, nil
	}

	subSummary := re.buildTreeSummary(nodes, pass1Resp.Paths...)
	return re.callRecallAI(query, subSummary, maxTopics, coreFacts)
}

func (re *RetrievalEngine) buildTreeSummary(nodes map[string]*memory.L1Node, prefixFilters ...string) string {
	type pathNode struct {
		path    string
		summary string
	}
	var sorted []pathNode
	for _, node := range nodes {
		if len(prefixFilters) > 0 && prefixFilters[0] != "" {
			matched := false
			for _, prefix := range prefixFilters {
				if strings.HasPrefix(node.Path, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		summary := node.Summary
		if summary == "" {
			summary = node.Title
		}
		sorted = append(sorted, pathNode{path: node.Path, summary: summary})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].path < sorted[j].path
	})

	var sb strings.Builder
	for _, pn := range sorted {
		sb.WriteString(pn.path)
		sb.WriteString(": ")
		sb.WriteString(pn.summary)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (re *RetrievalEngine) callRecallAI(query, treeSummary string, maxTopics int, coreFacts string) (*recallAIResponse, error) {
	templateStr, err := re.promptManager.LoadTemplateFile("retrieval", "recall_tree_search")
	if err != nil {
		return nil, fmt.Errorf("failed to load recall prompt: %w", err)
	}

	templateData := map[string]interface{}{
		"query":        query,
		"tree_summary": treeSummary,
		"max_topics":   maxTopics,
		"core_facts":   coreFacts,
	}

	prompt, err := re.promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render recall prompt: %w", err)
	}

	ctx := context.Background()
	completion, err := re.aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   800,
		Temperature: 0.2,
		Purpose:     "retrieval",
	})
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	content := strings.TrimSpace(completion.Content)
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		if strings.HasSuffix(content, "```") {
			content = content[:len(content)-3]
		}
		content = strings.TrimSpace(content)
	}

	var resp recallAIResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w (response: %.200s)", err, content)
	}

	return &resp, nil
}
