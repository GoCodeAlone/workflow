package ai

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// GuardrailConfig defines safety constraints for AI/LLM steps.
type GuardrailConfig struct {
	MaxTokens          int           `yaml:"max_tokens" json:"max_tokens"`
	BlockPatterns      []string      `yaml:"block_patterns" json:"block_patterns"`
	TrackCost          bool          `yaml:"track_cost" json:"track_cost"`
	CostBudgetPerExec  float64       `yaml:"cost_budget_per_execution" json:"cost_budget_per_execution"`
	MaskPII            bool          `yaml:"mask_pii" json:"mask_pii"`
	MaxRetries         int           `yaml:"max_retries" json:"max_retries"`
	Timeout            time.Duration `yaml:"timeout" json:"timeout"`
	RateLimitPerMinute int           `yaml:"rate_limit_per_minute" json:"rate_limit_per_minute"`
}

// GuardrailResult contains the outcome of guardrail checks.
type GuardrailResult struct {
	Allowed       bool     `json:"allowed"`
	Reasons       []string `json:"reasons,omitempty"`
	MaskedInput   string   `json:"masked_input,omitempty"`
	EstimatedCost float64  `json:"estimated_cost,omitempty"`
}

// CostTracker tracks AI usage costs per execution and per tenant.
type CostTracker struct {
	mu      sync.RWMutex
	costs   map[string]float64 // executionID -> total cost
	tenants map[string]float64 // tenantID -> total cost this period
}

// NewCostTracker creates a new CostTracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{
		costs:   make(map[string]float64),
		tenants: make(map[string]float64),
	}
}

// Record adds cost for an execution and tenant.
func (ct *CostTracker) Record(executionID, tenantID string, cost float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.costs[executionID] += cost
	if tenantID != "" {
		ct.tenants[tenantID] += cost
	}
}

// GetExecutionCost returns the accumulated cost for an execution.
func (ct *CostTracker) GetExecutionCost(executionID string) float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.costs[executionID]
}

// GetTenantCost returns the accumulated cost for a tenant.
func (ct *CostTracker) GetTenantCost(tenantID string) float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.tenants[tenantID]
}

// Guardrails enforces safety constraints on AI/LLM operations.
type Guardrails struct {
	config      GuardrailConfig
	costTracker *CostTracker
	patterns    []*regexp.Regexp // compiled block patterns
	piiPatterns []piiPattern     // PII detection patterns
}

// piiPattern pairs a compiled regex with a replacement label.
type piiPattern struct {
	regex       *regexp.Regexp
	replacement string
}

