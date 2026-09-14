package agent

import (
	"fmt"
	"time"
)

// StopCondition is a predicate the agent checks after each turn to decide
// whether to stop the loop early.
type StopCondition func(result string, usage UsageSnapshot, iteration int) bool

// Guardrails configures cost and time limits for autonomous runs.
type Guardrails struct {
	MaxTokens     int           // total token ceiling (0 = unlimited)
	MaxCostUSD    float64       // cost ceiling in USD (0 = unlimited)
	MaxDuration   time.Duration // wall-clock ceiling (0 = unlimited)
	StopCondition StopCondition // custom stop predicate
}

// WithGuardrails wraps the agent with cost/time guardrails. When the guardrails
// are exceeded, the agent returns partial progress instead of continuing.
func (a *Agent) WithGuardrails(g Guardrails) *Agent {
	return &Agent{
		llm:         a.llm,
		reg:         a.reg,
		maxIter:     a.maxIter,
		approve:     a.approve,
		onEvent:     a.onEvent,
		msgs:        a.msgs,
		usage:       a.usage,
		caps:        a.caps,
		budget:      a.budget,
		checkpoints: a.checkpoints,
		guardrails:  g,
		startTime:   time.Now(),
	}
}

// guardrails tracks running totals for cost/time limits.
type guardrailsState struct {
	startTime time.Time
}

// checkGuardrails returns true if any guardrail is exceeded.
func (g Guardrails) check(usage UsageSnapshot, dur time.Duration) (string, bool) {
	if g.MaxTokens > 0 && usage.TotalTokens > g.MaxTokens {
		return fmt.Sprintf("token ceiling reached (%d/%d)", usage.TotalTokens, g.MaxTokens), true
	}
	if g.MaxCostUSD > 0 && usage.CostUSD > g.MaxCostUSD {
		return fmt.Sprintf("cost ceiling reached (~$%.2f/$%.2f)", usage.CostUSD, g.MaxCostUSD), true
	}
	if g.MaxDuration > 0 && dur > g.MaxDuration {
		return fmt.Sprintf("duration ceiling reached (%s/%s)", dur.Round(time.Second), g.MaxDuration.Round(time.Second)), true
	}
	if g.StopCondition != nil && g.StopCondition("", usage, 0) {
		return "custom stop condition met", true
	}
	return "", false
}
