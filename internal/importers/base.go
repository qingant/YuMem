package importers

import (
	"context"
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
	L0Updates      int      `json:"l0_updates"`
	L1Created      int      `json:"l1_created"`
	L2Created      int      `json:"l2_created"`
	Errors         []string `json:"errors"`
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
	L0Updates map[string]map[string]string `json:"l0_updates"` // category → key → value
	L0Agenda  []AgendaUpdate              `json:"l0_agenda"`
	L1Node    *L1NodeResult               `json:"l1_node"`    // null if not worth indexing
	Reasoning string                      `json:"reasoning"`
}

type AgendaUpdate struct {
	Item     string `json:"item"`
	Priority string `json:"priority"`
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

// ProcessItem handles the full import pipeline for a single item:
// 1. Store raw content in L2 (get ID)
// 2. AI analysis (one call → L0 updates + L1 node)
// 3. Apply L0 updates and create L1 node
func (bi *BaseImporter) ProcessItem(item ImportItem, result *ImportResult) error {
	log := logging.Get()
	log.Info("import", fmt.Sprintf("processing item: %s (source=%s)", item.Title, item.Source))

	// Step 1: Store in L2 first
	l2Tags := []string{"imported", item.Source}
	l2Entry, err := bi.l2Manager.AddEntry(item.Title, item.Content, "imported_content", item.Source, l2Tags)
	if err != nil {
		log.Error("import", fmt.Sprintf("L2 store failed for %s: %v", item.Title, err))
		return fmt.Errorf("failed to store L2 entry: %w", err)
	}
	result.L2Created++
	fmt.Printf("  📄 L2 stored: %s\n", l2Entry.ID)
	log.Debug("import", fmt.Sprintf("L2 stored: %s", l2Entry.ID))

	// Step 2+3: Run analysis and apply L0/L1 updates
	return bi.AnalyzeAndApply(l2Entry.ID, item.Title, item.Content, item.Source, item.ContentDate, result)
}

// AnalyzeAndApply runs AI analysis on already-stored L2 content and applies L0/L1 updates.
// contentDate is the original creation date of the content (for ObservedAt). Zero value means use time.Now().
// This is used by ProcessItem (after L2 creation) and by store_memory (on existing L2 entries).
func (bi *BaseImporter) AnalyzeAndApply(l2ID, title, content, source string, contentDate time.Time, result *ImportResult) error {
	log := logging.Get()
	log.Info("import", fmt.Sprintf("analyzing content: %s (l2=%s)", title, l2ID))

	item := ImportItem{
		Title:   title,
		Content: content,
		Source:  source,
	}

	analysis, err := bi.analyzeContent(item, l2ID)
	if err != nil {
		log.Warn("import", fmt.Sprintf("AI analysis failed for %s: %v", title, err))
		fmt.Printf("  ⚠️  AI analysis failed: %v (L2 content preserved)\n", err)
		return nil // Analysis failure is non-fatal
	}

	// Apply L0 updates
	observedAt := time.Now()
	if !contentDate.IsZero() {
		observedAt = contentDate
	}
	if len(analysis.L0Updates) > 0 {
		l0Count := 0
		for category, kvs := range analysis.L0Updates {
			for key, value := range kvs {
				err := bi.l0Manager.MergeTraits(category, key, memory.TimestampedValue{
					Value:      value,
					ObservedAt: observedAt.Format("2006-01-02"),
					Source:     l2ID,
				})
				if err != nil {
					fmt.Printf("  ⚠️  L0 update failed (%s/%s): %v\n", category, key, err)
				} else {
					l0Count++
				}
			}
		}
		if l0Count > 0 {
			if result != nil {
				result.L0Updates += l0Count
			}
			fmt.Printf("  🧠 L0 updated: %d traits\n", l0Count)
		}
	}

	// Apply agenda updates
	for _, agendaItem := range analysis.L0Agenda {
		err := bi.l0Manager.AddAgenda(memory.AgendaItem{
			Item:        agendaItem.Item,
			Priority:    agendaItem.Priority,
			Since:       observedAt.Format("2006-01-02"),
			LastUpdated: observedAt.Format("2006-01-02"),
			Source:      l2ID,
		})
		if err != nil {
			fmt.Printf("  ⚠️  Agenda update failed: %v\n", err)
		}
	}

	// Create L1 node
	if analysis.L1Node != nil && analysis.L1Node.Path != "" {
		_, err := bi.l1Manager.CreateNode(
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
			fmt.Printf("  📂 L1 created: %s\n", analysis.L1Node.Path)
		}
	}

	// Auto-consolidate if L0 is oversize (at most once per 10 items)
	bi.itemsSinceConsolidate++
	if bi.itemsSinceConsolidate >= 10 && bi.l0Manager.IsOversize() {
		fmt.Printf("  🔄 L0 oversize, running auto-consolidation...\n")
		log.Info("import", "L0 oversize detected, running auto-consolidation")
		if cr, err := bi.RunConsolidation(); err != nil {
			log.Warn("import", fmt.Sprintf("auto-consolidation failed: %v", err))
			fmt.Printf("  ⚠️  Auto-consolidation failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ Consolidated: traits %d→%d, agenda %d→%d\n",
				cr.TraitsBefore, cr.TraitsAfter, cr.AgendaBefore, cr.AgendaAfter)
		}
		bi.itemsSinceConsolidate = 0
	}

	return nil
}

func (bi *BaseImporter) analyzeContent(item ImportItem, l2ID string) (*ContentAnalysisResult, error) {
	// Load prompt template from file
	templateStr, err := bi.promptManager.LoadTemplateFile("import", "analyze_content")
	if err != nil {
		return nil, fmt.Errorf("failed to load prompt template: %w", err)
	}

	// Get current L0 state
	l0Current, err := bi.l0Manager.GetTraitsJSON()
	if err != nil {
		l0Current = "{}"
	}

	// Get current agenda
	l0Agenda, err := bi.l0Manager.GetAgendaJSON()
	if err != nil {
		l0Agenda = "[]"
	}

	// Get current L1 structure
	l1Structure, err := bi.getL1Structure()
	if err != nil {
		l1Structure = make(map[string]string)
	}

	// Render prompt
	templateData := map[string]interface{}{
		"content":      item.Content,
		"source":       item.Source,
		"l2_id":        l2ID,
		"l0_current":   l0Current,
		"l0_agenda":    l0Agenda,
		"l1_structure": l1Structure,
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
