package retrieval

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type RetrievalEngine struct {
	l0Manager       *memory.L0Manager
	l1Manager       *memory.L1Manager
	l2Manager       *memory.L2Manager
	promptManager   *prompts.PromptManager
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
	RelevanceScore float64   `json:"relevance_score"`
	RecencyScore   float64   `json:"recency_score"`
	Timestamp      time.Time `json:"timestamp"`
	L2Refs         []string  `json:"l2_refs,omitempty"`
}

func NewRetrievalEngine(l0Manager *memory.L0Manager, l1Manager *memory.L1Manager, l2Manager *memory.L2Manager, promptManager *prompts.PromptManager) *RetrievalEngine {
	return &RetrievalEngine{
		l0Manager:     l0Manager,
		l1Manager:     l1Manager,
		l2Manager:     l2Manager,
		promptManager: promptManager,
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
	
	// Convert long-term traits
	context.LongTermTraits["personality"] = make(map[string]TimestampedInfo)
	for k, v := range l0Data.LongTermTraits.Personality {
		context.LongTermTraits["personality"][k] = TimestampedInfo{
			Value:     v.Value,
			UpdatedAt: v.UpdatedAt,
		}
	}
	
	context.LongTermTraits["philosophy"] = make(map[string]TimestampedInfo)
	for k, v := range l0Data.LongTermTraits.Philosophy {
		context.LongTermTraits["philosophy"][k] = TimestampedInfo{
			Value:     v.Value,
			UpdatedAt: v.UpdatedAt,
		}
	}
	
	context.LongTermTraits["background"] = make(map[string]TimestampedInfo)
	for k, v := range l0Data.LongTermTraits.Background {
		context.LongTermTraits["background"][k] = TimestampedInfo{
			Value:     v.Value,
			UpdatedAt: v.UpdatedAt,
		}
	}
	
	context.LongTermTraits["skills"] = make(map[string]TimestampedInfo)
	for k, v := range l0Data.LongTermTraits.Skills {
		context.LongTermTraits["skills"][k] = TimestampedInfo{
			Value:     v.Value,
			UpdatedAt: v.UpdatedAt,
		}
	}
	
	// Convert recent agenda
	for _, item := range l0Data.RecentAgenda.CurrentFocus {
		context.RecentAgenda.CurrentFocus = append(context.RecentAgenda.CurrentFocus, AgendaInfo{
			Item:        item.Item,
			Priority:    item.Priority,
			Since:       item.Since,
			LastUpdated: item.LastUpdated,
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
	
	// Search L2 entries
	entries, err := re.l2Manager.SearchEntries(strings.Join(request.Query.Keywords, " "), []string{})
	if err != nil {
		return nil, err
	}
	
	for _, entry := range entries {
		relevanceScore := re.calculateL2Relevance(entry, request.Query.Keywords)
		recencyScore := re.calculateRecencyScore(entry.UpdatedAt)
		
		memories = append(memories, ScoredMemory{
			Layer:          "l2",
			Path:           entry.FilePath,
			Summary:        fmt.Sprintf("Document: %s (%s)", entry.FilePath, entry.MimeType),
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

func (re *RetrievalEngine) assembleContextWithLLM(context struct {
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
		"long_term_traits": context.L0Structured.LongTermTraits,
		"recent_agenda":   context.L0Structured.RecentAgenda,
		"relevant_memories": context.RelevantMemories,
		"target_length":    request.Requirements.TargetLength,
	}
	
	// Render prompt
	assembledContext, err := re.promptManager.RenderPrompt(prompt, templateData)
	if err != nil {
		return "", err
	}
	
	// TODO: Here we would call an actual LLM to process the prompt
	// For now, return the formatted template
	return assembledContext, nil
}