package guardrails

import (
	"sync"
	"testing"
)

// --- Analytics Tests ---

func TestRecordCallAggregates(t *testing.T) {
	pt := NewPerformanceTracker()
	pt.RecordCall("gpt-4", 1000, 10, 5, 15, "success", "")
	pt.RecordCall("gpt-4", 1200, 20, 10, 30, "success", "")
	pt.RecordCall("gpt-4", 800, 5, 3, 8, "error", "server_error")

	stats := pt.GetModelStats("gpt-4")
	if stats == nil {
		t.Fatal("expected stats for gpt-4")
	}
	if stats.RequestCount != 3 {
		t.Fatalf("expected 3 requests, got %d", stats.RequestCount)
	}
	if stats.SuccessCount != 2 {
		t.Fatalf("expected 2 successes, got %d", stats.SuccessCount)
	}
	if stats.ErrorCount != 1 {
		t.Fatalf("expected 1 error, got %d", stats.ErrorCount)
	}
	if stats.TotalTokens != 53 {
		t.Fatalf("expected 53 total tokens, got %d", stats.TotalTokens)
	}
	if stats.TokensPrompt != 35 {
		t.Fatalf("expected 35 prompt tokens, got %d", stats.TokensPrompt)
	}
	if stats.TokensCompletion != 18 {
		t.Fatalf("expected 18 completion tokens, got %d", stats.TokensCompletion)
	}
	if stats.ErrorsByType["server_error"] != 1 {
		t.Fatalf("expected 1 server_error, got %d", stats.ErrorsByType["server_error"])
	}
}

func TestErrorRate(t *testing.T) {
	pt := NewPerformanceTracker()
	for i := 0; i < 8; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "success", "")
	}
	for i := 0; i < 2; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "error", "rate_limit")
	}

	rate := pt.ErrorRate("gpt-4")
	if rate != 0.2 {
		t.Fatalf("expected 0.2 error rate, got %f", rate)
	}
}

func TestErrorRateNoData(t *testing.T) {
	pt := NewPerformanceTracker()
	rate := pt.ErrorRate("nonexistent")
	if rate != 0 {
		t.Fatalf("expected 0 for unknown model, got %f", rate)
	}
}

func TestLatencyPercentiles(t *testing.T) {
	pt := NewPerformanceTracker()
	// Add 100 calls with latencies 1-100.
	for i := int64(1); i <= 100; i++ {
		pt.RecordCall("gpt-4", i, 0, 0, 0, "success", "")
	}

	stats := pt.GetModelStats("gpt-4")
	latency := stats.ComputeLatency()

	if latency.AvgMS != 50 {
		t.Fatalf("expected avg 50, got %d", latency.AvgMS)
	}
	if latency.P50MS != 51 {
		t.Fatalf("expected p50=51, got %d", latency.P50MS)
	}
	if latency.P95MS != 96 {
		t.Fatalf("expected p95=96, got %d", latency.P95MS)
	}
	if latency.P99MS != 100 {
		t.Fatalf("expected p99=100, got %d", latency.P99MS)
	}
}

func TestLatencyP95Method(t *testing.T) {
	pt := NewPerformanceTracker()
	for i := int64(1); i <= 20; i++ {
		pt.RecordCall("gpt-4o", i*100, 0, 0, 0, "success", "")
	}

	p95 := pt.LatencyP95("gpt-4o")
	if p95 == 0 {
		t.Fatal("expected nonzero p95")
	}
}

func TestGetAllStats(t *testing.T) {
	pt := NewPerformanceTracker()
	pt.RecordCall("gpt-4", 100, 0, 0, 0, "success", "")
	pt.RecordCall("gpt-4o", 200, 0, 0, 0, "success", "")
	pt.RecordCall("claude-3", 150, 0, 0, 0, "success", "")

	all := pt.GetAllStats()
	if len(all) != 3 {
		t.Fatalf("expected 3 models, got %d", len(all))
	}
}

