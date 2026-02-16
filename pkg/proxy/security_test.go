package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/recorder"
	"github.com/nostalgicskinco/air-blackbox-gateway/testdata"
)

// TestSecurity_NoPlaintextInAIRRecords verifies that AIR records contain
// vault references, not raw prompt/response content. This is the fundamental
// security guarantee: traces and logs never contain customer data.
func TestSecurity_NoPlaintextInAIRRecords(t *testing.T) {
	// Use the PII fixture — it contains SSN, email, account numbers.
	fix := testdata.SensitivePayload()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)
	cfg := Config{ProviderURL: upstream.URL, Recorder: rec}
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	runID := w.Header().Get("x-run-id")
	airFile := waitForAIRRecord(t, dir, runID)

	// Read the raw AIR file as bytes — don't just check struct fields,
	// check the ENTIRE file for plaintext leaks.
	data, err := os.ReadFile(airFile)
	if err != nil {
		t.Fatalf("read AIR file: %v", err)
	}
	raw := string(data)

	// These strings appear in the PII fixture and must NOT appear in AIR records.
	sensitiveStrings := []string{
		"123-45-6789",          // SSN
		"john@example.com",     // email
		"ACC-9876543210",       // account number
		"verify my identity",   // prompt content
		"account is active",    // response content
	}

	for _, s := range sensitiveStrings {
		if strings.Contains(raw, s) {
			t.Errorf("AIR record contains plaintext sensitive data: %q", s)
		}
	}
}

// TestSecurity_AllFixturesNoContentLeak runs every golden fixture and
// verifies no request/response body content leaks into AIR records.
func TestSecurity_AllFixturesNoContentLeak(t *testing.T) {
	for _, fix := range testdata.AllFixtures() {
		t.Run(fix.Name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(fix.UpstreamStatus)
				w.Write([]byte(fix.UpstreamResponse))
			}))
			defer upstream.Close()

			dir := t.TempDir()
			rec, _ := recorder.NewWriter(dir)
			cfg := Config{ProviderURL: upstream.URL, Recorder: rec}
			h := Handler(cfg)

			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			runID := w.Header().Get("x-run-id")
			airFile := waitForAIRRecord(t, dir, runID)

			data, err := os.ReadFile(airFile)
			if err != nil {
				t.Fatalf("read AIR file: %v", err)
			}
			raw := string(data)

			// Parse the request to get user message content.
			var parsedReq map[string]interface{}
			json.Unmarshal([]byte(fix.RequestBody), &parsedReq)

			if messages, ok := parsedReq["messages"].([]interface{}); ok {
				for _, msg := range messages {
					if m, ok := msg.(map[string]interface{}); ok {
						if content, ok := m["content"].(string); ok && len(content) > 20 {
							// Only check substantial content (not short things like "test")
							if strings.Contains(raw, content) {
								t.Errorf("AIR record leaks request content: %q", content[:min(50, len(content))])
							}
						}
					}
				}
			}
		})
	}
}

// TestSecurity_AuthHeaderNotStored verifies Authorization headers
// are never persisted in AIR records.
func TestSecurity_AuthHeaderNotStored(t *testing.T) {
	fix := testdata.HappyPath()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)
	cfg := Config{ProviderURL: upstream.URL, Recorder: rec}
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-super-secret-api-key-12345")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	runID := w.Header().Get("x-run-id")
	airFile := waitForAIRRecord(t, dir, runID)
	data, _ := os.ReadFile(airFile)
	raw := string(data)

	if strings.Contains(raw, "sk-super-secret-api-key-12345") {
		t.Error("AIR record contains API key from Authorization header")
	}
	if strings.Contains(raw, "Bearer") {
		t.Error("AIR record contains Bearer token prefix")
	}
}

// TestFailureMode_RecorderDown verifies proxy still works when recorder is nil.
func TestFailureMode_RecorderDown(t *testing.T) {
	fix := testdata.HappyPath()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	cfg := Config{
		ProviderURL: upstream.URL,
		Vault:       nil,
		Recorder:    nil, // recorder down
	}

	h := Handler(cfg)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Proxy should still return the response successfully.
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (proxy should work without recorder)", w.Code)
	}

	runID := w.Header().Get("x-run-id")
	if runID == "" {
		t.Error("should still get run_id even without recorder")
	}

	// Verify response body is correct.
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "chatcmpl-abc123" {
		t.Errorf("response id = %v, want chatcmpl-abc123", resp["id"])
	}
}

// TestFailureMode_ConcurrentRequests verifies the proxy handles
// multiple simultaneous requests without race conditions.
func TestFailureMode_ConcurrentRequests(t *testing.T) {
	fix := testdata.HappyPath()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)
	cfg := Config{ProviderURL: upstream.URL, Recorder: rec}
	h := Handler(cfg)

	done := make(chan string, 10)
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			done <- w.Header().Get("x-run-id")
		}()
	}

	runIDs := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := <-done
		if id == "" {
			t.Error("missing run_id in concurrent request")
		}
		if runIDs[id] {
			t.Errorf("duplicate run_id: %s", id)
		}
		runIDs[id] = true
	}

	// All 10 should have unique IDs.
	if len(runIDs) != 10 {
		t.Errorf("expected 10 unique run IDs, got %d", len(runIDs))
	}

	// Wait for all 10 background goroutines to write AIR records.
	airCount := waitForAIRRecords(t, dir, 10)
	if airCount != 10 {
		t.Errorf("expected 10 AIR records, got %d", airCount)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
