// Package proxy implements an OpenAI-compatible reverse proxy that intercepts
// LLM calls, vaults content, emits OTel spans, and writes AIR records.
package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/guardrails"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/recorder"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/vault"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("air-blackbox-gateway")

// upstreamClient is an HTTP client with explicit timeouts for LLM provider calls.
// The default Go http.Client has no timeout, which can hang goroutines forever.
var upstreamClient = &http.Client{
	Timeout: 120 * time.Second, // LLM calls can be slow; 2 min is generous but safe.
}

// Config holds proxy configuration.
type Config struct {
	ProviderURL string           // e.g. https://api.openai.com
	Vault       *vault.Client    // S3 vault for content (nil = disabled)
	Recorder    *recorder.Writer // AIR file writer (nil = disabled)
	GatewayKey  string           // optional API key required to use the gateway
	Guardrails  *guardrails.Config  // guardrails configuration (nil = disabled)
	Sessions    *guardrails.Manager // session state for guardrails (nil = disabled)
}

// Handler returns an http.Handler that proxies OpenAI-compatible requests.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if !authenticateGateway(w, r, cfg.GatewayKey) {
			return
		}
		handleProxy(w, r, cfg, "/v1/chat/completions")
	})

	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if !authenticateGateway(w, r, cfg.GatewayKey) {
			return
		}
		handleProxy(w, r, cfg, "/v1/responses")
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
}

