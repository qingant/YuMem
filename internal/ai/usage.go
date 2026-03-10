package ai

import (
	"strings"
	"sync"
	"time"
)

// UsageRecord represents a single AI API call's usage data.
type UsageRecord struct {
	Timestamp        time.Time `json:"timestamp"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Purpose          string    `json:"purpose"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	EstimatedCost    float64   `json:"estimated_cost"`
}

// UsageSummary provides aggregated usage statistics.
type UsageSummary struct {
	TotalCalls       int                      `json:"total_calls"`
	TotalTokens      int                      `json:"total_tokens"`
	TotalCost        float64                  `json:"total_cost"`
	ByProvider       map[string]*ProviderUsage `json:"by_provider"`
	ByPurpose        map[string]*PurposeUsage  `json:"by_purpose"`
	Recent           []UsageRecord            `json:"recent"`
}

// ProviderUsage holds per-provider usage stats.
type ProviderUsage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// PurposeUsage holds per-purpose usage stats.
type PurposeUsage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// UsageTracker tracks AI API usage in memory.
type UsageTracker struct {
	mu      sync.RWMutex
	records []UsageRecord
}

// NewUsageTracker creates a new UsageTracker.
func NewUsageTracker() *UsageTracker {
	return &UsageTracker{
		records: make([]UsageRecord, 0),
	}
}

// Track records a new usage entry.
func (ut *UsageTracker) Track(record UsageRecord) {
	ut.mu.Lock()
	defer ut.mu.Unlock()
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	if record.EstimatedCost == 0 {
		record.EstimatedCost = EstimateCost(record.Model, record.PromptTokens, record.CompletionTokens)
	}
	ut.records = append(ut.records, record)
}

// GetSummary returns aggregated usage statistics.
func (ut *UsageTracker) GetSummary(recentCount int) *UsageSummary {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	summary := &UsageSummary{
		ByProvider: make(map[string]*ProviderUsage),
		ByPurpose:  make(map[string]*PurposeUsage),
	}

	for _, r := range ut.records {
		summary.TotalCalls++
		summary.TotalTokens += r.TotalTokens
		summary.TotalCost += r.EstimatedCost

		// By provider
		if _, ok := summary.ByProvider[r.Provider]; !ok {
			summary.ByProvider[r.Provider] = &ProviderUsage{}
		}
		p := summary.ByProvider[r.Provider]
		p.Calls++
		p.PromptTokens += r.PromptTokens
		p.CompletionTokens += r.CompletionTokens
		p.TotalTokens += r.TotalTokens
		p.EstimatedCost += r.EstimatedCost

		// By purpose
		purpose := r.Purpose
		if purpose == "" {
			purpose = "other"
		}
		if _, ok := summary.ByPurpose[purpose]; !ok {
			summary.ByPurpose[purpose] = &PurposeUsage{}
		}
		pu := summary.ByPurpose[purpose]
		pu.Calls++
		pu.PromptTokens += r.PromptTokens
		pu.CompletionTokens += r.CompletionTokens
		pu.TotalTokens += r.TotalTokens
		pu.EstimatedCost += r.EstimatedCost
	}

	// Recent records (last N)
	if recentCount <= 0 {
		recentCount = 20
	}
	start := len(ut.records) - recentCount
	if start < 0 {
		start = 0
	}
	summary.Recent = make([]UsageRecord, len(ut.records[start:]))
	copy(summary.Recent, ut.records[start:])
	// Reverse so most recent is first
	for i, j := 0, len(summary.Recent)-1; i < j; i, j = i+1, j-1 {
		summary.Recent[i], summary.Recent[j] = summary.Recent[j], summary.Recent[i]
	}

	return summary
}

// GetRecent returns the last N records.
func (ut *UsageTracker) GetRecent(n int) []UsageRecord {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	if n <= 0 {
		n = 20
	}
	start := len(ut.records) - n
	if start < 0 {
		start = 0
	}
	result := make([]UsageRecord, len(ut.records[start:]))
	copy(result, ut.records[start:])
	return result
}

// Reset clears all usage records.
func (ut *UsageTracker) Reset() {
	ut.mu.Lock()
	defer ut.mu.Unlock()
	ut.records = make([]UsageRecord, 0)
}

// modelPricing holds per-1M-token pricing (input, output).
type modelPricing struct {
	inputPer1M  float64
	outputPer1M float64
}

// Pricing data sourced from official API pricing pages:
// - Gemini: https://ai.google.dev/gemini-api/docs/pricing
// - OpenAI: https://openai.com/api/pricing/
// - Claude: https://platform.claude.com/docs/en/about-claude/pricing
// Last updated: 2026-03-10
var pricingTable = map[string]modelPricing{
	// Gemini
	"gemini-2.0-flash":              {0.10, 0.40},
	"gemini-2.5-flash":              {0.30, 2.50},
	"gemini-2.5-flash-preview":      {0.30, 2.50},
	"gemini-2.5-pro":                {1.25, 10.00},
	"gemini-2.5-pro-preview":        {1.25, 10.00},
	"gemini-3.1-flash-lite-preview": {0.25, 1.50},
	// OpenAI
	"gpt-4o":              {2.50, 10.00},
	"gpt-4o-mini":         {0.15, 0.60},
	"gpt-4-turbo-preview": {10.00, 30.00},
	// Claude 4.5/4.6 series
	"claude-opus-4-6":              {5.00, 25.00},
	"claude-sonnet-4-6-20260204":   {3.00, 15.00},
	"claude-sonnet-4-5-20250929":   {3.00, 15.00},
	"claude-sonnet-4-20250514":     {3.00, 15.00},
	"claude-haiku-4-5-20251001":    {1.00, 5.00},
	// Claude legacy
	"claude-3-5-sonnet-20241022": {3.00, 15.00},
	"claude-3-haiku-20240307":    {0.25, 1.25},
}

// EstimateCost calculates the estimated cost for a given model and token counts.
func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	pricing, ok := pricingTable[model]
	if !ok {
		// Try partial match: find the longest key that is a prefix of the model
		// or the longest model prefix that matches a key.
		// This handles cases like "gemini-2.5-flash-preview-05-20" matching "gemini-2.5-flash-preview".
		bestLen := 0
		for key, p := range pricingTable {
			if strings.HasPrefix(model, key) && len(key) > bestLen {
				pricing = p
				ok = true
				bestLen = len(key)
			} else if strings.HasPrefix(key, model) && len(model) > bestLen {
				pricing = p
				ok = true
				bestLen = len(model)
			}
		}
	}
	if !ok {
		return 0
	}

	inputCost := float64(promptTokens) / 1_000_000 * pricing.inputPer1M
	outputCost := float64(completionTokens) / 1_000_000 * pricing.outputPer1M
	return inputCost + outputCost
}
