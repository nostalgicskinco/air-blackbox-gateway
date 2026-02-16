// Package trust provides cryptographic audit chains, compliance evaluation,
// and evidence export for the AIR Blackbox Gateway trust layer.
package trust

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ChainEntry is one signed link in the audit chain.
// Each entry includes the hash of the previous entry, forming a tamper-proof
// chain similar to a blockchain — modifying any record breaks the chain.
type ChainEntry struct {
	Sequence   int64     `json:"sequence"`    // monotonic counter (1-based)
	RunID      string    `json:"run_id"`      // the AIR record this signs
	RecordHash string    `json:"record_hash"` // sha256 of the AIR record JSON
	PrevHash   string    `json:"prev_hash"`   // hash of the previous ChainEntry (empty for first)
	Signature  string    `json:"signature"`   // HMAC-SHA256(sequence|run_id|record_hash|prev_hash, secret)
	Timestamp  time.Time `json:"timestamp"`
}

// AuditChain maintains an ordered, signed sequence of AIR record hashes.
// It is safe for concurrent use.
type AuditChain struct {
	mu      sync.Mutex
	secret  []byte
	entries []ChainEntry
	last    string // hash of last entry (for chaining)
	seq     int64
}

// NewAuditChain creates a new audit chain with the given HMAC signing key.
func NewAuditChain(secret string) *AuditChain {
	return &AuditChain{
		secret:  []byte(secret),
		entries: make([]ChainEntry, 0),
	}
}

// Append adds a new AIR record to the chain. It computes the SHA-256 hash of
// the record JSON, signs it with the previous entry's hash, and returns the
// new chain entry.
func (ac *AuditChain) Append(runID string, recordJSON []byte) ChainEntry {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.seq++

	// Hash the AIR record content.
	recordHash := sha256Hex(recordJSON)

	// Build the entry.
	entry := ChainEntry{
		Sequence:   ac.seq,
		RunID:      runID,
		RecordHash: recordHash,
		PrevHash:   ac.last,
		Timestamp:  time.Now().UTC(),
	}

	// Sign: HMAC-SHA256(sequence|run_id|record_hash|prev_hash, secret)
	entry.Signature = ac.sign(entry)

	// Hash this entry to become the prev_hash for the next one.
	entryJSON, _ := json.Marshal(entry)
	ac.last = sha256Hex(entryJSON)

	ac.entries = append(ac.entries, entry)
	return entry
}

// Verify walks the chain and checks that every entry's signature is valid
// and every prev_hash matches the actual hash of the previous entry.
// Returns (true, 0, nil) if valid, or (false, brokenAt, err) if tampered.
func (ac *AuditChain) Verify() (valid bool, brokenAt int64, err error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if len(ac.entries) == 0 {
		return true, 0, nil
	}

	prevHash := ""
	for i, entry := range ac.entries {
		// Check prev_hash matches.
		if entry.PrevHash != prevHash {
			return false, entry.Sequence, fmt.Errorf(
				"chain broken at sequence %d: prev_hash mismatch", entry.Sequence)
		}

		// Recompute and verify signature.
		expected := ac.sign(entry)
		if entry.Signature != expected {
			return false, entry.Sequence, fmt.Errorf(
				"chain broken at sequence %d: signature mismatch", entry.Sequence)
		}

		// Hash this entry to verify the next one's prev_hash.
		entryJSON, _ := json.Marshal(ac.entries[i])
		prevHash = sha256Hex(entryJSON)
	}

	return true, 0, nil
}

// Entries returns a copy of all chain entries.
func (ac *AuditChain) Entries() []ChainEntry {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	out := make([]ChainEntry, len(ac.entries))
	copy(out, ac.entries)
	return out
}

// Len returns the number of entries in the chain.
func (ac *AuditChain) Len() int64 {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.seq
}

// sign computes the HMAC-SHA256 signature for a chain entry.
func (ac *AuditChain) sign(e ChainEntry) string {
	msg := fmt.Sprintf("%d|%s|%s|%s", e.Sequence, e.RunID, e.RecordHash, e.PrevHash)
	mac := hmac.New(sha256.New, ac.secret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// sha256Hex computes the hex-encoded SHA-256 hash of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
