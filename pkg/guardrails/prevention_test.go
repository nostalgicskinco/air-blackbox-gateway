package guardrails

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- PII Tests ---

func TestPIIBlockSSN(t *testing.T) {
	cfg := &Config{
		Prevention: PreventionConfig{
			PII: PIIConfig{
				Enabled:    true,
				BlockSSN:   true,
				RedactMode: "block",
			},
		},
	}

	result := EvaluatePrevention(cfg, []byte(`{"model":"gpt-4","messages":[]}`),
		"My SSN is 123-45-6789", nil, "gpt-4", 0)

	if !result.Blocked {
		t.Fatal("expected request to be blocked for SSN")
	}
}

func TestPIIRedactEmail(t *testing.T) {
	cfg := &Config{
		Prevention: PreventionConfig{
			PII: PIIConfig{
				Enabled:    true,
				BlockEmail: true,
				RedactMode: "redact",
			},
		},
	}

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Contact me at alice@example.com"}]}`
	result := EvaluatePrevention(cfg, []byte(body),
		"Contact me at alice@example.com", nil, "gpt-4", 0)

	if result.Blocked {
		t.Fatal("expected request NOT to be blocked in redact mode")
	}
	if !result.PIIRedacted {
		t.Fatal("expected PIIRedacted=true")
	}
	if result.ModifiedBody == nil {
		t.Fatal("expected modified body")
	}
}

func TestPIIRedactCC(t *testing.T) {
	cfg := PIIConfig{
		Enabled:    true,
		BlockCC:    true,
		RedactMode: "redact",
	}

	blocked, redacted := checkPII(cfg, "My card is 4111-1111-1111-1111 please charge it")
	if blocked {
		t.Fatal("should not block in redact mode")
	}
	if redacted == "My card is 4111-1111-1111-1111 please charge it" {
		t.Fatal("expected CC to be redacted")
	}
}

func TestPIINothingToRedact(t *testing.T) {
	cfg := PIIConfig{
		Enabled:    true,
		BlockSSN:   true,
		BlockCC:    true,
		BlockEmail: true,
		BlockPhone: true,
		RedactMode: "redact",
	}

	blocked, redacted := checkPII(cfg, "This is a normal prompt about coding")
	if blocked {
		t.Fatal("should not block clean text")
	}
	if redacted != "This is a normal prompt about coding" {
		t.Fatalf("expected original text, got %q", redacted)
	}
}

func TestPIIDisabled(t *testing.T) {
	cfg := PIIConfig{Enabled: false, BlockSSN: true}
	blocked, redacted := checkPII(cfg, "My SSN is 123-45-6789")
	if blocked {
		t.Fatal("PII disabled should not block")
	}
	if redacted != "My SSN is 123-45-6789" {
		t.Fatal("PII disabled should not modify text")
	}
}

// --- Tool Filter Tests ---

func TestToolAllowlist(t *testing.T) {
	cfg := ToolFilterConfig{
		Enabled:   true,
		Allowlist: []string{"read_file", "write_file"},
	}

	result := filterTools(cfg, []string{"read_file", "delete_file", "execute_code"})
	if len(result) != 1 || result[0] != "read_file" {
		t.Fatalf("expected [read_file], got %v", result)
	}
}

func TestToolBlocklist(t *testing.T) {
	cfg := ToolFilterConfig{
		Enabled:   true,
		Blocklist: []string{"delete_file", "execute_code"},
	}

	result := filterTools(cfg, []string{"read_file", "delete_file", "write_file"})
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %v", result)
	}
	for _, tool := range result {
		if tool == "delete_file" {
			t.Fatal("delete_file should have been blocked")
		}
	}
}

func TestToolAllToolsBlocked(t *testing.T) {
	cfg := &Config{
		Prevention: PreventionConfig{
			Tools: ToolFilterConfig{
				Enabled:   true,
				Allowlist: []string{"safe_tool"},
			},
		},
	}

	result := EvaluatePrevention(cfg, []byte(`{"model":"gpt-4","messages":[]}`),
		"hello", []string{"dangerous_tool", "risky_tool"}, "gpt-4", 0)

	if !result.Blocked {
		t.Fatal("expected request to be blocked when all tools are filtered")
	}
}