func TestConcurrentRecording(t *testing.T) {
	pt := NewPerformanceTracker()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			model := "gpt-4"
			if n%2 == 0 {
				model = "gpt-4o"
			}
			pt.RecordCall(model, int64(n*10), 5, 3, 8, "success", "")
		}(i)
	}
	wg.Wait()

	all := pt.GetAllStats()
	if len(all) != 2 {
		t.Fatalf("expected 2 models, got %d", len(all))
	}

	var total int64
	for _, s := range all {
		total += s.RequestCount
	}
	if total != 100 {
		t.Fatalf("expected 100 total requests, got %d", total)
	}
}

func TestGetModelStatsNil(t *testing.T) {
	pt := NewPerformanceTracker()
	stats := pt.GetModelStats("nonexistent")
	if stats != nil {
		t.Fatal("expected nil for unknown model")
	}
}

// --- Failure Classifier Tests ---

func TestClassifyRateLimit(t *testing.T) {
	result := ClassifyFailure(429, "rate limit exceeded")
	if result != FailureRateLimit {
		t.Fatalf("expected %s, got %s", FailureRateLimit, result)
	}
}

func TestClassifyContextLength(t *testing.T) {
	result := ClassifyFailure(400, `{"error":{"message":"This model's maximum context length is 8192 tokens"}}`)
	if result != FailureContextLength {
		t.Fatalf("expected %s, got %s", FailureContextLength, result)
	}
}

func TestClassifyContextLengthMaxTokens(t *testing.T) {
	result := ClassifyFailure(400, `{"error":{"code":"max_tokens","message":"max_tokens exceeded"}}`)
	if result != FailureContextLength {
		t.Fatalf("expected %s, got %s", FailureContextLength, result)
	}
}

func TestClassifyAuthError(t *testing.T) {
	result := ClassifyFailure(401, "Unauthorized")
	if result != FailureAuthError {
		t.Fatalf("expected %s, got %s", FailureAuthError, result)
	}
}

func TestClassifyAuthErrorForbidden(t *testing.T) {
	result := ClassifyFailure(403, "Permission denied")
	if result != FailureAuthError {
		t.Fatalf("expected %s, got %s", FailureAuthError, result)
	}
}

func TestClassifyServerError(t *testing.T) {
	for _, code := range []int{500, 502, 503} {
		result := ClassifyFailure(code, "Internal server error")
		if result != FailureServerError {
			t.Fatalf("status %d: expected %s, got %s", code, FailureServerError, result)
		}
	}
}

func TestClassifyTimeout(t *testing.T) {
	result := ClassifyFailure(504, "Gateway timeout")
	if result != FailureTimeout {
		t.Fatalf("expected %s, got %s", FailureTimeout, result)
	}
}

func TestClassifyTimeoutFromBody(t *testing.T) {
	result := ClassifyFailure(400, `{"error":"deadline exceeded"}`)
	if result != FailureTimeout {
		t.Fatalf("expected %s, got %s", FailureTimeout, result)
	}
}

func TestClassifyContentFilter(t *testing.T) {
	result := ClassifyFailure(400, `{"error":{"code":"content_policy_violation","message":"content filtered"}}`)
	if result != FailureContentFilter {
		t.Fatalf("expected %s, got %s", FailureContentFilter, result)
	}
}

func TestClassifyInvalidRequest(t *testing.T) {
	result := ClassifyFailure(400, `{"error":{"message":"invalid model name"}}`)
	if result != FailureInvalidReq {
		t.Fatalf("expected %s, got %s", FailureInvalidReq, result)
	}
}

func TestClassifyUnknown(t *testing.T) {
	// Use a non-4xx, non-5xx status to get "unknown".
	result := ClassifyFailure(600, "something weird")
	if result != FailureUnknown {
		t.Fatalf("expected %s, got %s", FailureUnknown, result)
	}
}

func TestClassifyOther4xx(t *testing.T) {
	// 418 is a 4xx, so it should be classified as invalid_request.
	result := ClassifyFailure(418, "I'm a teapot")
	if result != FailureInvalidReq {
		t.Fatalf("expected %s, got %s", FailureInvalidReq, result)
	}
}

// --- Router Tests ---

