package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// ApprovalRequest is the payload sent to the approval webhook.
type ApprovalRequest struct {
	SessionID   string                 `json:"session_id"`
	ViolationID string                 `json:"violation_id"`
	Rule        string                 `json:"rule"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	Timestamp   time.Time              `json:"timestamp"`
}

// ApprovalResponse is expected back from the approval webhook.
type ApprovalResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// RequestApproval sends a violation to the approval webhook and waits for a decision.
// Returns (approved, error).
//
// If approval is disabled, returns (true, nil) — everything is allowed.
// If the webhook is unreachable or times out, returns (cfg.FallbackAllow, nil).
// Only rules listed in cfg.Rules trigger the approval flow.
func RequestApproval(ctx context.Context, cfg ApprovalConfig, v *Violation) (bool, error) {
	if !cfg.Enabled || cfg.WebhookURL == "" {
		return true, nil // approval disabled, fall through
	}

	// Check if this rule requires approval.
	if len(cfg.Rules) > 0 {
		needsApproval := false
		for _, rule := range cfg.Rules {
			if rule == v.Rule {
				needsApproval = true
				break
			}
		}
		if !needsApproval {
			return false, nil // rule not in approval list, use default block
		}
	}

	req := ApprovalRequest{
		SessionID:   v.SessionID,
		ViolationID: fmt.Sprintf("%s-%d", v.Rule, time.Now().UnixNano()),
		Rule:        v.Rule,
		Message:     v.Message,
		Details:     v.Details,
		Timestamp:   time.Now().UTC(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		log.Printf("[approval] marshal error: %v", err)
		return cfg.FallbackAllow, err
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(timeoutCtx, "POST", cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[approval] request creation error: %v", err)
		return cfg.FallbackAllow, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[approval] webhook error: %v (fallback: %v)", err, cfg.FallbackAllow)
		return cfg.FallbackAllow, nil
	}
	defer resp.Body.Close()

	var approval ApprovalResponse
	if err := json.NewDecoder(resp.Body).Decode(&approval); err != nil {
		log.Printf("[approval] decode error: %v (fallback: %v)", err, cfg.FallbackAllow)
		return cfg.FallbackAllow, nil
	}

	if approval.Approved {
		log.Printf("[approval] APPROVED: session=%s rule=%s reason=%s", v.SessionID, v.Rule, approval.Reason)
	} else {
		log.Printf("[approval] DENIED: session=%s rule=%s reason=%s", v.SessionID, v.Rule, approval.Reason)
	}

	return approval.Approved, nil
}
