package guardrails

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// slackMessage is the payload format for Slack incoming webhooks.
type slackMessage struct {
	Text string `json:"text"`
}

// SendWebhookAlert posts a narrative alert to a Slack webhook URL.
// Runs in its own goroutine so it never blocks the request path.
func SendWebhookAlert(webhookURL string, v *Violation) {
	if webhookURL == "" || v == nil {
		return
	}

	go func() {
		msg := buildNarrative(v)

		payload, err := json.Marshal(slackMessage{Text: msg})
		if err != nil {
			log.Printf("[guardrails] alert marshal error: %v", err)
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Printf("[guardrails] alert send error: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			log.Printf("[guardrails] alert webhook returned %d", resp.StatusCode)
		}
	}()
}

// buildNarrative creates a human-readable incident report from a violation.
func buildNarrative(v *Violation) string {
	var msg string

	msg += "🚨 *AI AGENT GUARDRAIL TRIGGERED*\n\n"
	msg += fmt.Sprintf("*Rule:* %s\n", ruleDisplayName(v.Rule))
	msg += fmt.Sprintf("*Session:* %s\n", v.SessionID)
	msg += fmt.Sprintf("*Time:* %s\n\n", time.Now().UTC().Format(time.RFC3339))

	msg += "*What happened:*\n"
	msg += v.Message + "\n\n"

	if len(v.Details) > 0 {
		msg += "*Details:*\n"
		for k, val := range v.Details {
			msg += fmt.Sprintf("• %s: %v\n", k, val)
		}
		msg += "\n"
	}

	msg += "*Action taken:*\n"
	msg += "✔ Request blocked\n"
	msg += "✔ Session flagged\n\n"

	msg += "*Recommended:* Review the agent's error handling and prompt logic."

	return msg
}

// ruleDisplayName returns a human-friendly name for a rule ID.
func ruleDisplayName(rule string) string {
	switch rule {
	case "token_budget":
		return "Token Budget Exceeded"
	case "prompt_loop":
		return "Prompt Loop Detection"
	case "tool_retry_storm":
		return "Tool Retry Storm"
	case "error_spiral":
		return "Error Retry Spiral"
	default:
		return rule
	}
}
