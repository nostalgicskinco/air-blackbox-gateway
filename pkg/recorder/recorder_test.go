package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	rec := Record{
		RunID:            "test-run-001",
		TraceID:          "abc123",
		Timestamp:        time.Now().UTC(),
		Model:            "gpt-4o-mini",
		Provider:         "openai",
		Endpoint:         "/v1/chat/completions",
		RequestVaultRef:  "vault://air-runs/test-run-001/request.json",
		ResponseVaultRef: "vault://air-runs/test-run-001/response.json",
		RequestChecksum:  "sha256:aaa",
		ResponseChecksum: "sha256:bbb",
		Tokens:           Tokens{Prompt: 10, Completion: 20, Total: 30},
		DurationMS:       450,
		Status:           "success",
	}

	if err := w.Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(dir, "test-run-001.air.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", loaded.Version)
	}
	if loaded.RunID != "test-run-001" {
		t.Errorf("run_id = %q, want test-run-001", loaded.RunID)
	}
	if loaded.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", loaded.Model)
	}
	if loaded.Tokens.Total != 30 {
		t.Errorf("tokens.total = %d, want 30", loaded.Tokens.Total)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/file.air.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter with nested dir: %v", err)
	}

	rec := Record{RunID: "nested-test", Status: "success"}
	if err := w.Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "nested-test.air.json")); err != nil {
		t.Fatalf("nested file not created: %v", err)
	}
	_ = w
}
