package importers

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"yumem/internal/ai"
	"yumem/internal/logging"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

type ImportResult struct {
	TotalProcessed int      `json:"total_processed"`
	Skipped        int      `json:"skipped"`
	L0Updates      int      `json:"l0_updates"`
	L1Created      int      `json:"l1_created"`
	L2Created      int      `json:"l2_created"`
	Errors         []string `json:"errors"`
}

// AnalysisResult holds IDs of L0 facts and L1 nodes created during analysis.
type AnalysisResult struct {
	L0FactIDs []string
	L1NodeIDs []string
}

type BaseImporter struct {
	l0Manager     *memory.L0Manager
	l1Manager     *memory.L1Manager
	l2Manager     *memory.L2Manager
	promptManager *prompts.PromptManager
	aiManager     *ai.Manager

	// Auto-consolidation tracking
	itemsSinceConsolidate int
}

type ImportItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Source      string    `json:"source"`
	ContentDate time.Time `json:"content_date"` // original creation date of the content
}

// AI response structures
type ContentAnalysisResult struct {
	L0Facts   []FactUpdate  `json:"l0_facts"`
	L1Node    *L1NodeResult `json:"l1_node"`
	Reasoning string        `json:"reasoning"`
}

type FactUpdate struct {
	Text       string `json:"text"`
	SourceName string `json:"source_name,omitempty"`
}

type L1NodeResult struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Keywords []string `json:"keywords"`
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

// StoreItem stores a single item in L2 only (no AI analysis).
// Returns the L2 entry ID.
func (bi *BaseImporter) StoreItem(item ImportItem, result *ImportResult) (string, error) {
	log := logging.Get()
	log.Info("import", fmt.Sprintf("storing item: %s (source=%s)", item.Title, item.Source))

	l2Tags := []string{"imported", item.Source}
	l2Entry, err := bi.l2Manager.AddEntry(item.Title, item.Content, "imported_content", item.Source, l2Tags)
	if err != nil {
		log.Error("import", fmt.Sprintf("L2 store failed for %s: %v", item.Title, err))
		return "", fmt.Errorf("failed to store L2 entry: %w", err)
	}
	result.L2Created++
	fmt.Printf("  📄 L2 stored: %s\n", l2Entry.ID)
	log.Debug("import", fmt.Sprintf("L2 stored: %s", l2Entry.ID))

	// Store content_date in L2 metadata for later indexing
	if !item.ContentDate.IsZero() {
		_ = bi.l2Manager.UpdateMetadata(l2Entry.ID, map[string]string{
			"content_date": item.ContentDate.Format("2006-01-02"),
		})
	}

	return l2Entry.ID, nil
}

// ProcessItem handles the full import pipeline for a single item:
// 1. Store raw content in L2 (get ID)
// 2. AI analysis (one call → L0 updates + L1 node)
// 3. Apply L0 updates and create L1 node
func (bi *BaseImporter) ProcessItem(item ImportItem, result *ImportResult) (string, error) {
	l2ID, err := bi.StoreItem(item, result)
	if err != nil {
		return "", err
	}

	// Run analysis and apply L0/L1 updates
	_, analysisErr := bi.AnalyzeAndApply(l2ID, item.Title, item.Content, item.Source, item.ContentDate, result)
	return l2ID, analysisErr
}

