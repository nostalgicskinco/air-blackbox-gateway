package guardrails

import (
	"regexp"
)

var (
	// SSN: XXX-XX-XXXX
	ssnRegex = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

	// Credit card: 13-19 digits with optional spaces/dashes (Visa, MC, Amex, Discover)
	ccRegex = regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)

	// Email: standard pattern
	emailRegex = regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)

	// US phone: (XXX) XXX-XXXX, XXX-XXX-XXXX, XXX.XXX.XXXX
	phoneRegex = regexp.MustCompile(`\b(?:\(\d{3}\)\s?|\d{3}[-.])\d{3}[-.]?\d{4}\b`)
)

// checkPII scans text for personally identifiable information.
// Returns (blocked, redactedText).
//   - If cfg.RedactMode == "block" and PII is found, blocked=true (reject the request).
//   - If cfg.RedactMode == "redact" and PII is found, the text is returned with PII replaced.
//   - If no PII is found, returns (false, original text).
func checkPII(cfg PIIConfig, text string) (bool, string) {
	if !cfg.Enabled || text == "" {
		return false, text
	}

	found := false
	redacted := text

	if cfg.BlockSSN && ssnRegex.MatchString(redacted) {
		found = true
		redacted = ssnRegex.ReplaceAllString(redacted, "[SSN]")
	}

	if cfg.BlockCC && ccRegex.MatchString(redacted) {
		found = true
		redacted = ccRegex.ReplaceAllString(redacted, "[CC]")
	}

	if cfg.BlockEmail && emailRegex.MatchString(redacted) {
		found = true
		redacted = emailRegex.ReplaceAllString(redacted, "[EMAIL]")
	}

	if cfg.BlockPhone && phoneRegex.MatchString(redacted) {
		found = true
		redacted = phoneRegex.ReplaceAllString(redacted, "[PHONE]")
	}

	if found && cfg.RedactMode == "block" {
		return true, text // blocked — return original for error context
	}

	return false, redacted
}
