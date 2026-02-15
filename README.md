# AIR Blackbox Gateway

[![CI](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-enabled-blueviolet?logo=opentelemetry)](https://opentelemetry.io)

**A flight recorder for AI systems. Every LLM call your agents make passes through this gateway, producing a tamper-evident, replayable audit record — without exposing sensitive content to your observability stack.**

When an autonomous agent sends an email, moves money, or changes data, someone will eventually ask: *"Show me exactly what the AI saw and why it made that decision."* Today, most organizations cannot answer that. AIR Blackbox Gateway is the missing infrastructure — an OpenAI-compatible reverse proxy that records every decision an AI system makes so you can reconstruct incidents, prove compliance, and replay runs deterministically.

> **See it in action:** [Interactive Test Suite Demo](https://nostalgicskinco.github.io/air-blackbox-gateway/test-suite-demo.html) — 30 tests across 8 LLM providers, security validation, and concurrency checks.

## How It Works

```
┌──────────────┐         ┌─────────────────────┐         ┌──────────────┐
│  Your Agent  │────────▶│  AIR Blackbox GW    │────────▶│  OpenAI /    │
│  (any framework)       │                     │         │  Anthropic   │
└──────────────┘         │  • assigns run_id   │         └──────────────┘
                         │  • vaults content   │
                         │  • emits OTel spans │
                         │  • writes AIR record│
                         └─────────┬───────────┘
                                   │
                    ┌──────────────┼──────────────┐
                    ▼              ▼              ▼
              ┌──────────┐  ┌──────────┐  ┌──────────────┐
              │  MinIO   │  │  OTel    │  │  runs/       │
              │  (vault) │  │ Collector│  │  <id>.air.json│
              └──────────┘  └────┬─────┘  └──────────────┘
                                 │
                          ┌──────┴──────┐
                          │ Jaeger /    │
                          │ Grafana     │
                          └─────────────┘
```

1. Your agent sends an OpenAI-compatible request to the gateway (just change the base URL)
2. The gateway assigns a `run_id`, forwards the request, captures the response
3. Raw prompts and completions are vaulted in MinIO (S3-compatible) — traces contain **references**, not content
4. An AIR record file captures the full run: vault refs, model, tokens, timing, tool calls
5. OTel spans flow through the collector pipeline (normalization → vault → redaction → export)
6. Later: `replayctl replay runs/<id>.air.json` replays the run and reports behavioral drift

## Why Not Just Use Langfuse / Helicone / Datadog?

Those are **observability** tools — they answer *"how is the system performing?"*

This is an **accountability** tool — it answers *"what exactly happened and can we prove it?"*

| Capability | Observability Tools | AIR Blackbox Gateway |
|---|---|---|
| Dashboards & latency | ✅ | ❌ (use Jaeger/Grafana) |
| Prompt/response storage | In their cloud | In **your** vault (S3/MinIO) |
| PII-safe traces | ❌ (stores raw content) | ✅ (vault references only) |
| Deterministic replay | ❌ | ✅ (`replayctl`) |
| Tamper-evident records | ❌ | ✅ (SHA-256 checksums) |
| Legal-grade reconstruction | ❌ | ✅ |

## Quick Start (5 minutes)

### Prerequisites

- Docker & Docker Compose
- An OpenAI API key (or any OpenAI-compatible provider)

### 1. Configure

```bash
cp .env.example .env
# Edit .env and add your OPENAI_API_KEY
```

### 2. Start the stack

```bash
docker compose up --build
```

This starts: the gateway (`:8080`), MinIO (`:9000`), an OTel Collector, and Jaeger (`:16686`).

### 3. Make an agent call

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "What is a flight recorder?"}]
  }'
```

The response includes an `x-run-id` header — that's your audit trail.

### 4. View traces

Open Jaeger at [http://localhost:16686](http://localhost:16686) — search for service `air-blackbox-gateway`.

### 5. Replay a run

```bash
# List recorded runs
ls runs/

# Replay and check for drift
go run ./cmd/replayctl replay runs/<run_id>.air.json
```

## Standalone (without Docker)

```bash
go build -o gateway ./cmd/gateway
go build -o replayctl ./cmd/replayctl