// authenticateGateway checks the x-gateway-key header if a key is configured.
// Returns true if auth passes (or no key is configured), false if rejected.
func authenticateGateway(w http.ResponseWriter, r *http.Request, gatewayKey string) bool {
	if gatewayKey == "" {
		return true // no gateway auth configured
	}
	provided := r.Header.Get("X-Gateway-Key")
	if provided == "" {
		provided = r.Header.Get("X-Api-Key")
	}
	if provided != gatewayKey {
		http.Error(w, `{"error":"unauthorized: invalid or missing gateway key"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

// chatRequest is the minimal OpenAI chat completion request we need to parse.
type chatRequest struct {
	Model    string          `json:"model"`
	Messages json.RawMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
}

// chatResponse is the minimal response structure for token extraction.
type chatResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func handleProxy(w http.ResponseWriter, r *http.Request, cfg Config, endpoint string) {
	start := time.Now()

	// Generate run ID.
	runID := uuid.New().String()

	ctx, span := tracer.Start(r.Context(), "llm.call",
		trace.WithAttributes(
			attribute.String("gen_ai.run.id", runID),
			attribute.String("gen_ai.request.endpoint", endpoint),
		),
	)
	defer span.End()

	// Read request body.
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Parse model name and check for streaming.
	var req chatRequest
	if err := json.Unmarshal(reqBody, &req); err == nil {
		span.SetAttributes(
			attribute.String("gen_ai.request.model", req.Model),
		)
	}

	// Determine provider from model name.
	provider := inferProvider(req.Model, cfg.ProviderURL)
	span.SetAttributes(attribute.String("gen_ai.system", provider))

	// --- Prevention layer (opt-in) ---
	// Runs BEFORE detection. May modify the request body (PII redaction, tool filtering,
	// model downgrade) or block entirely. Returns 403 for policy blocks.
	if cfg.Guardrails != nil {
		sessionID := extractSessionID(r)
		promptText := extractPromptText(req.Messages)
		toolNames := extractToolNames(reqBody)
		sessionTokens := 0
		if cfg.Sessions != nil {
			sessionTokens = cfg.Sessions.GetSessionTokens(sessionID)
		}

		prevResult := guardrails.EvaluatePrevention(cfg.Guardrails, reqBody, promptText, toolNames, req.Model, sessionTokens)
		if prevResult.Blocked {
			log.Printf("[prevention] blocked: %s (session=%s)", prevResult.BlockReason, sessionID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"type":    "prevention_policy_blocked",
					"message": prevResult.BlockReason,
				},
			})
			go guardrails.SendWebhookAlert(cfg.Guardrails.Alerts.WebhookURL, &guardrails.Violation{
				Rule:      "prevention",
				Message:   prevResult.BlockReason,
				SessionID: sessionID,
			})
			return
		}
		if prevResult.ModifiedBody != nil {
			reqBody = prevResult.ModifiedBody
			// Re-parse the modified body so downstream uses the updated model/messages.
			json.Unmarshal(reqBody, &req)
			log.Printf("[prevention] request modified: pii_redacted=%v tools_filtered=%v model_downgraded=%s",
				prevResult.PIIRedacted, prevResult.ToolsFiltered, prevResult.ModelDowngraded)
		}
	}

	// --- Detection layer (opt-in) ---
	// Catches runaway agents: token budgets, prompt loops, tool retry storms, error spirals.
	// Returns 429 for guardrail violations. Approval webhook can override blocks.
	if cfg.Guardrails != nil && cfg.Sessions != nil {
		sessionID := extractSessionID(r)
		cfg.Sessions.GetOrCreate(sessionID)

		promptText := extractPromptText(req.Messages)
		toolNames := extractToolNames(reqBody)
		cfg.Sessions.RecordRequest(sessionID, promptText, toolNames)

		evalReq := &guardrails.EvalRequest{
			PromptText: promptText,
			ToolNames:  toolNames,
			Model:      req.Model,
		}
		if v := guardrails.Evaluate(cfg.Guardrails, cfg.Sessions, sessionID, evalReq); v != nil {
			// Check approval webhook before blocking.
			approved, _ := guardrails.RequestApproval(r.Context(), cfg.Guardrails.Prevention.Approval, v)
			if approved {
				log.Printf("[guardrails] %s: approved via webhook (session=%s)", v.Rule, sessionID)
			} else {
				log.Printf("[guardrails] %s: %s (session=%s)", v.Rule, v.Message, sessionID)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{
						"type":       "agent_guardrail_triggered",
						"rule":       v.Rule,
						"message":    v.Message,
						"session_id": v.SessionID,
						"details":    v.Details,
					},
				})
				go guardrails.SendWebhookAlert(cfg.Guardrails.Alerts.WebhookURL, v)
				cfg.Sessions.Remove(sessionID)
				return
			}
		}
	}

	// Forward to upstream provider.
	upstream := cfg.ProviderURL + endpoint
	proxyReq, err := http.NewRequestWithContext(ctx, "POST", upstream, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Copy relevant headers.
	proxyReq.Header.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	resp, err := upstreamClient.Do(proxyReq)
	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		http.Error(w, fmt.Sprintf(`{"error":"upstream: %s"}`, err), http.StatusBadGateway)

		// Fire-and-forget: vault + AIR record for failed requests.
		go backgroundRecord(cfg, runID, span, req.Model, provider, endpoint,
			reqBody, nil, start, "error", err.Error())
		return
	}
	defer resp.Body.Close()

	// Set run_id header early so the client gets it even for streaming responses.
	w.Header().Set("x-run-id", runID)
	for _, h := range []string{"x-request-id", "openai-organization"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}

	// --- Streaming vs non-streaming response handling ---
	if req.Stream && resp.Header.Get("Content-Type") == "text/event-stream" {
		handleStreamingResponse(w, resp, cfg, runID, span, req, provider, endpoint, reqBody, start)
	} else {
		handleBufferedResponse(w, resp, cfg, runID, span, req, provider, endpoint, reqBody, start)
	}

	// Update guardrails session state after response.
	if cfg.Guardrails != nil && cfg.Sessions != nil {
		sessionID := extractSessionID(r)
		isError := resp.StatusCode >= 400
		cfg.Sessions.RecordResponse(sessionID, 0, isError)
	}
}

// handleStreamingResponse forwards SSE chunks to the client in real-time while
// capturing the full response in the background for vault storage.
func handleStreamingResponse(w http.ResponseWriter, resp *http.Response,
	cfg Config, runID string, span trace.Span, req chatRequest,
	provider, endpoint string, reqBody []byte, start time.Time) {

	// Set streaming headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)

	// Buffer the full response for vault/recording while streaming to client.
	var fullResponse bytes.Buffer
	buf := make([]byte, 4096)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			fullResponse.Write(buf[:n])
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[%s] stream read error: %v", runID, err)
			}
			break
		}
	}

	duration := time.Since(start)
	span.SetAttributes(attribute.Int64("gen_ai.duration_ms", duration.Milliseconds()))
	span.SetAttributes(attribute.Bool("gen_ai.stream", true))

	// Parse token usage from the final SSE chunk if available.
	// OpenAI includes usage in the last data chunk when stream_options.include_usage=true.
	respBytes := fullResponse.Bytes()
	tokens := extractStreamTokens(respBytes)
	if tokens.Total > 0 {
		span.SetAttributes(
			attribute.Int("gen_ai.usage.prompt_tokens", tokens.Prompt),
			attribute.Int("gen_ai.usage.completion_tokens", tokens.Completion),
		)
	}

	status := "success"
	if resp.StatusCode >= 400 {
		status = "error"
	}

	log.Printf("[%s] %s model=%s tokens=%d duration=%dms status=%s stream=true",
		runID, endpoint, req.Model, tokens.Total, duration.Milliseconds(), status)

	// Fire-and-forget: vault + AIR record in background.
	go backgroundRecord(cfg, runID, span, req.Model, provider, endpoint,
		reqBody, respBytes, start, status, "")
}

// handleBufferedResponse handles traditional (non-streaming) responses.
func handleBufferedResponse(w http.ResponseWriter, resp *http.Response,
	cfg Config, runID string, span trace.Span, req chatRequest,
	provider, endpoint string, reqBody []byte, start time.Time) {

	// Read response body.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	// Extract token usage.
	var tokens recorder.Tokens
	var respParsed chatResponse
	if err := json.Unmarshal(respBody, &respParsed); err == nil && respParsed.Usage != nil {
		tokens = recorder.Tokens{
			Prompt:     respParsed.Usage.PromptTokens,
			Completion: respParsed.Usage.CompletionTokens,
			Total:      respParsed.Usage.TotalTokens,
		}
		span.SetAttributes(
			attribute.Int("gen_ai.usage.prompt_tokens", tokens.Prompt),
			attribute.Int("gen_ai.usage.completion_tokens", tokens.Completion),
			attribute.String("gen_ai.response.model", respParsed.Model),
		)
	}

	duration := time.Since(start)
	span.SetAttributes(attribute.Int64("gen_ai.duration_ms", duration.Milliseconds()))

	status := "success"
	if resp.StatusCode >= 400 {
		status = "error"
	}

	// Return response to caller.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	log.Printf("[%s] %s model=%s tokens=%d duration=%dms status=%s",
		runID, endpoint, req.Model, tokens.Total, duration.Milliseconds(), status)

	// Fire-and-forget: vault + AIR record in background.
	go backgroundRecord(cfg, runID, span, req.Model, provider, endpoint,
		reqBody, respBody, start, status, "")
}

// backgroundRecord handles vault storage and AIR record writing off the hot path.
// Failures are logged but never block the response to the caller.
func backgroundRecord(cfg Config, runID string, span trace.Span,
	model, provider, endpoint string,
	reqBody, respBody []byte, start time.Time, status, errMsg string) {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Vault the request (best-effort).
	reqRef, err := vaultStore(ctx, cfg.Vault, runID, "request.json", reqBody)
	if err != nil {
		log.Printf("[%s] vault request (background): %v", runID, err)
	}

	// Vault the response (best-effort).
	var respRef vault.Ref
	if respBody != nil {
		respRef, err = vaultStore(ctx, cfg.Vault, runID, "response.json", respBody)
		if err != nil {
			log.Printf("[%s] vault response (background): %v", runID, err)
		}
	}

	// Extract tokens from response if we have it.
	var tokens recorder.Tokens
	if respBody != nil {
		var respParsed chatResponse
		if err := json.Unmarshal(respBody, &respParsed); err == nil && respParsed.Usage != nil {
			tokens = recorder.Tokens{
				Prompt:     respParsed.Usage.PromptTokens,
				Completion: respParsed.Usage.CompletionTokens,
				Total:      respParsed.Usage.TotalTokens,
			}
		} else {
			// Try extracting from SSE stream.
			tokens = extractStreamTokens(respBody)
		}
	}

	// Write AIR record (best-effort).
	writeAIRRecord(cfg.Recorder, runID, span, model, provider, endpoint,
		reqRef, respRef, tokens, start, status, errMsg)
}

// extractStreamTokens attempts to extract token usage from the last SSE data chunk.
// OpenAI includes usage when the request has stream_options.include_usage=true.
func extractStreamTokens(data []byte) recorder.Tokens {
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil && chunk.Usage != nil {
			return recorder.Tokens{
				Prompt:     chunk.Usage.PromptTokens,
				Completion: chunk.Usage.CompletionTokens,
				Total:      chunk.Usage.TotalTokens,
			}
		}
	}
	return recorder.Tokens{}
}

func vaultStore(ctx context.Context, vc *vault.Client, runID, name string, data []byte) (vault.Ref, error) {
	if vc == nil {
		return vault.Ref{}, nil
	}
	key := runID + "/" + name
	return vc.Store(ctx, key, data)
}

func writeAIRRecord(w *recorder.Writer, runID string, span trace.Span, model, provider, endpoint string,
	reqRef, respRef vault.Ref, tokens recorder.Tokens, start time.Time, status, errMsg string) {

	if w == nil {
		return
	}

	traceID := ""
	if sc := span.SpanContext(); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}

	rec := recorder.Record{
		RunID:            runID,
		TraceID:          traceID,
		Timestamp:        start.UTC(),
		Model:            model,
		Provider:         provider,
		Endpoint:         endpoint,
		RequestVaultRef:  reqRef.URI,
		ResponseVaultRef: respRef.URI,
		RequestChecksum:  reqRef.Checksum,
		ResponseChecksum: respRef.Checksum,
		Tokens:           tokens,
		DurationMS:       time.Since(start).Milliseconds(),
		Status:           status,
		Error:            errMsg,
	}

	if err := w.Write(rec); err != nil {
		log.Printf("[%s] write AIR record: %v", runID, err)
	}
}

// extractSessionID derives a session identifier from the request.
// Checks X-Session-ID header first, then falls back to a hash of the Authorization header.
func extractSessionID(r *http.Request) string {
	if sid := r.Header.Get("X-Session-ID"); sid != "" {
		return sid
	}
	auth := r.Header.Get("Authorization")
	if auth != "" {
		h := sha256.Sum256([]byte(auth))
		return fmt.Sprintf("auth_%x", h[:8])
	}
	return "anonymous"
}

// extractPromptText pulls the last user message content from raw messages JSON.
func extractPromptText(messages json.RawMessage) string {
	if messages == nil {
		return ""
	}
	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages, &msgs); err != nil {
		return ""
	}
	// Walk backwards to find the last user message.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			// Content can be a string or an array of content parts.
			var text string
			if err := json.Unmarshal(msgs[i].Content, &text); err == nil {
				return text
			}
			// Try array of content parts.
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(msgs[i].Content, &parts); err == nil {
				for _, p := range parts {
					if p.Type == "text" {
						return p.Text
					}
				}
			}
		}
	}
	return ""
}

// extractToolNames pulls tool/function names from the request body.
func extractToolNames(body []byte) []string {
	var req struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice interface{} `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	var names []string
	for _, t := range req.Tools {
		if t.Function.Name != "" {
			names = append(names, t.Function.Name)
		}
	}
	return names
}

func inferProvider(model, providerURL string) string {
	model = strings.ToLower(model)
	switch {
	case strings.HasPrefix(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"),
		strings.HasPrefix(model, "chatgpt"), strings.HasPrefix(model, "dall-e"):
		return "openai"
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "gemini"):
		return "google"
	case strings.HasPrefix(model, "mistral"), strings.HasPrefix(model, "mixtral"),
		strings.HasPrefix(model, "codestral"), strings.HasPrefix(model, "pixtral"):
		return "mistral"
	case strings.HasPrefix(model, "llama"), strings.HasPrefix(model, "meta-llama"):
		return "meta"
	case strings.HasPrefix(model, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(model, "grok"):
		return "xai"
	case strings.HasPrefix(model, "command"), strings.HasPrefix(model, "embed-"),
		strings.HasPrefix(model, "rerank-"):
		return "cohere"
	case strings.HasPrefix(model, "qwen"):
		return "alibaba"
	case strings.Contains(providerURL, "openai.com"):
		return "openai"
	case strings.Contains(providerURL, "anthropic.com"):
		return "anthropic"
	case strings.Contains(providerURL, "groq.com"):
		return "groq"
	case strings.Contains(providerURL, "together.xyz"), strings.Contains(providerURL, "together.ai"):
		return "together"
	case strings.Contains(providerURL, "fireworks.ai"):
		return "fireworks"
	default:
		return "unknown"
	}
}
