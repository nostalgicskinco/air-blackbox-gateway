package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitForAIRRecord polls for an AIR record file to appear in dir.
// Background goroutines write AIR records asynchronously, so tests
// must wait briefly for them to land on disk.
func waitForAIRRecord(t *testing.T, dir, runID string) string {
	t.Helper()
	airFile := filepath.Join(dir, runID+".air.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(airFile); err == nil {
			return airFile
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("AIR record %s not written within 2s", airFile)
	return ""
}

// waitForAIRRecords polls for a specific count of .air.json files in dir.
func waitForAIRRecords(t *testing.T, dir string, count int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(dir)
		n := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".air.json") {
				n++
			}
		}
		if n >= count {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".air.json") {
			n++
		}
	}
	return n
}
