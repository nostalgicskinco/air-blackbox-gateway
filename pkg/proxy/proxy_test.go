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
		// OpenAI model families
		{"gpt-4o-mini", "", "openai"},
		{"gpt-4o", "", "openai"},
		{"o1-preview", "", "openai"},
		{"o3-mini", "", "openai"},
		{"chatgpt-4o-latest", "", "openai"},
		{"dall-e-3", "", "openai"},
		// Anthropic
		{"claude-3-sonnet", "", "anthropic"},
		{"claude-3-5-sonnet-20241022", "", "anthropic"},
		{"claude-opus-4-20250514", "", "anthropic"},
		// Google
		{"gemini-pro", "", "google"},
		{"gemini-2.0-flash", "", "google"},
		// Mistral family
		{"mistral-7b", "", "mistral"},
		{"mixtral-8x7b", "", "mistral"},
		{"codestral-latest", "", "mistral"},
		{"pixtral-large-latest", "", "mistral"},
		// Meta
		{"llama-3.1-70b", "", "meta"},
		{"meta-llama-3.1-8b", "", "meta"},
		// DeepSeek
		{"deepseek-chat", "", "deepseek"},
		{"deepseek-coder", "", "deepseek"},
		{"deepseek-r1", "", "deepseek"},
		// xAI
		{"grok-2", "", "xai"},
		{"grok-3-mini", "", "xai"},
		// Cohere
		{"command-r-plus", "", "cohere"},
		{"embed-english-v3.0", "", "cohere"},
		{"rerank-english-v3.0", "", "cohere"},
		// Alibaba
		{"qwen-turbo", "", "alibaba"},
		{"qwen-2.5-72b", "", "alibaba"},
		// URL-based fallbacks
		{"custom-model", "https://api.openai.com", "openai"},
		{"custom-model", "https://api.anthropic.com", "anthropic"},
		{"custom-model", "https://api.groq.com", "groq"},
		{"custom-model", "https://api.together.xyz", "together"},
		{"custom-model", "https://api.together.ai", "together"},
		{"custom-model", "https://api.fireworks.ai", "fireworks"},
		{"custom-model", "https://unknown.example.com", "unknown"},
		// Case insensitivity
		{"GPT-4o", "", "openai"},
		{"DeepSeek-Chat", "", "deepseek"},
		{"CLAUDE-3-OPUS", "", "anthropic"},
	}

	for _, tt := range tests {
		got := inferProvider(tt.model, tt.url)
		if got != tt.expected {
			t.Errorf("inferProvider(%q, %q) = %q, want %q", tt.model, tt.url, got, tt.expected)
		}
	}
}
