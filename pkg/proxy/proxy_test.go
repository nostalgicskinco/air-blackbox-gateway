package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/recorder"
)

func TestHealthEndpoint(t *testing.T) {
	cfg := Config{ProviderURL: "http://example.com"}
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("health status = %d, want 200", w.Code)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Fatalf("health body = %v", body)
	}
}

func TestProxyAddsRunID(t *testing.T) {
	// Mock upstream provider.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Echo back a minimal response.
		resp := map[string]interface{}{
			"id":    "chatcmpl-test",
			"model": "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "Hello!"}},
			},
			"usage": map[string]int{
				"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
			},
		}
		_ = body
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)

	cfg := Config{
		ProviderURL: upstream.URL,
		Recorder:    rec,
	}

	h := Handler(cfg)
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("proxy status = %d, want 200", w.Code)
	}

	runID := w.Header().Get("x-run-id")
	if runID == "" {
		t.Fatal("missing x-run-id header")
	}

	// Verify AIR record was written.
	airFile := filepath.Join(dir, runID+".air.json")
	if _, err := os.Stat(airFile); err != nil {
		t.Fatalf("AIR record not written: %v", err)
	}

	loaded, err := recorder.Load(airFile)
	if err != nil {
		t.Fatalf("load AIR: %v", err)
	}
	if loaded.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", loaded.Model)
	}
	if loaded.Status != "success" {
		t.Errorf("status = %q, want success", loaded.Status)
	}
	if loaded.Tokens.Total != 15 {
		t.Errorf("tokens = %d, want 15", loaded.Tokens.Total)
	}
}

func TestProxyUpstreamError(t *testing.T) {
	// Upstream returns 500.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)
	cfg := Config{ProviderURL: upstream.URL, Recorder: rec}
	h := Handler(cfg)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500 passthrough, got %d", w.Code)
	}

	runID := w.Header().Get("x-run-id")
	if runID == "" {
		t.Fatal("missing x-run-id even on error")
	}

	// AIR record should have error status.
	loaded, _ := recorder.Load(filepath.Join(dir, runID+".air.json"))
	if loaded.Status != "error" {
		t.Errorf("status = %q, want error", loaded.Status)
	}
}

func TestInferProvider(t *testing.T) {
	tests := []struct {
		model    string
		url      string
		expected string
	}{
		{"gpt-4o-mini", "", "openai"},
		{"claude-3-sonnet", "", "anthropic"},
		{"gemini-pro", "", "google"},
		{"mistral-7b", "", "mistral"},
		{"llama-3.1-70b", "", "meta"},
		{"custom-model", "https://api.openai.com", "openai"},
		{"custom-model", "https://unknown.example.com", "unknown"},
	}

	for _, tt := range tests {
		got := inferProvider(tt.model, tt.url)
		if got != tt.expected {
			t.Errorf("inferProvider(%q, %q) = %q, want %q", tt.model, tt.url, got, tt.expected)
		}
	}
}
