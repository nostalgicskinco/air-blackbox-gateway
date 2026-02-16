package guardrails

import (
	"strings"
)

// Failure taxonomy constants for classifying upstream LLM provider errors.
const (
	FailureRateLimit     = "rate_limit"
	FailureContextLength = "context_length"
	FailureInvalidReq    = "invalid_request"
	FailureServerError   = "server_error"
	FailureTimeout       = "timeout"
	FailureContentFilter = "content_filter"
	FailureAuthError     = "auth_error"
	FailureUnknown       = "unknown"
)

// ClassifyFailure maps an HTTP status code and error body to a failure category.
// Returns one of the Failure* constants. Checks status code first, then falls
// back to substring matching on the error body for ambiguous 400 responses.
func ClassifyFailure(statusCode int, errorBody string) string {
	lower := strings.ToLower(errorBody)

	// Unambiguous status codes first.
	switch statusCode {
	case 429:
		return FailureRateLimit
	case 401, 403:
		return FailureAuthError
	case 500, 502, 503:
		return FailureServerError
	case 504:
		return FailureTimeout
	}

	// Check for timeout in error body regardless of status code.
	if containsAny(lower, "timeout", "deadline exceeded", "context deadline") {
		return FailureTimeout
	}

	// 400-level errors need body inspection.
	if statusCode == 400 {
		if containsAny(lower, "context_length", "context length", "max_tokens", "maximum context", "token limit") {
			return FailureContextLength
		}
		if containsAny(lower, "content_policy", "content policy", "content filter", "filtered", "violates", "safety") {
			return FailureContentFilter
		}
		return FailureInvalidReq
	}

	// Any other 4xx.
	if statusCode >= 400 && statusCode < 500 {
		return FailureInvalidReq
	}

	return FailureUnknown
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
