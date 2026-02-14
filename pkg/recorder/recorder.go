// Package recorder writes AIR (AI Incident Record) files — portable,
// tamper-evident audit records for every LLM interaction.
package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record is the AIR file format — one per LLM call.
type Record struct {
	Version          string    `json:"version"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider"`
	Endpoint         string    `json:"endpoint"`
	RequestVaultRef  string    `json:"request_vault_ref"`
	ResponseVaultRef string    `json:"response_vault_ref"`
	RequestChecksum  string    `json:"request_checksum"`
	ResponseChecksum string    `json:"response_checksum"`
	Tokens           Tokens    `json:"tokens"`
	DurationMS       int64     `json:"duration_ms"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
}

// Tokens holds token usage from the provider response.
type Tokens struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
	Total      int `json:"total"`
}

// Writer writes AIR records to a directory.
type Writer struct {
	dir string
}

// NewWriter creates a writer that saves AIR files to dir.
func NewWriter(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("recorder: create dir: %w", err)
	}
	return &Writer{dir: dir}, nil
}

// Write persists an AIR record as <run_id>.air.json.
func (w *Writer) Write(r Record) error {
	r.Version = "1.0.0"

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("recorder: marshal: %w", err)
	}

	path := filepath.Join(w.dir, r.RunID+".air.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("recorder: write %s: %w", path, err)
	}
	return nil
}

// Load reads an AIR record from a file path.
func Load(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("recorder: read %s: %w", path, err)
	}

	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("recorder: parse %s: %w", path, err)
	}
	return r, nil
}