// AnalyzeAndApply runs AI analysis on already-stored L2 content and applies L0/L1 updates.
// contentDate is the original creation date of the content (for ObservedAt). Zero value means use time.Now().
// This is used by ProcessItem (after L2 creation) and by store_memory (on existing L2 entries).
// Returns an AnalysisResult with IDs of created L0 facts and L1 nodes.
func (bi *BaseImporter) AnalyzeAndApply(l2ID, title, content, source string, contentDate time.Time, result *ImportResult) (*AnalysisResult, error) {
	log := logging.Get()
	log.Info("import", fmt.Sprintf("analyzing content: %s (l2=%s)", title, l2ID))

	analysisResult := &AnalysisResult{}

	item := ImportItem{
		Title:       title,
		Content:     content,
		Source:      source,
		ContentDate: contentDate,
	}

	analysis, err := bi.analyzeContent(item, l2ID)
	if err != nil {
		log.Warn("import", fmt.Sprintf("AI analysis failed for %s: %v", title, err))
		fmt.Printf("  ⚠️  AI analysis failed: %v (L2 content preserved)\n", err)
		return analysisResult, nil // Analysis failure is non-fatal
	}

	// Apply L0 facts
	observedAt := time.Now()
	if !contentDate.IsZero() {
		observedAt = contentDate
	}
	if len(analysis.L0Facts) > 0 {
		var facts []memory.Fact
		for _, fu := range analysis.L0Facts {
			facts = append(facts, memory.Fact{
				Text:       fu.Text,
				ObservedAt: observedAt.Format("2006-01-02"),
				Source:     l2ID,
				SourceName: fu.SourceName,
			})
		}
		if err := bi.l0Manager.AddFacts(facts); err != nil {
			fmt.Printf("  ⚠️  L0 facts update failed: %v\n", err)
		} else {
			if result != nil {
				result.L0Updates += len(facts)
			}
			for _, f := range facts {
				analysisResult.L0FactIDs = append(analysisResult.L0FactIDs, f.Source+":"+f.SourceName)
			}
			fmt.Printf("  🧠 L0 updated: %d facts\n", len(facts))
		}
	}

	// Create L1 node
	if analysis.L1Node != nil && analysis.L1Node.Path != "" {
		node, err := bi.l1Manager.CreateNode(
			analysis.L1Node.Path,
			analysis.L1Node.Title,
			analysis.L1Node.Summary,
			analysis.L1Node.Keywords,
			[]string{l2ID},
		)
		if err != nil {
			fmt.Printf("  ⚠️  L1 creation failed: %v\n", err)
		} else {
			if result != nil {
				result.L1Created++
			}
			analysisResult.L1NodeIDs = append(analysisResult.L1NodeIDs, node.ID)
			fmt.Printf("  📂 L1 created: %s\n", analysis.L1Node.Path)
		}
	}

	// Mark L2 entry as indexed
	_ = bi.l2Manager.UpdateMetadata(l2ID, map[string]string{
		"indexed":    "true",
		"indexed_at": time.Now().Format(time.RFC3339),
	})

	// Create conversation index node if L2 entry is a conversation
	bi.maybeCreateConversationIndex(l2ID, analysis, analysisResult)

	// Auto-consolidate if L0 is oversize (at most once per 10 items)
	bi.itemsSinceConsolidate++
	if bi.itemsSinceConsolidate >= 10 && bi.l0Manager.IsOversize() {
		fmt.Printf("  🔄 L0 oversize, running auto-consolidation...\n")
		log.Info("import", "L0 oversize detected, running auto-consolidation")
		if cr, err := bi.RunConsolidation(); err != nil {
			log.Warn("import", fmt.Sprintf("auto-consolidation failed: %v", err))
			fmt.Printf("  ⚠️  Auto-consolidation failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ Consolidated: facts %d→%d\n",
				cr.FactsBefore, cr.FactsAfter)
		}
		bi.itemsSinceConsolidate = 0
	}

	return analysisResult, nil
}

// maybeCreateConversationIndex creates an L1 conversation index node
// if the L2 entry is a conversation type.
func (bi *BaseImporter) maybeCreateConversationIndex(l2ID string, analysis *ContentAnalysisResult, ar *AnalysisResult) {
	if bi.l2Manager == nil || bi.l1Manager == nil {
		return
	}

	entry, err := bi.l2Manager.GetEntry(l2ID)
	if err != nil || entry.Type != "conversation" {
		return
	}

	sessionID := entry.Metadata["session_id"]
	if sessionID == "" {
		return
	}

	meta, err := bi.l2Manager.GetConversationMeta(l2ID)
	if err != nil {
		return
	}

	convPath := "conversations/" + sessionID
	summary := analysis.Reasoning
	if analysis.L1Node != nil {
		summary = analysis.L1Node.Summary
	}

	var keywords []string
	if analysis.L1Node != nil {
		keywords = analysis.L1Node.Keywords
	}

	node, err := bi.l1Manager.CreateNode(convPath, meta.Title, summary, keywords, []string{l2ID})
	if err != nil {
		fmt.Printf("  ⚠️  Conversation index node creation failed: %v\n", err)
		return
	}

	// Store fine-grained references in metadata
	metadata := map[string]string{}
	if len(ar.L0FactIDs) > 0 {
		metadata["l0_fact_ids"] = strings.Join(ar.L0FactIDs, ",")
	}
	if len(ar.L1NodeIDs) > 0 {
		metadata["l1_refs"] = strings.Join(ar.L1NodeIDs, ",")
	}
	if len(metadata) > 0 {
		// Update the node's metadata directly
		if existingNode, err := bi.l1Manager.GetNode(node.ID); err == nil {
			for k, v := range metadata {
				existingNode.Metadata[k] = v
			}
		}
	}

	fmt.Printf("  💬 Conversation index created: %s\n", convPath)
}

