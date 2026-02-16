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
	"github.com/nostalgicskinco/air-blackbox-gateway/testdata"
)

// TestGoldenFixtures runs all 7 golden scenarios through the proxy and
// validates AIR record fields against expected values.
func TestGoldenFixtures(t *testing.T) {
	for _, fix := range testdata.AllFixtures() {
		t.Run(fix.Name, func(t *testing.T) {
			// Mock upstream that returns the fixture's canned response.
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(fix.UpstreamStatus)
				w.Write([]byte(fix.UpstreamResponse))
			}))
			defer upstream.Close()

			dir := t.TempDir()
			rec, err := recorder.NewWriter(dir)
			if err != nil {
				t.Fatalf("recorder: %v", err)
			}

			cfg := Config{
				ProviderURL: upstream.URL,
				Recorder:    rec,
			}

			h := Handler(cfg)
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer sk-test")
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			// Every request should get a run_id.
			runID := w.Header().Get("x-run-id")
			if runID == "" {
				t.Fatal("missing x-run-id header")
			}

			// Wait for background goroutine to write the AIR record.
			airFile := waitForAIRRecord(t, dir, runID)
			loaded, err := recorder.Load(airFile)
			if err != nil {
				t.Fatalf("load AIR record: %v", err)
			}

			// Version must be set.
			if loaded.Version != "1.0.0" {
				t.Errorf("version = %q, want 1.0.0", loaded.Version)
			}

			// Run ID must match header.
			if loaded.RunID != runID {
				t.Errorf("run_id = %q, want %q", loaded.RunID, runID)
			}

			// Model.
			if loaded.Model != fix.ExpectedModel {
				t.Errorf("model = %q, want %q", loaded.Model, fix.ExpectedModel)
			}

			// Provider inference.
			if loaded.Provider != fix.ExpectedProvider {
				t.Errorf("provider = %q, want %q", loaded.Provider, fix.ExpectedProvider)
			}

			// Endpoint.
			if loaded.Endpoint != "/v1/chat/completions" {
				t.Errorf("endpoint = %q, want /v1/chat/completions", loaded.Endpoint)
			}

			// Status.
			if loaded.Status != fix.ExpectedStatus {
				t.Errorf("status = %q, want %q", loaded.Status, fix.ExpectedStatus)
			}

			// Tokens (only check on success responses that include usage).
			if fix.ExpectedTokens > 0 && loaded.Tokens.Total != fix.ExpectedTokens {
				t.Errorf("tokens.total = %d, want %d", loaded.Tokens.Total, fix.ExpectedTokens)
			}

			// Duration must be non-negative.
			if loaded.DurationMS < 0 {
				t.Errorf("duration_ms = %d, want >= 0", loaded.DurationMS)
			}

			// Timestamp must be set.
			if loaded.Timestamp.IsZero() {
				t.Error("timestamp is zero")
			}
		})
	}
}

// TestGoldenFixtures_NoVault verifies the proxy works when vault is nil.
func TestGoldenFixtures_NoVault(t *testing.T) {
	fix := testdata.HappyPath()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)

	cfg := Config{
		ProviderURL: upstream.URL,
		Vault:       nil, // explicitly nil
		Recorder:    rec,
	}

	h := Handler(cfg)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	runID := w.Header().Get("x-run-id")
	airFile := waitForAIRRecord(t, dir, runID)
	loaded, err := recorder.Load(airFile)
	if err != nil {
		t.Fatalf("load AIR: %v", err)
	}

	// Without vault, refs should be empty but record still valid.
	if loaded.RequestVaultRef != "" {
		t.Errorf("request_vault_ref should be empty without vault, got %q", loaded.RequestVaultRef)
	}
	if loaded.Status != "success" {
		t.Errorf("status = %q, want success", loaded.Status)
	}
}

// TestGoldenFixtures_UpstreamDown verifies behavior when the upstream is unreachable.
func TestGoldenFixtures_UpstreamDown(t *testing.T) {
	dir := t.TempDir()
	rec, _ := recorder.NewWriter(dir)

	cfg := Config{
		ProviderURL: "http://127.0.0.1:1", // nothing listening
		Recorder:    rec,
	}

	h := Handler(cfg)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}

	// Verify error response body is non-empty and mentions the error.
	respBody := w.Body.String()
	if !strings.Contains(respBody, "upstream") {
		t.Errorf("expected error body to mention upstream, got: %s", respBody)
	}

	// Wait for background goroutine to write the AIR record.
	n := waitForAIRRecords(t, dir, 1)
	if n == 0 {
		t.Fatal("no AIR record written for upstream failure")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".air.json") {
			loaded, err := recorder.Load(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("load AIR: %v", err)
			}
			if loaded.Status != "error" {
				t.Errorf("status = %q, want error", loaded.Status)
			}
			if loaded.Error == "" {
				t.Error("error field should be non-empty")
			}
		}
	}
}

// TestGoldenFixtures_ResponsePassthrough verifies the upstream response body is
// forwarded exactly to the caller.
func TestGoldenFixtures_ResponsePassthrough(t *testing.T) {
	fix := testdata.HappyPath()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fix.UpstreamResponse))
	}))
	defer upstream.Close()

	cfg := Config{ProviderURL: upstream.URL}
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fix.RequestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	respBody, _ := io.ReadAll(w.Result().Body)

	// Parse both and compare key fields (not byte-identical due to encoding).
	var got, want map[string]interface{}
	json.Unmarshal(respBody, &got)
	json.Unmarshal([]byte(fix.UpstreamResponse), &want)

	if got["id"] != want["id"] {
		t.Errorf("response id = %v, want %v", got["id"], want["id"])
	}
	if got["model"] != want["model"] {
		t.Errorf("response model = %v, want %v", got["model"], want["model"])
	}
}

// TestGoldenFixtures_AuthForwarding verifies the Authorization header is forwarded
// to the upstream provider.
func TestGoldenFixtures_AuthForwarding(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testdata.HappyPath().UpstreamResponse))
	}))
	defer upstream.Close()

	cfg := Config{ProviderURL: upstream.URL}
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(testdata.HappyPath().RequestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test-key-12345")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if receivedAuth != "Bearer sk-test-key-12345" {
		t.Errorf("upstream received auth = %q, want Bearer sk-test-key-12345", receivedAuth)
	}
}
