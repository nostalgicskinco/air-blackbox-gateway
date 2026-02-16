package guardrails

import (
	"fmt"
	"time"
)

// EvalRequest contains the parsed request data needed for detection.
type EvalRequest struct {
	PromptText string   // the last user message content
	ToolNames  []string // tool_choice or function names in the request
	Model      string
}

// Violation describes a guardrail that was triggered.
type Violation struct {
	Rule      string                 `json:"rule"`
	Message   string                 `json:"message"`
	SessionID string                 `json:"session_id"`
	Details   map[string]interface{} `json:"details"`
}

// Evaluate runs all detection rules against the current session state.
// Returns a Violation if any rule triggers, or nil if everything is normal.
// The session must already exist in the manager (call GetOrCreate first).
func Evaluate(cfg *Config, mgr *Manager, sessionID string, req *EvalRequest) *Violation {
	if cfg == nil {
		return nil
	}

	mgr.mu.Lock()
	s, ok := mgr.sessions[sessionID]
	if !ok {
		mgr.mu.Unlock()
		return nil
	}

	// Copy the values we need while holding the lock, then release
	totalTokens := s.TotalTokens
	consecutiveErrors := s.ConsecutiveErrors
	promptHistory := make([]promptEntry, len(s.PromptHistory))
	copy(promptHistory, s.PromptHistory)
	toolCalls := make(map[string][]time.Time)
	for k, v := range s.ToolCalls {
		cp := make([]time.Time, len(v))
		copy(cp, v)
		toolCalls[k] = cp
	}
	mgr.mu.Unlock()

	// Rule 1: Token budget
	if v := checkTokenBudget(cfg, sessionID, totalTokens); v != nil {
		return v
	}

	// Rule 2: Prompt loop
	if v := checkPromptLoop(cfg, sessionID, promptHistory, req.PromptText); v != nil {
		return v
	}

	// Rule 3: Tool retry storm
	if v := checkToolRetryStorm(cfg, sessionID, toolCalls, req.ToolNames); v != nil {
		return v
	}

	// Rule 4: Error retry spiral
	if v := checkErrorSpiral(cfg, sessionID, consecutiveErrors); v != nil {
		return v
	}

	return nil
}

// checkTokenBudget triggers if a session exceeds its token limit.
func checkTokenBudget(cfg *Config, sessionID string, totalTokens int) *Violation {
	max := cfg.Budgets.MaxSessionTokens
	if max <= 0 {
		return nil
	}

	if totalTokens >= max {
		return &Violation{
			Rule:      "token_budget",
			Message:   fmt.Sprintf("Session halted: token budget exceeded (%d / %d tokens).", totalTokens, max),
			SessionID: sessionID,
			Details: map[string]interface{}{
				"total_tokens": totalTokens,
				"max_tokens":   max,
			},
		}
	}
	return nil
}

// checkPromptLoop triggers if the last N prompts are too similar.
func checkPromptLoop(cfg *Config, sessionID string, history []promptEntry, currentPrompt string) *Violation {
	threshold := cfg.LoopDetection.SimilarPromptThreshold
	maxSimilar := cfg.LoopDetection.MaxSimilarPrompts
	windowSec := cfg.LoopDetection.WindowSeconds

	if threshold <= 0 || maxSimilar <= 0 || currentPrompt == "" {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(windowSec) * time.Second)
	matches := 0
	var highestScore float64

	for _, entry := range history {
		if entry.Timestamp.Before(cutoff) {
			continue
		}
		score := jaccardSimilarity(currentPrompt, entry.Text)
		if score >= threshold {
			matches++
		}
		if score > highestScore {
			highestScore = score
		}
	}

	if matches >= maxSimilar {
		return &Violation{
			Rule:      "prompt_loop",
			Message:   fmt.Sprintf("Session halted: agent appears stuck in a recursive loop. Last %d prompts were >%.0f%% identical.", matches, threshold*100),
			SessionID: sessionID,
			Details: map[string]interface{}{
				"similar_prompts":  matches,
				"similarity_score": highestScore,
				"threshold":        threshold,
			},
		}
	}
	return nil
}

// checkToolRetryStorm triggers if the same tool is called too many times
// within a short window.
func checkToolRetryStorm(cfg *Config, sessionID string, toolCalls map[string][]time.Time, currentTools []string) *Violation {
	maxCalls := cfg.ToolProtection.MaxRepeatCalls
	windowSec := cfg.ToolProtection.RepeatWindowSeconds

	if maxCalls <= 0 || windowSec <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(windowSec) * time.Second)

	for _, tool := range currentTools {
		timestamps, ok := toolCalls[tool]
		if !ok {
			continue
		}

		recentCount := 0
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				recentCount++
			}
		}

		if recentCount >= maxCalls {
			return &Violation{
				Rule:      "tool_retry_storm",
				Message:   fmt.Sprintf("Session halted: tool '%s' called %d times in %d seconds.", tool, recentCount, windowSec),
				SessionID: sessionID,
				Details: map[string]interface{}{
					"tool_name":     tool,
					"call_count":    recentCount,
					"window_seconds": windowSec,
				},
			}
		}
	}
	return nil
}

// checkErrorSpiral triggers after too many consecutive upstream errors.
func checkErrorSpiral(cfg *Config, sessionID string, consecutiveErrors int) *Violation {
	maxErrors := cfg.RetryProtection.MaxConsecutiveErrors
	if maxErrors <= 0 {
		return nil
	}

	if consecutiveErrors >= maxErrors {
		return &Violation{
			Rule:      "error_spiral",
			Message:   fmt.Sprintf("Session halted: %d consecutive errors detected. Agent may be stuck in a retry loop.", consecutiveErrors),
			SessionID: sessionID,
			Details: map[string]interface{}{
				"consecutive_errors": consecutiveErrors,
				"max_errors":         maxErrors,
			},
		}
	}
	return nil
}