// StoreAsConversation parses file content as a conversation and stores it
// in the L2 conversation structure. Returns the L2 entry ID.
//
// Parsing strategy:
// 1. Try direct JSON parse (for structured chat exports)
// 2. Fall back to AI parsing (for free-form text)
func (bi *BaseImporter) StoreAsConversation(item ImportItem, result *ImportResult) (string, error) {
	log := logging.Get()
	log.Info("import", fmt.Sprintf("parsing as conversation: %s", item.Title))

	// Try direct JSON parse first
	var messages []memory.Message
	var convTitle string
	var convModel string
	var convCreatedAt string

	parsed, err := bi.tryParseConversationJSON(item.Content)
	if err != nil {
		// Fall back to AI parsing
		log.Info("import", fmt.Sprintf("not valid JSON, trying AI parse: %s", item.Title))
		messages, err = bi.aiParseConversation(item)
		if err != nil {
			return "", err
		}
	} else {
		messages = parsed.Messages
		convTitle = parsed.Title
		convModel = parsed.Model
		convCreatedAt = parsed.CreatedAt
	}

	if len(messages) == 0 {
		return "", fmt.Errorf("no messages parsed from %s", item.Title)
	}

	// Use extracted title if available, fall back to filename
	title := item.Title
	if convTitle != "" {
		title = convTitle
	}

	// Generate session ID from title
	sessionID := fmt.Sprintf("import-%x", md5.Sum([]byte(item.Title+item.Source)))[:20]

	entry, err := bi.l2Manager.CreateConversation(sessionID, title, item.Source)
	if err != nil {
		return "", fmt.Errorf("failed to create conversation: %w", err)
	}

	// Update conversation meta with extracted timestamps
	if convCreatedAt != "" {
		bi.l2Manager.UpdateConversationMeta(entry.ID, func(meta *memory.ConversationMeta) {
			meta.CreatedAt = convCreatedAt
		})
	}

	for i, msg := range messages {
		if msg.ID == "" {
			msg.ID = fmt.Sprintf("msg-%03d", i)
		}
		if msg.Timestamp == "" {
			msg.Timestamp = time.Now().Format(time.RFC3339)
		}
		// Fill model from wrapper metadata if message doesn't have one
		if msg.Model == "" && convModel != "" {
			msg.Model = convModel
		}
		if err := bi.l2Manager.AddMessage(entry.ID, msg); err != nil {
			log.Warn("import", fmt.Sprintf("failed to add message %d: %v", i, err))
		}
	}

	result.L2Created++
	fmt.Printf("  💬 Conversation stored: %s (%d messages)\n", entry.ID, len(messages))

	return entry.ID, nil
}

// parsedConversation holds messages and optional metadata extracted from JSON.
type parsedConversation struct {
	Messages  []memory.Message
	Title     string // extracted from wrapper (e.g. ChatWise "title")
	Model     string // extracted from wrapper (e.g. "model")
	CreatedAt string // extracted from wrapper (e.g. "createdAt", "created_at")
}

