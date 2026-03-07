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
		Type        string   `json:"type"`         // topic, person, event, skill
		Keywords    []string `json:"keywords"`
		TimeRange   TimeRange `json:"time_range"`
		Scope       []string `json:"scope"`        // l0, l1, l2
		MaxItems    int      `json:"max_items"`
	} `json:"query"`
	
	Requirements struct {
		IncludeL0Structure bool   `json:"include_l0_structure"`
		IncludeRecency     bool   `json:"include_recency"`
		Summarize          bool   `json:"summarize"`
		TargetLength       string `json:"target_length"` // brief, detailed, comprehensive
	} `json:"context_requirements"`
}

type TimeRange struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Priority string `json:"priority"` // recent, all, relevant
}

type ContextResponse struct {
	Context struct {
		L0Structured     *L0StructuredContext `json:"l0_structured"`
		RelevantMemories []ScoredMemory       `json:"relevant_memories"`
		AssembledContext string               `json:"assembled_context"`
	} `json:"context"`
}

type L0StructuredContext struct {
	LongTermTraits map[string]map[string]TimestampedInfo `json:"long_term_traits"`
	RecentAgenda   RecentAgendaInfo                      `json:"recent_agenda"`
}

type TimestampedInfo struct {
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RecentAgendaInfo struct {
	CurrentFocus []AgendaInfo `json:"current_focus"`
}

type AgendaInfo struct {
	Item        string    `json:"item"`
	Priority    string    `json:"priority"`
	Since       time.Time `json:"since"`
	LastUpdated time.Time `json:"last_updated"`
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
// This is meant to be called at the start of every conversation to give the chatbot context.
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

	// Traits — grouped by category, with recency annotation
	if len(l0Data.Traits) > 0 {
		sb.WriteString("## Who You Are\n\n")

		// Sort categories for stable output
		categories := make([]string, 0, len(l0Data.Traits))
		for cat := range l0Data.Traits {
			categories = append(categories, cat)
		}
		sort.Strings(categories)

		for _, category := range categories {
			keys := l0Data.Traits[category]
			type traitEntry struct {
				key        string
				value      string
				observedAt string
			}
			var currentTraits []traitEntry

			for key, timeline := range keys {
				for _, tv := range timeline {
					if tv.ValidUntil == "" { // current value
						currentTraits = append(currentTraits, traitEntry{
							key:        key,
							value:      tv.Value,
							observedAt: tv.ObservedAt,
						})
					}
				}
			}

			if len(currentTraits) == 0 {
				continue
			}

			// Sort by key for stability
			sort.Slice(currentTraits, func(i, j int) bool {
				return currentTraits[i].key < currentTraits[j].key
			})

			sb.WriteString(fmt.Sprintf("### %s\n", strings.Title(category)))
			for _, t := range currentTraits {
				recency := formatRecency(t.observedAt, now)
				sb.WriteString(fmt.Sprintf("- **%s**: %s", t.key, t.value))
				if recency != "" {
					sb.WriteString(fmt.Sprintf(" _%s_", recency))
				}
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	// Active agenda — sorted by priority, with recency
	activeAgenda := []memory.AgendaItem{}
	for _, item := range l0Data.Agenda {
		if item.Status == "active" {
			activeAgenda = append(activeAgenda, item)
		}
	}

	if len(activeAgenda) > 0 {
		// Sort: high > medium > low, then by LastUpdated desc
		priorityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
		sort.Slice(activeAgenda, func(i, j int) bool {
			pi := priorityOrder[activeAgenda[i].Priority]
			pj := priorityOrder[activeAgenda[j].Priority]
			if pi != pj {
				return pi < pj
			}
			return activeAgenda[i].LastUpdated > activeAgenda[j].LastUpdated
		})

		sb.WriteString("## Current Focus\n\n")
		for _, item := range activeAgenda {
			recency := formatRecency(item.LastUpdated, now)
			if recency == "" {
				recency = formatRecency(item.Since, now)
			}
			sb.WriteString(fmt.Sprintf("- **[%s]** %s", strings.ToUpper(item.Priority), item.Item))
			if recency != "" {
				sb.WriteString(fmt.Sprintf(" _%s_", recency))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// L0 meta
	sb.WriteString(fmt.Sprintf("---\n*Last updated: %s*\n", l0Data.Meta.LastUpdated.Format("2006-01-02 15:04")))

	return sb.String(), nil
}

// formatRecency converts a date string to a human-readable recency label.
func formatRecency(dateStr string, now time.Time) string {
	if dateStr == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return ""
	}
	days := int(now.Sub(t).Hours() / 24)
	switch {
	case days < 0:
		return ""
	case days == 0:
		return "(today)"
	case days == 1:
		return "(yesterday)"
	case days < 7:
		return fmt.Sprintf("(%d days ago)", days)
	case days < 30:
		weeks := days / 7
		if weeks == 1 {
			return "(1 week ago)"
		}
		return fmt.Sprintf("(%d weeks ago)", weeks)
	case days < 365:
		months := days / 30
		if months == 1 {
			return "(1 month ago)"
		}
		return fmt.Sprintf("(%d months ago)", months)
	default:
		return fmt.Sprintf("(since %s)", dateStr)
	}
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
	
	context := &L0StructuredContext{
		LongTermTraits: make(map[string]map[string]TimestampedInfo),
		RecentAgenda: RecentAgendaInfo{
			CurrentFocus: []AgendaInfo{},
		},
	}
	
	// Convert dynamic traits
	for category, keys := range l0Data.Traits {
		context.LongTermTraits[category] = make(map[string]TimestampedInfo)
		for key, timeline := range keys {
			// Use the current value (ValidUntil is empty)
			for _, tv := range timeline {
				if tv.ValidUntil == "" {
					parsedTime, _ := time.Parse("2006-01-02", tv.ObservedAt)
					context.LongTermTraits[category][key] = TimestampedInfo{
						Value:     tv.Value,
						UpdatedAt: parsedTime,
					}
				}
			}
		}
	}

	// Convert agenda
	for _, item := range l0Data.Agenda {
		if item.Status != "active" {
			continue
		}
		since, _ := time.Parse("2006-01-02", item.Since)
		lastUpdated, _ := time.Parse("2006-01-02", item.LastUpdated)
		context.RecentAgenda.CurrentFocus = append(context.RecentAgenda.CurrentFocus, AgendaInfo{
			Item:        item.Item,
			Priority:    item.Priority,
			Since:       since,
			LastUpdated: lastUpdated,
		})
	}
	
	return context, nil
}

func (re *RetrievalEngine) searchRelevantMemories(request ContextRequest) ([]ScoredMemory, error) {
	var memories []ScoredMemory
	
	// Search L1 if requested
	if re.shouldSearchLayer("l1", request.Query.Scope) {
		l1Memories, err := re.searchL1Memories(request)
		if err != nil {
			return nil, err
		}
		memories = append(memories, l1Memories...)
	}
	
	// Search L2 if requested
	if re.shouldSearchLayer("l2", request.Query.Scope) {
		l2Memories, err := re.searchL2Memories(request)
		if err != nil {
			return nil, err
		}
		memories = append(memories, l2Memories...)
	}
	
	// Sort by relevance and recency
	sort.Slice(memories, func(i, j int) bool {
		// Combined score: 70% relevance, 30% recency
		scoreI := memories[i].RelevanceScore*0.7 + memories[i].RecencyScore*0.3
		scoreJ := memories[j].RelevanceScore*0.7 + memories[j].RecencyScore*0.3
		return scoreI > scoreJ
	})
	
	// Limit results
	if request.Query.MaxItems > 0 && len(memories) > request.Query.MaxItems {
		memories = memories[:request.Query.MaxItems]
	}
	
	return memories, nil
}

func (re *RetrievalEngine) searchL1Memories(request ContextRequest) ([]ScoredMemory, error) {
	var memories []ScoredMemory
	
	// Get all L1 nodes
	nodes, err := re.l1Manager.GetTree()
	if err != nil {
		return nil, err
	}
	
	queryStr := strings.Join(request.Query.Keywords, " ")
	
	for _, node := range nodes {
		relevanceScore := re.calculateL1Relevance(node, request.Query.Keywords, queryStr)
		if relevanceScore > 0.3 { // Minimum relevance threshold
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

		// Read actual content
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
	
	// Title match
	if re.containsKeywords(node.Title, keywords) {
		scores = append(scores, 1.0)
	}
	
	// Summary match
	if re.containsKeywords(node.Summary, keywords) {
		scores = append(scores, 0.8)
	}
	
	// Keywords match
	for _, keyword := range node.Keywords {
		if re.containsKeywords(keyword, keywords) {
			scores = append(scores, 0.6)
		}
	}
	
	// Path match
	if re.containsKeywords(node.Path, keywords) {
		scores = append(scores, 0.4)
	}
	
	if len(scores) == 0 {
		return 0
	}
	
	// Return highest score
	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}
	
	return maxScore
}

func (re *RetrievalEngine) calculateL2Relevance(entry *memory.L2Entry, keywords []string) float64 {
	// Simple relevance based on file path and tags
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
	
	// Exponential decay: score = e^(-days/30)
	// Recent content (within 30 days) gets higher score
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
		return true // Search all layers by default
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
	
	// Load context assembly prompt
	prompt, err := re.promptManager.LoadPrompt("context_assembly", "L0 Context Formatting")
	if err != nil {
		return "", err
	}
	
	// Prepare data for template
	templateData := map[string]interface{}{
		"timestamp":      time.Now(),
		"long_term_traits": contextData.L0Structured.LongTermTraits,
		"recent_agenda":   contextData.L0Structured.RecentAgenda,
		"relevant_memories": contextData.RelevantMemories,
		"target_length":    request.Requirements.TargetLength,
	}
	
	// Render prompt
	assembledPrompt, err := re.promptManager.RenderPrompt(prompt, templateData)
	if err != nil {
		return "", err
	}
	
	// Call AI provider to process the context
	ctx := context.Background()
	completion, err := re.aiManager.Complete(ctx, assembledPrompt, ai.CompletionOptions{
		MaxTokens:   1000,
		Temperature: 0.3,
	})
	if err != nil {
		// If AI call fails, return the formatted template as fallback
		return assembledPrompt, nil
	}
	
	return completion.Content, nil
}

// RecallResponse is the result of a RecallMemory query.
type RecallResponse struct {
	Profile        string        `json:"profile"`
	RelevantTopics []RecallTopic `json:"relevant_topics"`
}

// RecallTopic represents a matched L1 node with its L2 content.
type RecallTopic struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
}

// RecallMemory performs AI-powered semantic search on the L1 tree.
// It always includes the L0 profile and returns matching topics with L2 content.
func (re *RetrievalEngine) RecallMemory(query string, maxTopics int) (*RecallResponse, error) {
	if maxTopics <= 0 {
		maxTopics = 5
	}

	// 1. Load L0 profile (always included)
	profile, err := re.l0Manager.GetContext()
	if err != nil {
		profile = "(profile unavailable)"
	}

	// 2. Load L1 tree
	nodes, err := re.l1Manager.GetTree()
	if err != nil {
		return &RecallResponse{Profile: profile}, nil
	}

	if len(nodes) == 0 {
		return &RecallResponse{Profile: profile}, nil
	}

	// 3. Select relevant nodes via AI
	var selectedPaths []string
	if len(nodes) <= 50 {
		selectedPaths, err = re.recallSinglePass(query, nodes, maxTopics)
	} else {
		selectedPaths, err = re.recallTwoPass(query, nodes, maxTopics)
	}
	if err != nil {
		return &RecallResponse{Profile: profile}, nil
	}

	// 4. Build path→node lookup
	pathToNode := make(map[string]*memory.L1Node)
	for _, node := range nodes {
		pathToNode[node.Path] = node
	}

	// 5. Assemble topics with L2 content
	const maxContentLen = 2000
	var topics []RecallTopic
	for _, path := range selectedPaths {
		node, ok := pathToNode[path]
		if !ok {
			continue
		}

		topic := RecallTopic{
			Path:    node.Path,
			Title:   node.Title,
			Summary: node.Summary,
		}

		// Read L2 content from first ref
		if len(node.L2Refs) > 0 {
			if contentBytes, err := re.l2Manager.GetContent(node.L2Refs[0]); err == nil {
				content := string(contentBytes)
				if len(content) > maxContentLen {
					content = content[:maxContentLen] + "...[truncated]"
				}
				topic.Content = content
			}
		}

		topics = append(topics, topic)
	}

	return &RecallResponse{
		Profile:        profile,
		RelevantTopics: topics,
	}, nil
}

// recallSinglePass sends the full tree to AI in one call (≤50 nodes).
func (re *RetrievalEngine) recallSinglePass(query string, nodes map[string]*memory.L1Node, maxTopics int) ([]string, error) {
	treeSummary := re.buildTreeSummary(nodes, "")
	return re.callRecallAI(query, treeSummary, maxTopics)
}

// recallTwoPass uses two AI calls for large trees (>50 nodes).
// Pass 1: select top branches from depth 1-2
// Pass 2: select specific nodes from within those branches
func (re *RetrievalEngine) recallTwoPass(query string, nodes map[string]*memory.L1Node, maxTopics int) ([]string, error) {
	// Pass 1: Build top-level summary (depth ≤ 2)
	topSummary := re.buildTreeSummary(nodes, "")
	// Filter to depth ≤ 2
	var topLines []string
	for _, line := range strings.Split(topSummary, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Count depth by path segments
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) < 1 {
			continue
		}
		pathDepth := len(strings.Split(strings.TrimSpace(parts[0]), "/"))
		if pathDepth <= 2 {
			topLines = append(topLines, line)
		}
	}

	branchPaths, err := re.callRecallAI(query, strings.Join(topLines, "\n"), 3)
	if err != nil {
		return nil, err
	}

	if len(branchPaths) == 0 {
		return nil, nil
	}

	// Pass 2: Build summary of nodes under selected branches
	subSummary := re.buildTreeSummary(nodes, branchPaths...)
	return re.callRecallAI(query, subSummary, maxTopics)
}

// buildTreeSummary creates a compact "path: summary" representation.
// If prefixFilters are provided, only include nodes whose path starts with one of them.
func (re *RetrievalEngine) buildTreeSummary(nodes map[string]*memory.L1Node, prefixFilters ...string) string {
	// Sort paths for stable output
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

// callRecallAI calls the AI with the recall_tree_search prompt and parses the JSON response.
func (re *RetrievalEngine) callRecallAI(query, treeSummary string, maxTopics int) ([]string, error) {
	templateStr, err := re.promptManager.LoadTemplateFile("retrieval", "recall_tree_search")
	if err != nil {
		return nil, fmt.Errorf("failed to load recall prompt: %w", err)
	}

	templateData := map[string]interface{}{
		"query":        query,
		"tree_summary": treeSummary,
		"max_topics":   maxTopics,
	}

	prompt, err := re.promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render recall prompt: %w", err)
	}

	ctx := context.Background()
	completion, err := re.aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   300,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON array response
	content := strings.TrimSpace(completion.Content)
	// Strip markdown code blocks if present
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		if strings.HasSuffix(content, "```") {
			content = content[:len(content)-3]
		}
		content = strings.TrimSpace(content)
	}

	var paths []string
	if err := json.Unmarshal([]byte(content), &paths); err != nil {
		return nil, fmt.Errorf("failed to parse AI response as JSON array: %w (response: %.200s)", err, content)
	}

	return paths, nil
}