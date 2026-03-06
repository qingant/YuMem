package importers

import (
	"context"
	"encoding/json"
	"fmt"
	"yumem/internal/ai"
	"yumem/internal/memory"
	"yumem/internal/prompts"
)

// ConsolidationResult holds before/after stats from a consolidation run.
type ConsolidationResult struct {
	TraitsBefore  int
	TraitsAfter   int
	AgendaBefore  int
	AgendaAfter   int
	ChangesSummary string
}

// consolidationResponse is the AI response format for consolidation.
type consolidationResponse struct {
	Traits  map[string]map[string][]memory.TimestampedValue `json:"traits"`
	Agenda  []memory.AgendaItem                             `json:"agenda"`
	Changes string                                          `json:"changes"`
}

// ConsolidateL0 runs AI-driven consolidation on the current L0 data.
// It deduplicates, narrativizes traits, and caps agenda at 10 items.
func ConsolidateL0(l0Manager *memory.L0Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager) (*ConsolidationResult, error) {
	// 1. Load current L0 state
	l0Data, err := l0Manager.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load L0 data: %w", err)
	}

	traitsJSON, err := json.MarshalIndent(l0Data.Traits, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal traits: %w", err)
	}

	agendaJSON, err := json.MarshalIndent(l0Data.Agenda, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agenda: %w", err)
	}

	// Count before stats
	traitCountBefore := countTraits(l0Data.Traits)
	agendaCountBefore := len(l0Data.Agenda)

	// 2. Load consolidation prompt template
	templateStr, err := promptManager.LoadTemplateFile("l0", "consolidate")
	if err != nil {
		return nil, fmt.Errorf("failed to load consolidation prompt: %w", err)
	}

	// 3. Render prompt
	templateData := map[string]interface{}{
		"traits_json": string(traitsJSON),
		"agenda_json": string(agendaJSON),
	}

	prompt, err := promptManager.RenderTemplate(templateStr, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render prompt: %w", err)
	}

	// 4. Call AI
	ctx := context.Background()
	completion, err := aiManager.Complete(ctx, prompt, ai.CompletionOptions{
		MaxTokens:   4000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// 5. Parse response
	content := cleanAIResponse(completion.Content)

	var resp consolidationResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w (response: %.500s)", err, content)
	}

	// 6. Apply consolidated data
	if resp.Traits != nil {
		if err := l0Manager.ReplaceTraits(resp.Traits); err != nil {
			return nil, fmt.Errorf("failed to replace traits: %w", err)
		}
	}

	if resp.Agenda != nil {
		if err := l0Manager.ReplaceAgenda(resp.Agenda); err != nil {
			return nil, fmt.Errorf("failed to replace agenda: %w", err)
		}
	}

	return &ConsolidationResult{
		TraitsBefore:   traitCountBefore,
		TraitsAfter:    countTraits(resp.Traits),
		AgendaBefore:   agendaCountBefore,
		AgendaAfter:    len(resp.Agenda),
		ChangesSummary: resp.Changes,
	}, nil
}

// countTraits counts total trait key-value pairs across all categories.
func countTraits(traits map[string]map[string][]memory.TimestampedValue) int {
	count := 0
	for _, keys := range traits {
		count += len(keys)
	}
	return count
}
