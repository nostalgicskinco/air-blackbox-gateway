// Package testdata provides golden test fixtures for the AIR Blackbox Gateway.
// Each fixture represents a specific scenario with a pre-built request body
// and the expected upstream response, used to validate proxy behavior,
// AIR record creation, and OTel span attributes.
package testdata

import "encoding/json"

// Fixture represents a single golden test scenario.
type Fixture struct {
	Name             string // human-readable scenario name
	RequestBody      string // JSON request body to send to the proxy
	UpstreamResponse string // JSON response the mock upstream returns
	UpstreamStatus   int    // HTTP status from mock upstream
	ExpectedModel    string // expected model in AIR record
	ExpectedProvider string // expected provider in AIR record
	ExpectedStatus   string // "success" or "error"
	ExpectedTokens   int    // expected total token count
}

// HappyPath returns a standard single-turn chat completion.
func HappyPath() Fixture {
	return Fixture{
		Name: "happy_path",
		RequestBody: `{
			"model": "gpt-4o-mini",
			"messages": [{"role": "user", "content": "What is the capital of France?"}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-abc123",
			"model": "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "The capital of France is Paris."}},
			},
			"usage": map[string]int{
				"prompt_tokens": 14, "completion_tokens": 8, "total_tokens": 22,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "gpt-4o-mini",
		ExpectedProvider: "openai",
		ExpectedStatus:   "success",
		ExpectedTokens:   22,
	}
}

// ToolCallChain returns a multi-turn conversation with tool_calls and tool responses.
func ToolCallChain() Fixture {
	return Fixture{
		Name: "tool_call_chain",
		RequestBody: `{
			"model": "gpt-4o",
			"messages": [
				{"role": "user", "content": "What's the weather in NYC?"},
				{"role": "assistant", "content": null, "tool_calls": [
					{"id": "call_001", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"NYC\"}"}}
				]},
				{"role": "tool", "tool_call_id": "call_001", "content": "{\"temp\":72,\"condition\":\"sunny\"}"},
				{"role": "user", "content": "And in London?"}
			]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-tool456",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{
						{"id": "call_002", "type": "function", "function": map[string]string{
							"name": "get_weather", "arguments": `{"city":"London"}`,
						}},
					},
				}},
			},
			"usage": map[string]int{
				"prompt_tokens": 85, "completion_tokens": 22, "total_tokens": 107,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "gpt-4o",
		ExpectedProvider: "openai",
		ExpectedStatus:   "success",
		ExpectedTokens:   107,
	}
}

// SensitivePayload returns a request containing PII (SSN, email, account numbers).
func SensitivePayload() Fixture {
	return Fixture{
		Name: "sensitive_payload",
		RequestBody: `{
			"model": "gpt-4o-mini",
			"messages": [{"role": "user", "content": "My SSN is 123-45-6789, email is john@example.com, account #ACC-9876543210. Please verify my identity."}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-pii789",
			"model": "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "I've verified your identity. Your account is active."}},
			},
			"usage": map[string]int{
				"prompt_tokens": 42, "completion_tokens": 12, "total_tokens": 54,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "gpt-4o-mini",
		ExpectedProvider: "openai",
		ExpectedStatus:   "success",
		ExpectedTokens:   54,
	}
}

// HugePayload returns a ~50KB request body to test large content handling.
func HugePayload() Fixture {
	// Build a large message content (~50KB).
	largeContent := ""
	for i := 0; i < 500; i++ {
		largeContent += "This is line " + string(rune('A'+i%26)) + " of a very large payload for stress testing the gateway proxy. "
	}

	req := map[string]interface{}{
		"model":    "gpt-4o-mini",
		"messages": []map[string]string{{"role": "user", "content": largeContent}},
	}

	return Fixture{
		Name:        "huge_payload",
		RequestBody: mustJSON(req),
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-huge",
			"model": "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "Acknowledged."}},
			},
			"usage": map[string]int{
				"prompt_tokens": 5000, "completion_tokens": 3, "total_tokens": 5003,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "gpt-4o-mini",
		ExpectedProvider: "openai",
		ExpectedStatus:   "success",
		ExpectedTokens:   5003,
	}
}

// MixedProviders returns a Claude model to test provider inference.
func MixedProviders() Fixture {
	return Fixture{
		Name: "mixed_providers",
		RequestBody: `{
			"model": "claude-3-5-sonnet-20241022",
			"messages": [{"role": "user", "content": "Hello Claude"}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "msg_claude123",
			"model": "claude-3-5-sonnet-20241022",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "Hello! How can I help?"}},
			},
			"usage": map[string]int{
				"prompt_tokens": 8, "completion_tokens": 6, "total_tokens": 14,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "claude-3-5-sonnet-20241022",
		ExpectedProvider: "anthropic",
		ExpectedStatus:   "success",
		ExpectedTokens:   14,
	}
}

