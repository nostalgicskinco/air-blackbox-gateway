package guardrails

import (
	"sort"
	"sync"
	"time"
)

// LatencyStats holds computed latency percentiles for a model.
type LatencyStats struct {
	AvgMS int64 `json:"avg_ms"`
	P50MS int64 `json:"p50_ms"`
	P95MS int64 `json:"p95_ms"`
	P99MS int64 `json:"p99_ms"`
}

// ModelStats holds aggregated performance metrics for one model.
type ModelStats struct {
	Model            string           `json:"model"`
	RequestCount     int64            `json:"request_count"`
	SuccessCount     int64            `json:"success_count"`
	ErrorCount       int64            `json:"error_count"`
	TotalTokens      int64            `json:"total_tokens"`
	TokensPrompt     int64            `json:"tokens_prompt"`
	TokensCompletion int64            `json:"tokens_completion"`
	Latencies        []int64          `json:"-"` // raw durations, not serialized
	ErrorsByType     map[string]int64 `json:"errors_by_type"`
	LastUpdated      time.Time        `json:"last_updated"`
}

// ComputeLatency calculates latency percentiles from stored durations.
func (ms *ModelStats) ComputeLatency() LatencyStats {
	if len(ms.Latencies) == 0 {
		return LatencyStats{}
	}

	// Copy and sort to avoid mutating the original.
	sorted := make([]int64, len(ms.Latencies))
	copy(sorted, ms.Latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)
	var sum int64
	for _, v := range sorted {
		sum += v
	}

	return LatencyStats{
		AvgMS: sum / int64(n),
		P50MS: sorted[n/2],
		P95MS: sorted[percentileIndex(n, 95)],
		P99MS: sorted[percentileIndex(n, 99)],
	}
}

// ComputeErrorRate returns the error rate as a float between 0 and 1.
func (ms *ModelStats) ComputeErrorRate() float64 {
	if ms.RequestCount == 0 {
		return 0
	}
	return float64(ms.ErrorCount) / float64(ms.RequestCount)
}

func percentileIndex(n, pct int) int {
	idx := (n * pct) / 100
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// PerformanceTracker aggregates per-model performance metrics in memory.
// Thread-safe via RWMutex. Stats reset on gateway restart.
type PerformanceTracker struct {
	mu     sync.RWMutex
	models map[string]*ModelStats
}

// NewPerformanceTracker creates a new empty tracker.
func NewPerformanceTracker() *PerformanceTracker {
	return &PerformanceTracker{
		models: make(map[string]*ModelStats),
	}
}

// RecordCall records metrics for a completed LLM call.
// status should be "success" or "error". errorType is the failure classification
// (empty for successful calls).
func (pt *PerformanceTracker) RecordCall(model string, durationMS int64, promptTokens, completionTokens, totalTokens int, status string, errorType string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	ms, ok := pt.models[model]
	if !ok {
		ms = &ModelStats{
			Model:        model,
			ErrorsByType: make(map[string]int64),
		}
		pt.models[model] = ms
	}

	ms.RequestCount++
	ms.TotalTokens += int64(totalTokens)
	ms.TokensPrompt += int64(promptTokens)
	ms.TokensCompletion += int64(completionTokens)
	ms.LastUpdated = time.Now()

	// Cap latency history at 10,000 entries to prevent unbounded growth.
	if len(ms.Latencies) < 10000 {
		ms.Latencies = append(ms.Latencies, durationMS)
	} else {
		// Overwrite oldest: simple ring behavior.
		ms.Latencies[ms.RequestCount%10000] = durationMS
	}

	if status == "success" {
		ms.SuccessCount++
	} else {
		ms.ErrorCount++
		if errorType != "" {
			ms.ErrorsByType[errorType]++
		}
	}
}

// GetModelStats returns a copy of stats for a specific model.
// Returns nil if the model has no recorded calls.
func (pt *PerformanceTracker) GetModelStats(model string) *ModelStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	ms, ok := pt.models[model]
	if !ok {
		return nil
	}

	// Return a copy to avoid races.
	cp := *ms
	cp.Latencies = make([]int64, len(ms.Latencies))
	copy(cp.Latencies, ms.Latencies)
	cp.ErrorsByType = make(map[string]int64, len(ms.ErrorsByType))
	for k, v := range ms.ErrorsByType {
		cp.ErrorsByType[k] = v
	}
	return &cp
}

// GetAllStats returns copies of stats for all tracked models.
func (pt *PerformanceTracker) GetAllStats() []*ModelStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make([]*ModelStats, 0, len(pt.models))
	for _, ms := range pt.models {
		cp := *ms
		cp.Latencies = make([]int64, len(ms.Latencies))
		copy(cp.Latencies, ms.Latencies)
		cp.ErrorsByType = make(map[string]int64, len(ms.ErrorsByType))
		for k, v := range ms.ErrorsByType {
			cp.ErrorsByType[k] = v
		}
		result = append(result, &cp)
	}
	return result
}

// ErrorRate returns the error rate for a model (0.0 to 1.0).
// Returns 0 if the model has no recorded calls.
func (pt *PerformanceTracker) ErrorRate(model string) float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	ms, ok := pt.models[model]
	if !ok {
		return 0
	}
	return ms.ComputeErrorRate()
}

// LatencyP95 returns the p95 latency in milliseconds for a model.
// Returns 0 if the model has no recorded calls.
func (pt *PerformanceTracker) LatencyP95(model string) int64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	ms, ok := pt.models[model]
	if !ok {
		return 0
	}
	stats := ms.ComputeLatency()
	return stats.P95MS
}
