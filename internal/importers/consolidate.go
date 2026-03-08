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
	FactsBefore    int
	FactsAfter     int
	ChangesSummary string
}

// consolidationResponse is the AI response format for consolidation.
type consolidationResponse struct {
	Facts   []memory.Fact `json:"facts"`
	Changes string        `json:"changes"`
}

// ConsolidateL0 runs AI-driven consolidation on the current L0 data.
// It deduplicates facts, marks expired ones, removes sensitive data, and rewrites judgmental language.
func ConsolidateL0(l0Manager *memory.L0Manager, promptManager *prompts.PromptManager, aiManager *ai.Manager) (*ConsolidationResult, error) {
	// 1. Load current L0 state
	l0Data, err := l0Manager.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load L0 data: %w", err)
	}

	factsJSON, err := json.MarshalIndent(l0Data.Facts, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal facts: %w", err)
	}

	factsCountBefore := len(l0Data.Facts)

	// 2. Load consolidation prompt template
	templateStr, err := promptManager.LoadTemplateFile("l0", "consolidate")
	if err != nil {
		return nil, fmt.Errorf("failed to load consolidation prompt: %w", err)
	}

	// 3. Render prompt
	templateData := map[string]interface{}{
		"facts_json": string(factsJSON),
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
	if resp.Facts != nil {
		if err := l0Manager.ReplaceFacts(resp.Facts); err != nil {
			return nil, fmt.Errorf("failed to replace facts: %w", err)
		}
	}

	return &ConsolidationResult{
		FactsBefore:    factsCountBefore,
		FactsAfter:     len(resp.Facts),
		ChangesSummary: resp.Changes,
	}, nil
}
