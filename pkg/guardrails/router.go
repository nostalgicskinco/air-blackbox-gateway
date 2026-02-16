package guardrails

import (
	"fmt"
)

// RoutingRule defines a condition under which a model should be swapped.
type RoutingRule struct {
	FromModel string  `yaml:"from_model"`
	ToModel   string  `yaml:"to_model"`
	Condition string  `yaml:"condition"` // "error_rate" or "latency_p95"
	Threshold float64 `yaml:"threshold"` // e.g. 0.2 for 20% error rate, 10000 for 10s p95
	Enabled   bool    `yaml:"enabled"`
}

// RoutingDecision describes the result of model routing evaluation.
type RoutingDecision struct {
	OriginalModel string
	RoutedModel   string
	Rule          string // which rule matched (empty if no routing)
	Reason        string // human-readable explanation
}

// EvaluateRouting checks if the requested model should be swapped based on
// analytics data and configured routing rules. Returns a decision with the
// (possibly different) model to use.
//
// Rules are evaluated in order; the first match wins. If no rule matches
// or routing is disabled, returns the original model unchanged.
func EvaluateRouting(cfg OptimizationConfig, tracker *PerformanceTracker, model string) *RoutingDecision {
	decision := &RoutingDecision{
		OriginalModel: model,
		RoutedModel:   model,
	}

	if !cfg.Router.Enabled || tracker == nil {
		return decision
	}

	for _, rule := range cfg.Router.Rules {
		if !rule.Enabled || rule.FromModel != model {
			continue
		}

		matched := false
		var reason string

		switch rule.Condition {
		case "error_rate":
			rate := tracker.ErrorRate(model)
			if rate > rule.Threshold {
				matched = true
				reason = fmt.Sprintf("error_rate=%.1f%% exceeds threshold %.1f%%",
					rate*100, rule.Threshold*100)
			}
		case "latency_p95":
			p95 := tracker.LatencyP95(model)
			if p95 > int64(rule.Threshold) {
				matched = true
				reason = fmt.Sprintf("latency_p95=%dms exceeds threshold %.0fms",
					p95, rule.Threshold)
			}
		}

		if matched {
			decision.RoutedModel = rule.ToModel
			decision.Rule = rule.Condition
			decision.Reason = reason
			return decision
		}
	}

	return decision
}