// RunawayLoop returns 5 identical messages to simulate a loop scenario.
func RunawayLoop() Fixture {
	messages := make([]map[string]string, 10)
	for i := 0; i < 5; i++ {
		messages[i*2] = map[string]string{"role": "user", "content": "What is 2+2?"}
		messages[i*2+1] = map[string]string{"role": "assistant", "content": "4"}
	}

	req := map[string]interface{}{
		"model":    "gpt-4o",
		"messages": messages,
	}

	return Fixture{
		Name:        "runaway_loop",
		RequestBody: mustJSON(req),
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-loop",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "4"}},
			},
			"usage": map[string]int{
				"prompt_tokens": 50, "completion_tokens": 1, "total_tokens": 51,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "gpt-4o",
		ExpectedProvider: "openai",
		ExpectedStatus:   "success",
		ExpectedTokens:   51,
	}
}

// DeepSeekChat returns a DeepSeek model request to test provider inference.
func DeepSeekChat() Fixture {
	return Fixture{
		Name: "deepseek_chat",
		RequestBody: `{
			"model": "deepseek-chat",
			"messages": [{"role": "user", "content": "Explain quicksort in one sentence."}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-ds001",
			"model": "deepseek-chat",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "Quicksort partitions an array around a pivot, recursively sorting the sub-arrays."}},
			},
			"usage": map[string]int{
				"prompt_tokens": 12, "completion_tokens": 16, "total_tokens": 28,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "deepseek-chat",
		ExpectedProvider: "deepseek",
		ExpectedStatus:   "success",
		ExpectedTokens:   28,
	}
}

// GrokXAI returns an xAI Grok model request to test provider inference.
func GrokXAI() Fixture {
	return Fixture{
		Name: "grok_xai",
		RequestBody: `{
			"model": "grok-2",
			"messages": [{"role": "user", "content": "What is the meaning of life?"}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-grok001",
			"model": "grok-2",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "42, according to Douglas Adams."}},
			},
			"usage": map[string]int{
				"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "grok-2",
		ExpectedProvider: "xai",
		ExpectedStatus:   "success",
		ExpectedTokens:   18,
	}
}

// QwenAlibaba returns a Qwen model request to test provider inference.
func QwenAlibaba() Fixture {
	return Fixture{
		Name: "qwen_alibaba",
		RequestBody: `{
			"model": "qwen-turbo",
			"messages": [{"role": "user", "content": "Translate 'hello' to Mandarin."}]
		}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"id":    "chatcmpl-qwen001",
			"model": "qwen-turbo",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "你好 (nǐ hǎo)"}},
			},
			"usage": map[string]int{
				"prompt_tokens": 9, "completion_tokens": 7, "total_tokens": 16,
			},
		}),
		UpstreamStatus:   200,
		ExpectedModel:    "qwen-turbo",
		ExpectedProvider: "alibaba",
		ExpectedStatus:   "success",
		ExpectedTokens:   16,
	}
}

// MalformedRequest returns a request with missing model and empty messages.
func MalformedRequest() Fixture {
	return Fixture{
		Name:        "malformed_request",
		RequestBody: `{"messages": []}`,
		UpstreamResponse: mustJSON(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "model is required",
				"type":    "invalid_request_error",
			},
		}),
		UpstreamStatus:   400,
		ExpectedModel:    "",
		ExpectedProvider: "unknown",
		ExpectedStatus:   "error",
		ExpectedTokens:   0,
	}
}

// AllFixtures returns every golden fixture for table-driven tests.
func AllFixtures() []Fixture {
	return []Fixture{
		HappyPath(),
		ToolCallChain(),
		SensitivePayload(),
		HugePayload(),
		MixedProviders(),
		DeepSeekChat(),
		GrokXAI(),
		QwenAlibaba(),
		RunawayLoop(),
		MalformedRequest(),
	}
}

func mustJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
