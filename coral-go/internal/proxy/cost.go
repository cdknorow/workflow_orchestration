package proxy

import (
	"sort"
	"strings"
)

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	InputPerMTok      float64 // $ per 1M input tokens
	OutputPerMTok     float64 // $ per 1M output tokens
	CacheReadPerMTok  float64 // $ per 1M cache-read tokens (Anthropic)
	CacheWritePerMTok float64 // $ per 1M cache-write tokens (Anthropic)
}

// Pricing maps canonical model names to their pricing.
// Use lookupPricing() for matching — it handles aliases and short names.
var Pricing = map[string]ModelPricing{
	// Anthropic
	"claude-opus-4-20250514":   {InputPerMTok: 15.00, OutputPerMTok: 75.00, CacheReadPerMTok: 1.50, CacheWritePerMTok: 18.75},
	"claude-sonnet-4-20250514": {InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"claude-haiku-4-20250514":  {InputPerMTok: 0.80, OutputPerMTok: 4.00, CacheReadPerMTok: 0.08, CacheWritePerMTok: 1.00},

	// Bedrock (on-demand pricing matches direct API; model IDs use anthropic. prefix)
	"anthropic.claude-opus-4-20250514-v1:0":   {InputPerMTok: 15.00, OutputPerMTok: 75.00, CacheReadPerMTok: 1.50, CacheWritePerMTok: 18.75},
	"anthropic.claude-sonnet-4-20250514-v1:0": {InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"anthropic.claude-haiku-4-20250514-v1:0":  {InputPerMTok: 0.80, OutputPerMTok: 4.00, CacheReadPerMTok: 0.08, CacheWritePerMTok: 1.00},
	"us.anthropic.claude-opus-4-20250514-v1:0":   {InputPerMTok: 15.00, OutputPerMTok: 75.00, CacheReadPerMTok: 1.50, CacheWritePerMTok: 18.75},
	"us.anthropic.claude-sonnet-4-20250514-v1:0": {InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"us.anthropic.claude-haiku-4-20250514-v1:0":  {InputPerMTok: 0.80, OutputPerMTok: 4.00, CacheReadPerMTok: 0.08, CacheWritePerMTok: 1.00},

	// OpenAI
	"gpt-4o":      {InputPerMTok: 2.50, OutputPerMTok: 10.00},
	"gpt-4o-mini": {InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"o3":          {InputPerMTok: 2.00, OutputPerMTok: 8.00},

	// Google
	"gemini-2.5-pro":   {InputPerMTok: 1.25, OutputPerMTok: 10.00},
	"gemini-2.5-flash": {InputPerMTok: 0.15, OutputPerMTok: 0.60},
}

// lookupPricing finds pricing for a model. Matching strategy:
//  1. Exact match against pricing table
//  2. Prefix match: model is a prefix of a known key (e.g. "claude-opus-4" matches "claude-opus-4-20250514")
//  3. Longest common prefix: find the pricing key with the longest shared
//     dash-delimited prefix. Handles aliases like "claude-opus-4-6" matching
//     "claude-opus-4-20250514" (both share prefix "claude-opus-4").
func lookupPricing(model string) (ModelPricing, bool) {
	// 1. Exact match
	if p, ok := Pricing[model]; ok {
		return p, true
	}

	// 2. Prefix match: the incoming model is a prefix of a known key.
	for key, p := range Pricing {
		if strings.HasPrefix(key, model) {
			return p, true
		}
	}

	// 3. Longest common dash-delimited prefix.
	// Split both the model and each pricing key on dashes, count how many
	// leading segments match. The key with the most matching segments wins.
	// Requires at least 2 matching segments to avoid false positives.
	modelParts := strings.Split(model, "-")
	var best ModelPricing
	bestMatch := 1 // minimum 2 matching segments required
	for key, p := range Pricing {
		keyParts := strings.Split(key, "-")
		match := commonPrefixLen(modelParts, keyParts)
		if match > bestMatch {
			best = p
			bestMatch = match
		}
	}
	if bestMatch > 1 {
		return best, true
	}
	return ModelPricing{}, false
}

// commonPrefixLen returns the number of matching leading elements between two slices.
func commonPrefixLen(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// TokenUsage holds token counts from a provider response.
type TokenUsage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// CostBreakdown stores the applied pricing and computed dollar breakdown.
type CostBreakdown struct {
	Model             string       `json:"model"`
	PricingFound      bool         `json:"pricing_found"`
	Pricing           ModelPricing `json:"pricing"`
	InputCostUSD      float64      `json:"input_cost_usd"`
	OutputCostUSD     float64      `json:"output_cost_usd"`
	CacheReadCostUSD  float64      `json:"cache_read_cost_usd"`
	CacheWriteCostUSD float64      `json:"cache_write_cost_usd"`
	TotalCostUSD      float64      `json:"total_cost_usd"`
}

// PricingEntry is a serializable pricing table row.
type PricingEntry struct {
	Model string `json:"model"`
	ModelPricing
}

// PricingTable returns the current pricing table in stable model order.
func PricingTable() []PricingEntry {
	keys := make([]string, 0, len(Pricing))
	for model := range Pricing {
		keys = append(keys, model)
	}
	sort.Strings(keys)

	rows := make([]PricingEntry, 0, len(keys))
	for _, model := range keys {
		rows = append(rows, PricingEntry{
			Model:        model,
			ModelPricing: Pricing[model],
		})
	}
	return rows
}

// CalculateCostBreakdown computes the dollar cost breakdown for a request.
func CalculateCostBreakdown(model string, usage TokenUsage) CostBreakdown {
	pricing, ok := lookupPricing(model)
	if !ok {
		return CostBreakdown{Model: model}
	}

	breakdown := CostBreakdown{
		Model:             model,
		PricingFound:      true,
		Pricing:           pricing,
		InputCostUSD:      float64(usage.InputTokens) * pricing.InputPerMTok / 1_000_000,
		OutputCostUSD:     float64(usage.OutputTokens) * pricing.OutputPerMTok / 1_000_000,
		CacheReadCostUSD:  float64(usage.CacheReadTokens) * pricing.CacheReadPerMTok / 1_000_000,
		CacheWriteCostUSD: float64(usage.CacheWriteTokens) * pricing.CacheWritePerMTok / 1_000_000,
	}
	breakdown.TotalCostUSD = breakdown.InputCostUSD + breakdown.OutputCostUSD +
		breakdown.CacheReadCostUSD + breakdown.CacheWriteCostUSD
	return breakdown
}

// CalculateCost computes the dollar cost for a request.
func CalculateCost(model string, usage TokenUsage) float64 {
	return CalculateCostBreakdown(model, usage).TotalCostUSD
}
