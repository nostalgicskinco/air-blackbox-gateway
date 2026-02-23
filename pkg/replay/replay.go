// Package replay fetches a recorded AIR run from vault, replays it against
// the LLM provider, and reports behavioral drift.
package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/airblackbox/gateway/pkg/recorder"
	"github.com/airblackbox/gateway/pkg/vault"
)

// Result holds the outcome of a replay.
type Result struct {
	RunID          string  `json:"run_id"`
	OriginalModel  string  `json:"original_model"`
	ReplayModel    string  `json:"replay_model"`
	Drift          bool    `json:"drift"`
	DriftSummary   string  `json:"drift_summary,omitempty"`
	OriginalTokens int     `json:"original_tokens"`
	ReplayTokens   int     `json:"replay_tokens"`
	Similarity     float64 `json:"similarity"` // 0.0–1.0 basic token overlap
}

// Options configures a replay.
type Options struct {
	ProviderURL string       // upstream provider for replay
	VaultClient *vault.Client // to fetch original request/response
	APIKey      string       // provider API key for replay
}

// Run loads an AIR record, fetches the original request from vault,
// replays it, and compares responses.
func Run(ctx context.Context, rec recorder.Record, opts Options) (Result, error) {
	result := Result{
		RunID:          rec.RunID,
		OriginalModel:  rec.Model,
		OriginalTokens: rec.Tokens.Total,
	}

	// Fetch original request from vault.
	reqKey := extractKey(rec.RequestVaultRef)
	if reqKey == "" {
		return result, fmt.Errorf("replay: no request vault ref in AIR record")
	}

	reqData, err := opts.VaultClient.Fetch(ctx, reqKey)
	if err != nil {
		return result, fmt.Errorf("replay: fetch request: %w", err)
	}

	// Verify checksum.
	if rec.RequestChecksum != "" && !vault.VerifyChecksum(reqData, rec.RequestChecksum) {
		return result, fmt.Errorf("replay: request checksum mismatch (tampered?)")
	}

	// Fetch original response for comparison.
	respKey := extractKey(rec.ResponseVaultRef)
	var originalResp []byte
	if respKey != "" {
		originalResp, err = opts.VaultClient.Fetch(ctx, respKey)
		if err != nil {
			return result, fmt.Errorf("replay: fetch response: %w", err)
		}
		if rec.ResponseChecksum != "" && !vault.VerifyChecksum(originalResp, rec.ResponseChecksum) {
			return result, fmt.Errorf("replay: response checksum mismatch (tampered?)")
		}
	}

	// Replay: send the same request to the provider.
	providerURL := opts.ProviderURL
	if providerURL == "" {
		providerURL = "https://api.openai.com"
	}

	replayReq, err := http.NewRequestWithContext(ctx, "POST",
		providerURL+rec.Endpoint, bytes.NewReader(reqData))
	if err != nil {
		return result, fmt.Errorf("replay: create request: %w", err)
	}
	replayReq.Header.Set("Content-Type", "application/json")
	if opts.APIKey != "" {
		replayReq.Header.Set("Authorization", "Bearer "+opts.APIKey)
	}

	resp, err := http.DefaultClient.Do(replayReq)
	if err != nil {
		return result, fmt.Errorf("replay: upstream: %w", err)
	}
	defer resp.Body.Close()

	replayBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("replay: read response: %w", err)
	}

	// Parse replay response for tokens.
	var replayParsed struct {
		Model string `json:"model"`
		Usage *struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(replayBody, &replayParsed); err == nil {
		result.ReplayModel = replayParsed.Model
		if replayParsed.Usage != nil {
			result.ReplayTokens = replayParsed.Usage.TotalTokens
		}
	}

	// Compare: extract content from both responses.
	originalContent := extractContent(originalResp)
	replayContent := extractContent(replayBody)

	result.Similarity = tokenSimilarity(originalContent, replayContent)
	result.Drift = result.Similarity < 0.8

	if result.Drift {
		result.DriftSummary = fmt.Sprintf(
			"similarity=%.2f (threshold=0.80); original=%d chars, replay=%d chars",
			result.Similarity, len(originalContent), len(replayContent))
	}

	return result, nil
}

// extractKey converts "vault://bucket/key" → "key"
func extractKey(uri string) string {
	if uri == "" {
		return ""
	}
	// vault://bucket/run_id/file.json → run_id/file.json
	parts := strings.SplitN(uri, "//", 2)
	if len(parts) != 2 {
		return ""
	}
	bucketAndKey := parts[1] // bucket/run_id/file.json
	idx := strings.Index(bucketAndKey, "/")
	if idx < 0 {
		return ""
	}
	return bucketAndKey[idx+1:]
}

// extractContent pulls the assistant message content from an OpenAI response.
func extractContent(data []byte) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err == nil && len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content
	}
	return string(data)
}

// tokenSimilarity computes a basic word-overlap Jaccard similarity.
func tokenSimilarity(a, b string) float64 {
	if a == "" && b == "" {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}

	setA := tokenSet(a)
	setB := tokenSet(b)

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA)
	for w := range setB {
		if !setA[w] {
			union++
		}
	}

	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

func tokenSet(s string) map[string]bool {
	words := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}
