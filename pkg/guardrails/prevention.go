package guardrails

import (
	"encoding/json"
	"log"
)

// PreventionResult describes what happened when prevention policies evaluated a request.
type PreventionResult struct {
	// Blocked is true if the request should be rejected entirely.
	Blocked     bool
	BlockReason string

	// ModifiedBody is the modified JSON request body to send upstream.
	// nil means use the original body unchanged.
	ModifiedBody []byte

	// Tracking fields for logging / alerting.
	ModelDowngraded string // original model if downgraded, empty if not
	PIIRedacted     bool
	ToolsFiltered   bool
}

// EvaluatePrevention runs all prevention policies against the request.
// Policies run in order: PII → Tools → Model Downgrade.
// If any policy blocks, we return immediately. Otherwise, modifications accumulate.
//
// Returns a result with Blocked=false and ModifiedBody=nil if no prevention config exists.
func EvaluatePrevention(cfg *Config, reqBody []byte, promptText string, toolNames []string, model string, sessionTokens int) *PreventionResult {
	result := &PreventionResult{}

	if cfg == nil {
		return result
	}
	prev := cfg.Prevention

	// Track whether we need to rewrite the request body.
	needsRewrite := false
	newPrompt := promptText
	newTools := toolNames
	newModel := model

	// --- Rule 1: PII blocking/redaction ---
	if prev.PII.Enabled {
		blocked, redacted := checkPII(prev.PII, promptText)
		if blocked {
			result.Blocked = true
			result.BlockReason = "PII detected in request (policy: block)"
			return result
		}
		if redacted != promptText {
			newPrompt = redacted
			result.PIIRedacted = true
			needsRewrite = true
			log.Printf("[prevention] PII redacted from prompt")
		}
	}

	// --- Rule 2: Tool filtering ---
	if prev.Tools.Enabled && len(toolNames) > 0 {
		filtered := filterTools(prev.Tools, toolNames)
		if len(filtered) == 0 && len(toolNames) > 0 {
			result.Blocked = true
			result.BlockReason = "all requested tools are blocked by policy"
			return result
		}
		if len(filtered) != len(toolNames) {
			newTools = filtered
			result.ToolsFiltered = true
			needsRewrite = true
			log.Printf("[prevention] tools filtered: %d → %d", len(toolNames), len(filtered))
		}
	}

	// --- Rule 3: Model downgrade ---
	if prev.ModelLimits.Enabled {
		downgraded := checkModelDowngrade(prev.ModelLimits, model, sessionTokens)
		if downgraded != model {
			result.ModelDowngraded = model
			newModel = downgraded
			needsRewrite = true
			log.Printf("[prevention] model downgrade: %s → %s", model, downgraded)
		}
	}

	// If any modifications were made, rewrite the request body.
	if needsRewrite {
		modified, err := modifyRequestBody(reqBody, newPrompt, newTools, newModel, promptText)
		if err != nil {
			log.Printf("[prevention] failed to modify request body: %v", err)
			// Fall through with original body rather than blocking.
			return result
		}
		result.ModifiedBody = modified
	}

	return result
}

// modifyRequestBody applies prevention modifications to the raw JSON request.
// It updates messages (for PII redaction), tools (for filtering), and model (for downgrade).
func modifyRequestBody(body []byte, newPrompt string, newTools []string, newModel string, originalPrompt string) ([]byte, error) {
	// Parse the body into a generic map to preserve all fields.
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	// Update model if changed.
	if newModel != "" {
		modelJSON, _ := json.Marshal(newModel)
		req["model"] = modelJSON
	}

	// Update messages if prompt was redacted (PII).
	if newPrompt != originalPrompt {
		if messagesRaw, ok := req["messages"]; ok {
			modified := redactMessagesJSON(messagesRaw, originalPrompt, newPrompt)
			req["messages"] = modified
		}
	}

	// Update tools if filtered.
	if newTools != nil {
		if toolsRaw, ok := req["tools"]; ok {
			filtered := filterToolsJSON(toolsRaw, newTools)
			req["tools"] = filtered
		}
	}

	return json.Marshal(req)
}

// redactMessagesJSON replaces occurrences of originalText with redactedText
// in the messages array.
func redactMessagesJSON(messagesRaw json.RawMessage, original, redacted string) json.RawMessage {
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(messagesRaw, &messages); err != nil {
		return messagesRaw // can't parse, return original
	}

	for i, msg := range messages {
		roleRaw, ok := msg["role"]
		if !ok {
			continue
		}
		var role string
		if err := json.Unmarshal(roleRaw, &role); err != nil || role != "user" {
			continue
		}

		contentRaw, ok := msg["content"]
		if !ok {
			continue
		}

		// Try string content.
		var text string
		if err := json.Unmarshal(contentRaw, &text); err == nil {
			if text == original {
				newContent, _ := json.Marshal(redacted)
				messages[i]["content"] = newContent
			}
			continue
		}

		// Try array-of-parts content.
		var parts []map[string]json.RawMessage
		if err := json.Unmarshal(contentRaw, &parts); err == nil {
			for j, part := range parts {
				typeRaw, ok := part["type"]
				if !ok {
					continue
				}
				var partType string
				if err := json.Unmarshal(typeRaw, &partType); err != nil || partType != "text" {
					continue
				}
				textRaw, ok := part["text"]
				if !ok {
					continue
				}
				var partText string
				if err := json.Unmarshal(textRaw, &partText); err == nil && partText == original {
					newText, _ := json.Marshal(redacted)
					parts[j]["text"] = newText
				}
			}
			newParts, _ := json.Marshal(parts)
			messages[i]["content"] = newParts
		}
	}

	result, _ := json.Marshal(messages)
	return result
}

// filterToolsJSON keeps only tools whose function.name is in the allowed list.
func filterToolsJSON(toolsRaw json.RawMessage, allowed []string) json.RawMessage {
	allowSet := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		allowSet[t] = true
	}

	var tools []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return toolsRaw
	}

	var filtered []json.RawMessage
	for _, toolRaw := range tools {
		var tool struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(toolRaw, &tool); err != nil {
			continue
		}
		if allowSet[tool.Function.Name] {
			filtered = append(filtered, toolRaw)
		}
	}

	result, _ := json.Marshal(filtered)
	return result
}
