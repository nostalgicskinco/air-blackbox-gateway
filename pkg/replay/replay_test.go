package replay

import (
	"testing"
)

func TestExtractKey(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"vault://air-runs/abc-123/request.json", "abc-123/request.json"},
		{"vault://air-runs/abc-123/response.json", "abc-123/response.json"},
		{"vault://mybucket/deep/nested/key.json", "deep/nested/key.json"},
		{"", ""},
		{"invalid", ""},
	}
	for _, tt := range tests {
		got := extractKey(tt.uri)
		if got != tt.want {
			t.Errorf("extractKey(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestExtractContent(t *testing.T) {
	resp := `{
		"choices": [{
			"message": {"role": "assistant", "content": "A flight recorder captures data."}
		}]
	}`
	got := extractContent([]byte(resp))
	if got != "A flight recorder captures data." {
		t.Errorf("extractContent = %q", got)
	}

	// Non-OpenAI format falls back to raw string.
	raw := `just some text`
	got = extractContent([]byte(raw))
	if got != raw {
		t.Errorf("extractContent fallback = %q, want %q", got, raw)
	}
}

func TestTokenSimilarity(t *testing.T) {
	// Identical strings.
	s := tokenSimilarity("hello world foo bar", "hello world foo bar")
	if s != 1.0 {
		t.Errorf("identical similarity = %f, want 1.0", s)
	}

	// Completely different.
	s = tokenSimilarity("hello world", "foo bar baz")
	if s != 0.0 {
		t.Errorf("disjoint similarity = %f, want 0.0", s)
	}

	// Partial overlap.
	s = tokenSimilarity("the quick brown fox", "the slow brown dog")
	// overlap: "the", "brown" = 2;  union: "the","quick","brown","fox","slow","dog" = 6
	expected := 2.0 / 6.0
	if s < expected-0.01 || s > expected+0.01 {
		t.Errorf("partial similarity = %f, want ~%f", s, expected)
	}

	// Both empty.
	s = tokenSimilarity("", "")
	if s != 1.0 {
		t.Errorf("empty similarity = %f, want 1.0", s)
	}

	// One empty.
	s = tokenSimilarity("hello", "")
	if s != 0.0 {
		t.Errorf("one-empty similarity = %f, want 0.0", s)
	}
}
