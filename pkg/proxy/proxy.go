// Package proxy implements an OpenAI-compatible reverse proxy that intercepts
// LLM calls, vaults content, emits OTel spans, and writes AIR records.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/recorder"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/vault"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("air-blackbox-gateway")

// Config holds proxy configuration.
type Config struct {
	ProviderURL string           // e.g. https://api.openai.com
	Vault       *vault.Client    // S3 vault for content
	Recorder    *recorder.Writer // AIR file writer
}

// Handler returns an http.Handler that proxies OpenAI-compatible requests.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		handleProxy(w, r, cfg, "/v1/chat/completions")
	})

	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		handleProxy(w, r, cfg, "/v1/responses")
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
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

	// Parse model name for span attributes.
	var req chatRequest
	if err := json.Unmarshal(reqBody, &req); err == nil {
		span.SetAttributes(
			attribute.String("gen_ai.request.model", req.Model),
		)
	}

	// Vault the request.
	reqRef, err := vaultStore(ctx, cfg.Vault, runID, "request.json", reqBody)
	if err != nil {
		log.Printf("[%s] vault request: %v", runID, err)
	}

	// Determine provider from model name.
	provider := inferProvider(req.Model, cfg.ProviderURL)
	span.SetAttributes(attribute.String("gen_ai.system", provider))

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

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		http.Error(w, fmt.Sprintf(`{"error":"upstream: %s"}`, err), http.StatusBadGateway)

		writeAIRRecord(cfg.Recorder, runID, span, req.Model, provider, endpoint,
			reqRef, vault.Ref{}, recorder.Tokens{}, start, "error", err.Error())
		return
	}
	defer resp.Body.Close()

	// Read response body.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	// Vault the response.
	respRef, err := vaultStore(ctx, cfg.Vault, runID, "response.json", respBody)
	if err != nil {
		log.Printf("[%s] vault response: %v", runID, err)
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

	// Write AIR record.
	status := "success"
	if resp.StatusCode >= 400 {
		status = "error"
	}
	writeAIRRecord(cfg.Recorder, runID, span, req.Model, provider, endpoint,
		reqRef, respRef, tokens, start, status, "")

	// Return response to caller with run_id header.
	w.Header().Set("x-run-id", runID)
	w.Header().Set("Content-Type", "application/json")
	for _, h := range []string{"x-request-id", "openai-organization"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	log.Printf("[%s] %s model=%s tokens=%d duration=%dms status=%s",
		runID, endpoint, req.Model, tokens.Total, duration.Milliseconds(), status)
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
