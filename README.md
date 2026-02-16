# AIR Blackbox Gateway

[![CI](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-enabled-blueviolet?logo=opentelemetry)](https://opentelemetry.io)

**A flight recorder for AI systems. Every LLM call your agents make passes through this gateway, producing a tamper-evident, replayable audit record — without exposing sensitive content to your observability stack.**

When an autonomous agent sends an email, moves money, or changes data, someone will eventually ask: *"Show me exactly what the AI saw and why it made that decision."* Today, most organizations cannot answer that. AIR Blackbox Gateway is the missing infrastructure — an OpenAI-compatible reverse proxy that records every decision an AI system makes so you can reconstruct incidents, prove compliance, and replay runs deterministically.

> **See it in action:** [Interactive Test Suite Demo](https://nostalgicskinco.github.io/air-blackbox-gateway/test-suite-demo.html) — 30 tests across 8 LLM providers, security validation, and concurrency checks.

## Who Is This For?

**ML / Platform Engineers** — You're deploying agents that call LLMs. You need every request and response recorded without leaking PII into your observability stack. Drop this in front of your provider and get vault-backed audit trails with zero code changes.

**Compliance & Security Teams** — Your organization is shipping AI features and regulators are asking questions. You need tamper-evident records that prove exactly what the AI saw, what it decided, and when. AIR records give you legal-grade reconstruction with SHA-256 checksums.

**Startup CTOs** — You're moving fast but know that "we can't prove what our AI did" will eventually become a blocker for enterprise deals, SOC2, or insurance. This is the infrastructure you install now so you're not scrambling later.

## How It Works

<p align="center">
  <img src="docs/architecture.svg" alt="AIR Blackbox Gateway Architecture" width="900"/>
</p>

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

## Operational Guarantees

AIR Blackbox Gateway is a **witness, not a gatekeeper**. It must never become a dependency for uptime.

**Non-blocking** — If the vault (MinIO) is unreachable, the gateway still proxies requests. Your AI system never stops working because recording failed.

**Lossy-safe** — A dropped record is acceptable. A dropped request is not. Recording is best-effort; proxying is guaranteed.

**Self-degrading** — If the OTel Collector is down, spans are dropped silently. If the filesystem is full, AIR records fail gracefully. The gateway logs warnings but never returns errors to your agent.

> **Design principle:** AirBlackBox cannot cause your AI system to fail.

This follows the same contract as Datadog agents, OTel collectors, and other production observability infrastructure. Companies will not insert a system into their AI pipeline if it can break that pipeline.

## Privacy & Data Boundaries

The gateway records AI interactions. Companies will ask: *"Are you storing our customer data?"*

The answer is: **you control all data, and you choose what gets recorded.**

| Recording Mode | What's Stored | Use Case |
|---|---|---|
| **Full vault** (default) | Prompts + completions in your MinIO, references in traces | Complete reconstruction capability |
| **Metadata only** | Model, tokens, timing, run_id — no content | Lightweight audit without content exposure |
| **Hash only** | SHA-256 of request/response, no content stored | Prove *that* a call happened without storing *what* was said |
| **Selective redaction** | Content with PII/PHI fields stripped by the genai processor | Healthcare, fintech, enterprise compliance |

The key insight: *"We can prove what happened without exposing the data."* That's what makes this viable for healthcare, fintech, and enterprise buyers who need accountability but have strict data handling requirements.

See [SECURITY.md](SECURITY.md) for the full data handling policy and threat model.

## What Gets Recorded

An AIR record captures everything needed to reconstruct an incident or replay a run. Beyond the request and response, the gateway captures version and configuration context that matters for root cause analysis:

| Field | Why It Matters |
|---|---|
| `model` | Which model handled this request |
| `provider` | Which provider endpoint was called |
| `timestamp` | When the interaction happened (signed) |
| `run_id` + `trace_id` | Links the record to distributed traces |
| `request_checksum` / `response_checksum` | SHA-256 tamper evidence |
| `vault_ref` | Where the raw content is stored |
| `tokens.prompt` / `tokens.completion` | Token usage for cost and drift analysis |
| `duration_ms` | Latency for performance correlation |
| `status` | Success, error, timeout |

**Why version awareness matters:** Models change silently. OpenAI, Anthropic, and others update weights without notice. When behavior changes and nobody knows why, the first question is: *"Did the model change?"* AIR records let you answer: *"The incident began after the model update at 3:14 PM."*

**Chain of custody:** Every AIR record includes a creation timestamp, checksums, and the identity of the recording system. This isn't just tamper-evidence (proving it wasn't modified) — it's provenance tracking (proving it wasn't mishandled). That's what regulators and lawyers actually verify.

## Roadmap

AIR Blackbox Gateway is building toward becoming an **operational trust layer for AI systems**. Here's what's coming:

| Phase | Layer | Capability | Status |
|---|---|---|---|
| **Now** | Visibility | Recording, replay, vault, OTel pipeline, 8 providers | ✅ Shipped |
| **Now** | Visibility | Non-blocking proxy with streaming, auth, timeout safety | ✅ Shipped |
| **Next** | Visibility | Selective recording modes (metadata-only, hash-only, field redaction) | In progress |
| **Next** | Visibility | Human-readable incident reports (trace data → narrative) | Planned |
| **Next** | Visibility | Backfill ingestion (reconstruct from existing logs) | Planned |
| **Now** | Detection | Runaway agent kill-switch and cost guardrails (`guardrails.yaml`) | ✅ Shipped |
| **Now** | Detection | Loop detection (recursive planner traps, tool retry storms) | ✅ Shipped |
| **Now** | Detection | Token explosion and cost anomaly alerts | ✅ Shipped |
| **Now** | Detection | Slack/webhook alerting with incident narratives | ✅ Shipped |
| **Now** | Prevention | Automatic policy enforcement (block tool, redact data, downgrade model) | ✅ Shipped |
| **Now** | Prevention | Human-in-the-loop approval workflows | ✅ Shipped |
| **Now** | Prevention | Tool allowlists, environment segmentation, PII blocking | ✅ Shipped |
| **v0.6** | Optimization | Cross-agent performance analytics | ✅ Shipped |
| **v0.6** | Optimization | Automatic model routing and prompt recommendations | ✅ Shipped |
| **v0.6** | Optimization | Agent failure taxonomy and pattern library | ✅ Shipped |
| **Future** | Trust Layer | Hosted evidence storage (commercial trust layer) | Roadmap |
| **Future** | Trust Layer | Tamper-proof ledger service | Roadmap |
| **Future** | Trust Layer | Legal hold, retention policies, regulator exports | Roadmap |
| **Future** | Trust Layer | Organization dashboards, access policies | Roadmap |

**The value ladder:** Visibility (what happened) → Detection (something is wrong) → Prevention (stop it automatically) → Optimization (make it better) → Trust Layer (prove it to regulators). Each layer builds on the one below it.

The open-source protocol layer (recording, replay, detection, OTel processors, CLI) will always be Apache-2.0. The commercial trust and governance layers (hosted storage, ledger, compliance reporting, optimization engine) are separate future offerings.

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