func TestToolFilterDisabled(t *testing.T) {
	cfg := ToolFilterConfig{Enabled: false, Blocklist: []string{"delete_file"}}
	result := filterTools(cfg, []string{"delete_file", "read_file"})
	if len(result) != 2 {
		t.Fatalf("disabled filter should pass all tools, got %v", result)
	}
}

func TestToolFilterEmptyInput(t *testing.T) {
	cfg := ToolFilterConfig{
		Enabled:   true,
		Allowlist: []string{"read_file"},
	}
	result := filterTools(cfg, nil)
	if result != nil {
		t.Fatalf("empty input should return nil, got %v", result)
	}
}

// --- Model Downgrade Tests ---

func TestModelDowngrade(t *testing.T) {
	cfg := ModelLimitConfig{
		Enabled:          true,
		CostThresholdUSD: 1.0,
		CostPerMToken: map[string]float64{
			"gpt-4":         0.03,
			"gpt-3.5-turbo": 0.0005,
		},
		DowngradeMap: map[string]string{
			"gpt-4": "gpt-3.5-turbo",
		},
	}

	// 50M tokens × $0.03/M = $1.50 > threshold
	result := checkModelDowngrade(cfg, "gpt-4", 50_000_000)
	if result != "gpt-3.5-turbo" {
		t.Fatalf("expected downgrade to gpt-3.5-turbo, got %s", result)
	}
}

func TestModelDowngradeNotTriggered(t *testing.T) {
	cfg := ModelLimitConfig{
		Enabled:          true,
		CostThresholdUSD: 10.0,
		CostPerMToken: map[string]float64{
			"gpt-4": 0.03,
		},
		DowngradeMap: map[string]string{
			"gpt-4": "gpt-3.5-turbo",
		},
	}

	// 100K tokens × $0.03/M = $0.003 < threshold
	result := checkModelDowngrade(cfg, "gpt-4", 100_000)
	if result != "gpt-4" {
		t.Fatalf("expected no downgrade, got %s", result)
	}
}

func TestModelDowngradeDisabled(t *testing.T) {
	cfg := ModelLimitConfig{Enabled: false}
	result := checkModelDowngrade(cfg, "gpt-4", 999_999_999)
	if result != "gpt-4" {
		t.Fatalf("disabled should not downgrade, got %s", result)
	}
}

func TestModelDowngradeUnknownModel(t *testing.T) {
	cfg := ModelLimitConfig{
		Enabled:          true,
		CostThresholdUSD: 1.0,
		CostPerMToken:    map[string]float64{"gpt-4": 0.03},
		DowngradeMap:     map[string]string{"gpt-4": "gpt-3.5-turbo"},
	}

	// Unknown model has 0 cost → no downgrade
	result := checkModelDowngrade(cfg, "unknown-model", 999_999_999)
	if result != "unknown-model" {
		t.Fatalf("unknown model should not be downgraded, got %s", result)
	}
}

func TestEstimateSessionCost(t *testing.T) {
	costs := map[string]float64{"gpt-4": 0.03}

	cost := estimateSessionCost(costs, "gpt-4", 1_000_000)
	if cost != 0.03 {
		t.Fatalf("expected $0.03, got $%.4f", cost)
	}

	cost = estimateSessionCost(costs, "gpt-4", 10_000_000)
	if cost != 0.30 {
		t.Fatalf("expected $0.30, got $%.4f", cost)
	}
}

// --- Prevention Integration Tests ---