// tryParseConversationJSON attempts to parse content as a JSON array of messages.
// Supports multiple common chat export formats and extracts wrapper metadata.
func (bi *BaseImporter) tryParseConversationJSON(content string) (*parsedConversation, error) {
	content = strings.TrimSpace(content)

	// Try direct array of messages: [{"role": "user", "content": "..."}]
	var directMessages []memory.Message
	if err := json.Unmarshal([]byte(content), &directMessages); err == nil && len(directMessages) > 0 {
		if directMessages[0].Role != "" && directMessages[0].Content != "" {
			return &parsedConversation{Messages: directMessages}, nil
		}
	}

	// Try common wrapper formats: {"messages": [...]} or {"conversation": [...]}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil {
		for _, key := range []string{"messages", "conversation", "chat", "data", "turns"} {
			if raw, ok := wrapper[key]; ok {
				var msgs []memory.Message
				if err := json.Unmarshal(raw, &msgs); err == nil && len(msgs) > 0 {
					if msgs[0].Role != "" && msgs[0].Content != "" {
						pc := &parsedConversation{Messages: msgs}
						pc.extractWrapperMeta(wrapper)
						return pc, nil
					}
				}
				// Try flexible format: role might be "sender", content might be "text"
				var flexMsgs []map[string]any
				if err := json.Unmarshal(raw, &flexMsgs); err == nil && len(flexMsgs) > 0 {
					pc := &parsedConversation{Messages: convertFlexMessages(flexMsgs)}
					pc.extractWrapperMeta(wrapper)
					return pc, nil
				}
			}
		}
	}

	// Try flexible array format with non-standard field names
	var flexArray []map[string]any
	if err := json.Unmarshal([]byte(content), &flexArray); err == nil && len(flexArray) > 0 {
		msgs := convertFlexMessages(flexArray)
		if len(msgs) > 0 {
			return &parsedConversation{Messages: msgs}, nil
		}
	}

	return nil, fmt.Errorf("not a recognized JSON conversation format")
}

// extractWrapperMeta extracts title, model, createdAt from a JSON wrapper object.
func (pc *parsedConversation) extractWrapperMeta(wrapper map[string]json.RawMessage) {
	getString := func(keys ...string) string {
		for _, k := range keys {
			if raw, ok := wrapper[k]; ok {
				var s string
				if json.Unmarshal(raw, &s) == nil && s != "" {
					return s
				}
			}
		}
		return ""
	}
	pc.Title = getString("title", "name", "subject")
	pc.Model = getString("model", "model_id")
	pc.CreatedAt = getString("createdAt", "created_at", "date", "timestamp")
}

// convertFlexMessages converts messages with non-standard field names.
func convertFlexMessages(items []map[string]any) []memory.Message {
	var messages []memory.Message
	for _, item := range items {
		msg := memory.Message{}

		// Role: try role, sender, author, from
		for _, key := range []string{"role", "sender", "author", "from"} {
			if v, ok := item[key]; ok {
				msg.Role = fmt.Sprintf("%v", v)
				break
			}
		}

		// Content: try content, text, message, body
		for _, key := range []string{"content", "text", "message", "body"} {
			if v, ok := item[key]; ok {
				msg.Content = fmt.Sprintf("%v", v)
				break
			}
		}

		// Timestamp: try timestamp, time, created_at, date
		for _, key := range []string{"timestamp", "time", "created_at", "date", "createdAt"} {
			if v, ok := item[key]; ok {
				msg.Timestamp = fmt.Sprintf("%v", v)
				break
			}
		}

		// Model
		if v, ok := item["model"]; ok {
			msg.Model = fmt.Sprintf("%v", v)
		}

		if msg.Role != "" && msg.Content != "" {
			// Normalize role
			roleLower := strings.ToLower(msg.Role)
			if roleLower == "human" || roleLower == "user" || roleLower == "me" {
				msg.Role = "user"
			} else if roleLower == "ai" || roleLower == "bot" || roleLower == "system" {
				msg.Role = "assistant"
			} else if roleLower != "user" && roleLower != "assistant" {
				msg.Role = "assistant"
			}
			messages = append(messages, msg)
		}
	}
	return messages
}

