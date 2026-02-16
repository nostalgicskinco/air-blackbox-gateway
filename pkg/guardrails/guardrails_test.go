package guardrails

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenBudgetExceeded(t *testing.T) {
	cfg := &Config{
		Budgets: BudgetConfig{MaxSessionTokens: 1000},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-token-budget"

	mgr.GetOrCreate(sid)
	mgr.RecordResponse(sid, 1200, false)

	v := Evaluate(cfg, mgr, sid, &EvalRequest{PromptText: "hello"})
	if v == nil {
		t.Fatal("expected violation, got nil")
	}
	if v.Rule != "token_budget" {
		t.Fatalf("expected rule token_budget, got %s", v.Rule)
	}
}

func TestTokenBudgetNotExceeded(t *testing.T) {
	cfg := &Config{
		Budgets: BudgetConfig{MaxSessionTokens: 5000},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-token-ok"

	mgr.GetOrCreate(sid)
	mgr.RecordResponse(sid, 500, false)

	v := Evaluate(cfg, mgr, sid, &EvalRequest{PromptText: "hello"})
	if v != nil {
		t.Fatalf("expected no violation, got %+v", v)
	}
}

func TestPromptLoopDetected(t *testing.T) {
	cfg := &Config{
		LoopDetection: LoopConfig{
			SimilarPromptThreshold: 0.80,
			MaxSimilarPrompts:      3,
			WindowSeconds:          60,
		},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-prompt-loop"

	mgr.GetOrCreate(sid)

	// Record 3 nearly identical prompts.
	for i := 0; i < 3; i++ {
		mgr.RecordRequest(sid, "please help me fix the authentication error in my code", nil)
	}

	// The 4th similar prompt should trigger the loop detector.
	v := Evaluate(cfg, mgr, sid, &EvalRequest{
		PromptText: "please help me fix the authentication error in my code",
	})
	if v == nil {
		t.Fatal("expected violation, got nil")
	}
	if v.Rule != "prompt_loop" {
		t.Fatalf("expected rule prompt_loop, got %s", v.Rule)
	}
}

func TestPromptLoopNotTriggeredWithDifferentPrompts(t *testing.T) {
	cfg := &Config{
		LoopDetection: LoopConfig{
			SimilarPromptThreshold: 0.80,
			MaxSimilarPrompts:      3,
			WindowSeconds:          60,
		},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-no-loop"

	mgr.GetOrCreate(sid)

	mgr.RecordRequest(sid, "help me with authentication", nil)
	mgr.RecordRequest(sid, "now work on the database layer", nil)
	mgr.RecordRequest(sid, "create a new REST endpoint for users", nil)

	v := Evaluate(cfg, mgr, sid, &EvalRequest{
		PromptText: "write unit tests for the handler",
	})
	if v != nil {
		t.Fatalf("expected no violation, got %+v", v)
	}
}

func TestToolRetryStorm(t *testing.T) {
	cfg := &Config{
		ToolProtection: ToolConfig{
			MaxRepeatCalls:      3,
			RepeatWindowSeconds: 30,
		},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-tool-storm"

	mgr.GetOrCreate(sid)

	// Record 3 calls to the same tool.
	for i := 0; i < 3; i++ {
		mgr.RecordRequest(sid, "", []string{"code_interpreter"})
	}

	// Next request with the same tool should trigger.
	v := Evaluate(cfg, mgr, sid, &EvalRequest{
		ToolNames: []string{"code_interpreter"},
	})
	if v == nil {
		t.Fatal("expected violation, got nil")
	}
	if v.Rule != "tool_retry_storm" {
		t.Fatalf("expected rule tool_retry_storm, got %s", v.Rule)
	}
}

func TestToolRetryStormDifferentTools(t *testing.T) {
	cfg := &Config{
		ToolProtection: ToolConfig{
			MaxRepeatCalls:      3,
			RepeatWindowSeconds: 30,
		},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-tool-ok"

	mgr.GetOrCreate(sid)

	mgr.RecordRequest(sid, "", []string{"code_interpreter"})
	mgr.RecordRequest(sid, "", []string{"web_search"})
	mgr.RecordRequest(sid, "", []string{"file_reader"})

	v := Evaluate(cfg, mgr, sid, &EvalRequest{
		ToolNames: []string{"code_interpreter"},
	})
	if v != nil {
		t.Fatalf("expected no violation, got %+v", v)
	}
}

func TestErrorSpiral(t *testing.T) {
	cfg := &Config{
		RetryProtection: RetryConfig{MaxConsecutiveErrors: 3},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-error-spiral"

	mgr.GetOrCreate(sid)

	// Record 3 consecutive errors.
	for i := 0; i < 3; i++ {
		mgr.RecordResponse(sid, 100, true)
	}

	v := Evaluate(cfg, mgr, sid, &EvalRequest{PromptText: "retry"})
	if v == nil {
		t.Fatal("expected violation, got nil")
	}
	if v.Rule != "error_spiral" {
		t.Fatalf("expected rule error_spiral, got %s", v.Rule)
	}
}

func TestErrorSpiralResetsOnSuccess(t *testing.T) {
	cfg := &Config{
		RetryProtection: RetryConfig{MaxConsecutiveErrors: 3},
	}
	mgr := NewManager(5 * time.Minute)
	sid := "test-error-reset"

	mgr.GetOrCreate(sid)

	mgr.RecordResponse(sid, 100, true)
	mgr.RecordResponse(sid, 100, true)
	mgr.RecordResponse(sid, 200, false) // success resets counter
	mgr.RecordResponse(sid, 100, true)

	v := Evaluate(cfg, mgr, sid, &EvalRequest{PromptText: "hello"})
	if v != nil {
		t.Fatalf("expected no violation after reset, got %+v", v)
	}
}

func TestNoConfigPassthrough(t *testing.T) {
	mgr := NewManager(5 * time.Minute)
	sid := "test-nil-config"
	mgr.GetOrCreate(sid)

	// Nil config should never trigger a violation.
	v := Evaluate(nil, mgr, sid, &EvalRequest{PromptText: "hello"})
	if v != nil {
		t.Fatalf("nil config should return nil violation, got %+v", v)
	}
}

func TestWebhookAlert(t *testing.T) {
	received := make(chan string, 1)

	// Start a mock Slack webhook server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg slackMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("failed to decode webhook payload: %v", err)
			return
		}
		received <- msg.Text
		w.WriteHeader(200)
	}))
	defer srv.Close()

	v := &Violation{
		Rule:      "token_budget",
		Message:   "Session halted: token budget exceeded (90000 / 80000 tokens).",
		SessionID: "test-webhook",
		Details: map[string]interface{}{
			"total_tokens": 90000,
			"max_tokens":   80000,
		},
	}

	SendWebhookAlert(srv.URL, v)

	select {
	case msg := <-received:
		if msg == "" {
			t.Fatal("received empty webhook message")
		}
		if !contains(msg, "GUARDRAIL TRIGGERED") {
			t.Errorf("webhook message missing expected text, got: %s", msg)
		}
		if !contains(msg, "Token Budget Exceeded") {
			t.Errorf("webhook message missing rule name, got: %s", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for webhook alert")
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		a, b     string
		minScore float64
		maxScore float64
	}{
		{"hello world", "hello world", 1.0, 1.0},
		{"the quick brown fox", "the slow red fox", 0.25, 0.5},
		{"completely different", "nothing alike here", 0.0, 0.01},
		{"", "", 1.0, 1.0},
	}

	for _, tt := range tests {
		score := jaccardSimilarity(tt.a, tt.b)
		if score < tt.minScore || score > tt.maxScore {
			t.Errorf("jaccardSimilarity(%q, %q) = %.3f, want [%.2f, %.2f]",
				tt.a, tt.b, score, tt.minScore, tt.maxScore)
		}
	}
}

func TestSessionCleanup(t *testing.T) {
	// Use a very short TTL to test cleanup.
	mgr := &Manager{
		sessions: make(map[string]*SessionState),
		ttl:      50 * time.Millisecond,
	}

	s := mgr.GetOrCreate("ephemeral")
	s.LastActive = time.Now().Add(-1 * time.Second) // already expired

	mgr.mu.Lock()
	now := time.Now()
	for id, sess := range mgr.sessions {
		if now.Sub(sess.LastActive) > mgr.ttl {
			delete(mgr.sessions, id)
		}
	}
	mgr.mu.Unlock()

	mgr.mu.Lock()
	_, exists := mgr.sessions["ephemeral"]
	mgr.mu.Unlock()

	if exists {
		t.Fatal("expected expired session to be cleaned up")
	}
}

func TestConfigLoad(t *testing.T) {
	// Test nil path returns nil config.
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	if cfg != nil {
		t.Fatal("empty path should return nil config")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
