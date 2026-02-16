package guardrails

// checkModelDowngrade checks whether the current model should be swapped to a
// cheaper alternative based on the estimated session cost so far.
//
// Returns the model to use. If no downgrade is needed, returns the original model.
func checkModelDowngrade(cfg ModelLimitConfig, model string, sessionTokens int) string {
	if !cfg.Enabled || cfg.CostThresholdUSD == 0 {
		return model
	}

	cost := estimateSessionCost(cfg.CostPerMToken, model, sessionTokens)
	if cost >= cfg.CostThresholdUSD {
		if downgrade, ok := cfg.DowngradeMap[model]; ok {
			return downgrade
		}
	}

	return model
}

// estimateSessionCost calculates the approximate cost of a session.
// cost = (tokens / 1,000,000) × cost_per_million_tokens
func estimateSessionCost(costMap map[string]float64, model string, tokens int) float64 {
	costPerMToken, ok := costMap[model]
	if !ok {
		return 0 // unknown model, can't estimate — don't downgrade
	}
	return float64(tokens) / 1_000_000.0 * costPerMToken
}