# Gateway
export OPENAI_API_KEY=sk-...
export VAULT_ENDPOINT=localhost:9000
export VAULT_ACCESS_KEY=minioadmin
export VAULT_SECRET_KEY=minioadmin
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
./gateway --addr :8080

# Replay
./replayctl replay runs/<run_id>.air.json
```

## AIR Record Format

Each run produces a `.air.json` file — a portable, self-contained audit record:

```json
{
  "version": "1.0.0",
  "run_id": "550e8400-e29b-41d4-a716-446655440000",
  "trace_id": "abc123...",
  "timestamp": "2025-02-14T10:30:00Z",
  "model": "gpt-4o-mini",
  "provider": "openai",
  "endpoint": "/v1/chat/completions",
  "request_vault_ref": "vault://air-runs/550e8400.../request.json",
  "response_vault_ref": "vault://air-runs/550e8400.../response.json",
  "request_checksum": "sha256:a1b2c3...",
  "response_checksum": "sha256:d4e5f6...",
  "tokens": {
    "prompt": 25,
    "completion": 142,
    "total": 167
  },
  "duration_ms": 1230,
  "status": "success"
}
```

## Collector Pipeline

The OTel Collector runs your existing processors in order:

```yaml
processors:
  genai_semantic_normalizer:    # Vendor-agnostic attribute names
  prompt_vault:                  # Offload content → MinIO references
  genai:                         # Redaction, token counting, loop detection
  batch:                         # Standard batching

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [genai_semantic_normalizer, prompt_vault, genai, batch]
      exporters: [otlp/jaeger, debug]
```

## Architecture

AIR Blackbox Gateway is the **spine** of the [nostalgicskinco](https://github.com/nostalgicskinco) GenAI infrastructure portfolio:

| Layer | Repos | Role |
|---|---|---|
| **Gateway** | `air-blackbox-gateway` (this repo) | Proxy + run_id + vault + AIR records |
| **Collector** | `genai-semantic-normalizer`, `prompt-vault-processor`, `opentelemetry-collector-processor-genai`, `genai-cost-slo` | Normalize → vault → redact → metrics |
| **Replay** | `agent-vcr`, `trace-regression-harness` | Record/replay, policy assertions |
| **Governance** | `mcp-policy-gateway`, `mcp-security-scanner`, `agent-tool-sandbox`, `aibom-policy-engine`, `runtime-aibom-emitter` | Tool perimeter, SBOM, security scanning |

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Gateway listen address |
| `PROVIDER_URL` | `https://api.openai.com` | Upstream LLM provider |
| `VAULT_ENDPOINT` | `localhost:9000` | MinIO/S3 endpoint |
| `VAULT_ACCESS_KEY` | `minioadmin` | S3 access key |
| `VAULT_SECRET_KEY` | `minioadmin` | S3 secret key |
| `VAULT_BUCKET` | `air-runs` | S3 bucket for vault storage |
| `VAULT_USE_SSL` | `false` | Use TLS for S3 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTel collector gRPC endpoint |
| `RUNS_DIR` | `./runs` | Directory for AIR record files |

## Project Philosophy

We believe AI systems should be observable and accountable by default. As autonomous agents gain the ability to send emails, move money, and change data, organizations need an open standard for recording and replaying AI system behavior — not another proprietary lock-in.

AIR Blackbox Gateway provides the open recording protocol. The recorder, replay engine, OTel processors, and CLI tools are Apache-2.0 licensed so that any team can adopt them without legal friction. Future governance and compliance services (hosted evidence storage, tamper-proof ledgers, legal hold, regulator exports) may be provided separately as commercial offerings.

**Open protocol → common dependency → operational expectation → compliance requirement.**

That's the path. And it only works if the foundation is genuinely open.

## License

Apache-2.0 — see [LICENSE](LICENSE) for details.

The goal is to create an open standard for recording and replaying AI system behavior. See [COMMERCIAL_LICENSE.md](COMMERCIAL_LICENSE.md) for information about future commercial governance services.