func TestRouteErrorRateHigh(t *testing.T) {
	pt := NewPerformanceTracker()
	// 5 errors out of 10 = 50% error rate.
	for i := 0; i < 5; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "success", "")
	}
	for i := 0; i < 5; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "error", "server_error")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: true,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o", Condition: "error_rate", Threshold: 0.2, Enabled: true},
			},
		},
	}

	decision := EvaluateRouting(cfg, pt, "gpt-4")
	if decision.RoutedModel != "gpt-4o" {
		t.Fatalf("expected routing to gpt-4o, got %s", decision.RoutedModel)
	}
	if decision.Rule != "error_rate" {
		t.Fatalf("expected rule error_rate, got %s", decision.Rule)
	}
	if decision.Reason == "" {
		t.Fatal("expected a reason string")
	}
}

func TestRouteLatencyHigh(t *testing.T) {
	pt := NewPerformanceTracker()
	// All calls take 5000ms. P95 will be ~5000.
	for i := 0; i < 20; i++ {
		pt.RecordCall("gpt-4", 5000, 0, 0, 0, "success", "")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: true,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o-mini", Condition: "latency_p95", Threshold: 3000, Enabled: true},
			},
		},
	}

	decision := EvaluateRouting(cfg, pt, "gpt-4")
	if decision.RoutedModel != "gpt-4o-mini" {
		t.Fatalf("expected routing to gpt-4o-mini, got %s", decision.RoutedModel)
	}
}

func TestRouteNoMatch(t *testing.T) {
	pt := NewPerformanceTracker()
	// Low error rate, fast latency.
	for i := 0; i < 20; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "success", "")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: true,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o", Condition: "error_rate", Threshold: 0.2, Enabled: true},
			},
		},
	}

	decision := EvaluateRouting(cfg, pt, "gpt-4")
	if decision.RoutedModel != "gpt-4" {
		t.Fatalf("expected no routing, got %s", decision.RoutedModel)
	}
	if decision.Rule != "" {
		t.Fatalf("expected empty rule, got %s", decision.Rule)
	}
}

func TestRouteDisabled(t *testing.T) {
	pt := NewPerformanceTracker()
	for i := 0; i < 10; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "error", "server_error")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: false,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o", Condition: "error_rate", Threshold: 0.2, Enabled: true},
			},
		},
	}

	decision := EvaluateRouting(cfg, pt, "gpt-4")
	if decision.RoutedModel != "gpt-4" {
		t.Fatalf("disabled router should not route, got %s", decision.RoutedModel)
	}
}

func TestRouteRulePriority(t *testing.T) {
	pt := NewPerformanceTracker()
	for i := 0; i < 10; i++ {
		pt.RecordCall("gpt-4", 5000, 0, 0, 0, "error", "server_error")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: true,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o", Condition: "error_rate", Threshold: 0.2, Enabled: true},
				{FromModel: "gpt-4", ToModel: "gpt-3.5-turbo", Condition: "latency_p95", Threshold: 3000, Enabled: true},
			},
		},
	}

	// Both rules match, but first should win.
	decision := EvaluateRouting(cfg, pt, "gpt-4")
	if decision.RoutedModel != "gpt-4o" {
		t.Fatalf("expected first rule to win (gpt-4o), got %s", decision.RoutedModel)
	}
}

func TestRouteWrongModel(t *testing.T) {
	pt := NewPerformanceTracker()
	for i := 0; i < 10; i++ {
		pt.RecordCall("gpt-4", 100, 0, 0, 0, "error", "server_error")
	}

	cfg := OptimizationConfig{
		Router: RouterConfig{
			Enabled: true,
			Rules: []RoutingRule{
				{FromModel: "gpt-4", ToModel: "gpt-4o", Condition: "error_rate", Threshold: 0.2, Enabled: true},
			},
		},
	}

	// Request for claude-3 should NOT be routed by a gpt-4 rule.
	decision := EvaluateRouting(cfg, pt, "claude-3")
	if decision.RoutedModel != "claude-3" {
		t.Fatalf("expected no routing for claude-3, got %s", decision.RoutedModel)
	}
}

func TestRouteNilTracker(t *testing.T) {
	cfg := OptimizationConfig{
		Router: RouterConfig{Enabled: true},
	}
	decision := EvaluateRouting(cfg, nil, "gpt-4")
	if decision.RoutedModel != "gpt-4" {
		t.Fatalf("nil tracker should not route, got %s", decision.RoutedModel)
	}
}