func TestPreventionModelDowngradeIntegration(t *testing.T) {
	cfg := &Config{
		Prevention: PreventionConfig{
			ModelLimits: ModelLimitConfig{
				Enabled:          true,
				CostThresholdUSD: 1.0,
				CostPerMToken:    map[string]float64{"gpt-4": 0.03},
				DowngradeMap:     map[string]string{"gpt-4": "gpt-3.5-turbo"},
			},
		},
	}

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	result := EvaluatePrevention(cfg, []byte(body), "hello", nil, "gpt-4", 50_000_000)

	if result.Blocked {
		t.Fatal("should not block, should downgrade")
	}
	if result.ModelDowngraded != "gpt-4" {
		t.Fatal("expected ModelDowngraded to be original model")
	}
	if result.ModifiedBody == nil {
		t.Fatal("expected modified body for model downgrade")
	}

	// Verify model was changed in the body.
	var parsed map[string]interface{}
	if err := json.Unmarshal(result.ModifiedBody, &parsed); err != nil {
		t.Fatalf("failed to parse modified body: %v", err)
	}
	if parsed["model"] != "gpt-3.5-turbo" {
		t.Fatalf("expected model gpt-3.5-turbo in body, got %v", parsed["model"])
	}
}

func TestPreventionNilConfig(t *testing.T) {
	result := EvaluatePrevention(nil, []byte(`{}`), "hello", nil, "gpt-4", 0)
	if result.Blocked {
		t.Fatal("nil config should not block")
	}
	if result.ModifiedBody != nil {
		t.Fatal("nil config should not modify body")
	}
}

// --- Approval Tests ---

func TestApprovalWebhookApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ApprovalRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(ApprovalResponse{Approved: true, Reason: "admin approved"})
	}))
	defer srv.Close()

	cfg := ApprovalConfig{
		Enabled:        true,
		WebhookURL:     srv.URL,
		TimeoutSeconds: 5,
		Rules:          []string{"token_budget"},
		FallbackAllow:  false,
	}

	v := &Violation{Rule: "token_budget", SessionID: "test", Message: "budget exceeded"}
	approved, err := RequestApproval(context.Background(), cfg, v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatal("expected approval")
	}
}

func TestApprovalWebhookDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ApprovalResponse{Approved: false, Reason: "too risky"})
	}))
	defer srv.Close()

	cfg := ApprovalConfig{
		Enabled:        true,
		WebhookURL:     srv.URL,
		TimeoutSeconds: 5,
		Rules:          []string{"token_budget"},
		FallbackAllow:  true,
	}

	v := &Violation{Rule: "token_budget", SessionID: "test", Message: "budget exceeded"}
	approved, _ := RequestApproval(context.Background(), cfg, v)
	if approved {
		t.Fatal("expected denial")
	}
}

func TestApprovalTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // will timeout
	}))
	defer srv.Close()

	cfg := ApprovalConfig{
		Enabled:        true,
		WebhookURL:     srv.URL,
		TimeoutSeconds: 1, // 1 second timeout
		Rules:          []string{"token_budget"},
		FallbackAllow:  true, // allow on timeout
	}

	v := &Violation{Rule: "token_budget", SessionID: "test", Message: "budget exceeded"}
	approved, _ := RequestApproval(context.Background(), cfg, v)
	if !approved {
		t.Fatal("expected fallback_allow=true on timeout")
	}
}

func TestApprovalDisabled(t *testing.T) {
	cfg := ApprovalConfig{Enabled: false}
	v := &Violation{Rule: "token_budget", SessionID: "test", Message: "test"}
	approved, _ := RequestApproval(context.Background(), cfg, v)
	if !approved {
		t.Fatal("disabled approval should allow")
	}
}

func TestApprovalRuleNotInList(t *testing.T) {
	cfg := ApprovalConfig{
		Enabled:    true,
		WebhookURL: "http://localhost:9999", // won't be called
		Rules:      []string{"token_budget"},
	}

	v := &Violation{Rule: "error_spiral", SessionID: "test", Message: "test"}
	approved, _ := RequestApproval(context.Background(), cfg, v)
	if approved {
		t.Fatal("rule not in approval list should return false (use default block)")
	}
}