// aiParseConversation uses AI to parse free-form text into messages.
func (bi *BaseImporter) aiParseConversation(item ImportItem) ([]memory.Message, error) {
	if bi.aiManager == nil {
		return nil, fmt.Errorf("AI manager required to parse non-JSON conversation")
	}

	templateStr, err := bi.promptManager.LoadTemplateFile("import", "parse_conversation")
	if err != nil {
		return nil, fmt.Errorf("failed to load parse_conversation template: %w", err)
	}

	templateData := map[string]any{
		"content": item.Content,
		"source":  item.Source,
		"title":   item.Title,
	}

	prompt, err := bi.promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	ctx := context.Background()
	completion, err := bi.aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   1000000,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	content := cleanAIResponse(completion.Content)

	var messages []memory.Message
	if err := json.Unmarshal([]byte(content), &messages); err != nil {
		return nil, fmt.Errorf("failed to parse AI response as messages: %w (response: %.200s)", err, content)
	}

	return messages, nil
}

func (bi *BaseImporter) analyzeContent(item ImportItem, l2ID string) (*ContentAnalysisResult, error) {
	// Load prompt template from file
	templateStr, err := bi.promptManager.LoadTemplateFile("import", "analyze_content")
	if err != nil {
		return nil, fmt.Errorf("failed to load prompt template: %w", err)
	}

	// Get current L0 facts
	l0Facts, err := bi.l0Manager.GetFactsJSON()
	if err != nil {
		l0Facts = "[]"
	}

	// Get current L1 structure
	l1Structure, err := bi.getL1Structure()
	if err != nil {
		l1Structure = make(map[string]string)
	}

	// Search L1 for related memories based on title keywords
	relatedMemories := bi.findRelatedMemories(item.Title)

	// Render prompt
	templateData := map[string]interface{}{
		"content":          item.Content,
		"source":           item.Source,
		"title":            item.Title,
		"content_date":     formatContentDate(item.ContentDate),
		"l2_id":            l2ID,
		"l0_facts":        l0Facts,
		"l1_structure":    l1Structure,
		"related_memories": relatedMemories,
	}

	prompt, err := bi.promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render prompt: %w", err)
	}

	// Call AI
	ctx := context.Background()
	completion, err := bi.aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   800,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Clean and parse response
	content := cleanAIResponse(completion.Content)

	var analysis ContentAnalysisResult
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w (response: %.200s)", err, content)
	}

	if analysis.Reasoning != "" {
		fmt.Printf("  🤖 AI: %s\n", analysis.Reasoning)
	}

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

// RunConsolidation runs L0 consolidation using the importer's AI and prompt managers.
func (bi *BaseImporter) RunConsolidation() (*ConsolidationResult, error) {
	return ConsolidateL0(bi.l0Manager, bi.promptManager, bi.aiManager)
}

// findRelatedMemories searches L1 for nodes related to the given title.
// Returns a formatted string of related memories, or empty string if none found.
func (bi *BaseImporter) findRelatedMemories(title string) string {
	if title == "" {
		return ""
	}

	// Split title into search keywords (skip short words)
	words := strings.Fields(title)
	seen := make(map[string]bool)
	var matches []string

	for _, word := range words {
		// Skip very short words and common punctuation
		clean := strings.Trim(word, `.,;:!?()[]{}"'` + "\u201c\u201d\u2018\u2019\u300a\u300b")
		if len(clean) < 2 {
			continue
		}

		nodes, err := bi.l1Manager.SearchNodes(clean)
		if err != nil || len(nodes) == 0 {
			continue
		}

		for _, node := range nodes {
			if seen[node.Path] {
				continue
			}
			seen[node.Path] = true
			matches = append(matches, fmt.Sprintf("- %s: %s — %s", node.Path, node.Title, node.Summary))
		}
	}

	if len(matches) == 0 {
		return ""
	}

	// Limit to top 10
	if len(matches) > 10 {
		matches = matches[:10]
	}

	return strings.Join(matches, "\n")
}

// formatContentDate formats a time.Time as "2006-01-02" for prompt templates.
// Returns empty string for zero values.
func formatContentDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// cleanAIResponse strips markdown code block wrappers and whitespace.
func cleanAIResponse(content string) string {
	content = strings.TrimSpace(content)

	// Remove ```json ... ``` wrapper
	if strings.HasPrefix(content, "```") {
		// Find end of first line
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		if strings.HasSuffix(content, "```") {
			content = content[:len(content)-3]
		}
		content = strings.TrimSpace(content)
	}

	return content
}