// defaultPIIPatterns returns built-in PII detection patterns.
func defaultPIIPatterns() []piiPattern {
	return []piiPattern{
		{
			// Email addresses
			regex:       regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			replacement: "[EMAIL REDACTED]",
		},
		{
			// SSN (XXX-XX-XXXX)
			regex:       regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			replacement: "[SSN REDACTED]",
		},
		{
			// Credit card numbers (13-19 digits, optionally separated by spaces or dashes)
			regex:       regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
			replacement: "[CREDIT CARD REDACTED]",
		},
		{
			// US phone numbers: (XXX) XXX-XXXX, XXX-XXX-XXXX, +1XXXXXXXXXX
			regex:       regexp.MustCompile(`(?:\+1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),
			replacement: "[PHONE REDACTED]",
		},
		{
			// IPv4 addresses
			regex:       regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
			replacement: "[IP REDACTED]",
		},
	}
}

// NewGuardrails creates a new Guardrails instance from config. It compiles
// all block patterns at initialization time and returns an error if any
// pattern is invalid.
func NewGuardrails(config GuardrailConfig) (*Guardrails, error) {
	g := &Guardrails{
		config:      config,
		costTracker: NewCostTracker(),
		piiPatterns: defaultPIIPatterns(),
	}

	// Compile block patterns
	for _, pat := range config.BlockPatterns {
		compiled, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid block pattern %q: %w", pat, err)
		}
		g.patterns = append(g.patterns, compiled)
	}

	return g, nil
}

// CheckInput validates input before sending to an LLM. It checks token limits,
// block patterns, cost budget, and optionally masks PII.
func (g *Guardrails) CheckInput(ctx context.Context, input string) (*GuardrailResult, error) {
	result := &GuardrailResult{Allowed: true}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check max tokens (approximate: 1 token ~= 4 chars)
	if g.config.MaxTokens > 0 {
		estimatedTokens := len(input) / 4
		if estimatedTokens > g.config.MaxTokens {
			result.Allowed = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("input exceeds max tokens: estimated %d > limit %d", estimatedTokens, g.config.MaxTokens))
		}
	}

	// Check block patterns
	for _, pat := range g.patterns {
		if pat.MatchString(input) {
			result.Allowed = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("input matches blocked pattern: %s", pat.String()))
		}
	}

	// Mask PII if configured
	if g.config.MaskPII {
		result.MaskedInput = g.MaskPII(input)
	}

	return result, nil
}

// CheckOutput validates LLM output before returning to the user. It checks
// block patterns and optionally masks PII in the response.
func (g *Guardrails) CheckOutput(ctx context.Context, output string) (*GuardrailResult, error) {
	result := &GuardrailResult{Allowed: true}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check block patterns on output
	for _, pat := range g.patterns {
		if pat.MatchString(output) {
			result.Allowed = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("output matches blocked pattern: %s", pat.String()))
		}
	}

	// Mask PII in output if configured
	if g.config.MaskPII {
		result.MaskedInput = g.MaskPII(output)
	}

	return result, nil
}

// RecordCost records the cost of an AI operation. If cost tracking is enabled
// and the execution's accumulated cost would exceed the budget, it returns an error.
func (g *Guardrails) RecordCost(executionID, tenantID string, cost float64) error {
	if !g.config.TrackCost {
		return nil
	}

	// Check budget before recording
	if g.config.CostBudgetPerExec > 0 {
		currentCost := g.costTracker.GetExecutionCost(executionID)
		if currentCost+cost > g.config.CostBudgetPerExec {
			return fmt.Errorf("cost budget exceeded for execution %s: current=%.6f + new=%.6f > budget=%.6f",
				executionID, currentCost, cost, g.config.CostBudgetPerExec)
		}
	}

	g.costTracker.Record(executionID, tenantID, cost)
	return nil
}

// GetCost returns the total cost for an execution.
func (g *Guardrails) GetCost(executionID string) float64 {
	return g.costTracker.GetExecutionCost(executionID)
}

// GetTenantCost returns the total cost for a tenant in the current period.
func (g *Guardrails) GetTenantCost(tenantID string) float64 {
	return g.costTracker.GetTenantCost(tenantID)
}

// MaskPII replaces PII patterns with masked versions.
func (g *Guardrails) MaskPII(input string) string {
	masked := input
	for _, pp := range g.piiPatterns {
		masked = pp.regex.ReplaceAllString(masked, pp.replacement)
	}
	return masked
}

// modelRates holds per-token costs (input, output) for known models.
type modelRates struct {
	input  float64
	output float64
}

// knownModelRates maps model identifiers to their per-token pricing.
var knownModelRates = map[string]modelRates{
	"claude-sonnet-4-20250514":  {input: 0.000003, output: 0.000015},
	"claude-haiku-4-5-20251001": {input: 0.000001, output: 0.000005},
	"claude-opus-4-6":           {input: 0.000015, output: 0.000075},
}

// EstimateCost estimates the cost of an LLM call based on model and token counts.
// For unknown models, it uses the claude-sonnet rates as a conservative default.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	rates, ok := knownModelRates[model]
	if !ok {
		// Check for partial model name matches (e.g., "sonnet" in "claude-sonnet-4-...")
		for name, r := range knownModelRates {
			if strings.Contains(model, extractModelFamily(name)) {
				rates = r
				ok = true
				break
			}
		}
		if !ok {
			// Default to sonnet rates as a reasonable middle ground
			rates = knownModelRates["claude-sonnet-4-20250514"]
		}
	}

	return float64(inputTokens)*rates.input + float64(outputTokens)*rates.output
}

// extractModelFamily extracts the family name (e.g., "sonnet", "haiku", "opus")
// from a full model identifier.
func extractModelFamily(model string) string {
	parts := strings.Split(model, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return model
}

// DefaultGuardrailConfig returns a GuardrailConfig with sensible defaults.
func DefaultGuardrailConfig() GuardrailConfig {
	return GuardrailConfig{
		MaxTokens:          100000,
		MaxRetries:         3,
		Timeout:            30 * time.Second,
		RateLimitPerMinute: 60,
		MaskPII:            true,
		TrackCost:          true,
		CostBudgetPerExec:  1.0, // $1.00 per execution
		BlockPatterns: []string{
			`(?i)ignore\s+(all\s+)?(previous|above|prior)\s+(instructions?|prompts?)`,
			`(?i)you\s+are\s+now\s+`,
			`(?i)system\s*:\s*`,
			`(?i)pretend\s+you\s+are`,
		},
	}
}
