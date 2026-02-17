# AIR Blackbox Gateway

[![CI](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/nostalgicskinco/air-blackbox-gateway/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-enabled-blueviolet?logo=opentelemetry)](https://opentelemetry.io)
[![Python SDK](https://img.shields.io/badge/SDK-Python-3776AB?logo=python&logoColor=white)](https://github.com/nostalgicskinco/air-sdk-python)

**Your AI agent just sent an email, moved money, or changed production data. Someone asks: *"Show me exactly what it saw and why it made that decision."***

**Can you answer that today?**

AIR Blackbox Gateway is a flight recorder for AI systems. Drop it in front of any OpenAI-compatible provider and every LLM call produces a tamper-evident, replayable audit record — without exposing sensitive content to your observability stack.

```python
# Add one line. Every AI decision is now recorded.
from openai import OpenAI
import air

client = air.air_wrap(OpenAI())
```

15 repos. 200+ tests. CI on every push. Apache-2.0.

> **See it live:** [Interactive Test Suite](https://nostalgicskinco.github.io/air-blackbox-gateway/test-suite-demo.html) — 30 tests across 8 LLM providers.

---

## Get Started in 5 Minutes

**1. Start the stack**

```bash
git clone https://github.com/nostalgicskinco/air-blackbox-gateway.git
cd air-blackbox-gateway
cp .env.example .env   # add your OPENAI_API_KEY
docker compose up --build
```

**2. Install the SDK**

```bash
pip install air-blackbox-sdk
```

**3. Record everything**

```python
from openai import OpenAI
import air

client = air.air_wrap(OpenAI())

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "What is a flight recorder?"}],
)
# x-run-id header = your audit trail
# Prompts vaulted in your MinIO, not a third-party cloud
# HMAC-SHA256 chain = tamper-proof record
```

**Works with your framework:**

```python
# LangChain
from air.integrations.langchain import air_langchain_llm
llm = air_langchain_llm("gpt-4o-mini")

# CrewAI
from air.integrations.crewai import air_crewai_llm
llm = air_crewai_llm("gpt-4o-mini")
```

**4. View traces** → [localhost:16686](http://localhost:16686) (Jaeger)

**5. Replay any run**

```bash
go run ./cmd/replayctl replay runs/<run_id>.air.json
```

---

## Why This Exists

Langfuse, Helicone, and Datadog answer *"how is the system performing?"*

AIR answers **"what exactly happened, and can we prove it?"**

| | Observability Tools | AIR Blackbox Gateway |
|---|---|---|
| Dashboards & latency | ✅ | ❌ (use Jaeger/Grafana) |
| Where data lives | Their cloud | **Your** vault (S3/MinIO) |
| PII in traces | ❌ Raw content exposed | ✅ Vault references only |
| Tamper-evident records | ❌ | ✅ SHA-256 + HMAC chain |
| Deterministic replay | ❌ | ✅ `replayctl` |
| Compliance reporting | ❌ | ✅ 22 controls (SOC 2 + ISO 27001) |
| Signed evidence export | ❌ | ✅ HMAC-attested packages |
| Agent guardrails | ❌ | ✅ Cost, loop, tool, PII |

Nobody else ships tamper-evident audit chains for AI systems as open source. Not Langfuse (6k+ stars), not Helicone, not LangSmith. They're observability. This is accountability.

---

## Who This Is For

**Platform engineers** deploying agents that call LLMs. You need every request recorded without leaking PII into your observability stack. Drop this in front of your provider — zero code changes.

**Compliance teams** whose regulators are asking *"show me what the AI did."* AIR records give you legal-grade reconstruction with SHA-256 checksums and signed evidence packages.

**Startup CTOs** who know *"we can't prove what our AI did"* will block enterprise deals, SOC 2, or insurance. Install this now so you're not scrambling later.

**Agent builders** moving beyond chatbots toward systems that operate across hours, call tools, and interact with production data. You need decision provenance, replay, and the ability to prove your agent did the right thing — or a clear record of where it didn't.

---

## How It Works

<p align="center">
  <img src="docs/architecture.svg" alt="AIR Blackbox Gateway Architecture" width="900"/>
</p>

1. Your agent sends an OpenAI-compatible request to the gateway (just change the base URL)
2. The gateway assigns a `run_id`, forwards the request, captures the response
3. Prompts and completions are vaulted in MinIO (S3-compatible) — traces contain **references**, not content
4. An `.air.json` record captures the full run: vault refs, model, tokens, timing, tool calls
5. OTel spans flow through the collector pipeline (normalize → vault → redact → export)
6. Later: `replayctl` replays the run and reports behavioral drift

---

## The Trust Layer

This is the part nobody else has.

**Audit Chain** — Every proxied request is appended to an HMAC-SHA256 chain. Each entry links to the previous entry's hash. Modify any record and the chain breaks from that point forward. Same integrity model as certificate transparency logs, without the blockchain overhead.

**Compliance Reporting** — The gateway evaluates your live configuration against 22 controls across SOC 2 (12 controls) and ISO 27001 (10 controls). Controls pass or fail based on what's actually enabled — vault, guardrails, analytics, audit chain. No self-assessment forms. The gateway evaluates itself.

**Evidence Export** — `GET /v1/audit/export` generates a signed evidence package: full audit chain, compliance report, time range, HMAC attestation. Hand it to your auditor as a single JSON document. The attestation can be independently verified against your signing key.

| Endpoint | Method | Description |
|---|---|---|
| `/v1/audit` | GET | Chain integrity + live compliance evaluation |
| `/v1/audit/export` | GET | Signed evidence package for regulators |

---

## Operational Guarantees

AIR is a **witness, not a gatekeeper**. It cannot cause your AI system to fail.

**Non-blocking** — Vault unreachable? Gateway still proxies. Your AI never stops because recording failed.

**Lossy-safe** — A dropped record is acceptable. A dropped request is not. Recording is best-effort; proxying is guaranteed.

**Self-degrading** — OTel Collector down? Spans dropped silently. Filesystem full? AIR records fail gracefully. Warnings logged, never errors returned.

> Same contract as Datadog agents, OTel collectors, and every other production observability tool. Companies won't insert infrastructure that can break their pipeline.

---

## Privacy & Data Boundaries

You control all data. You choose what gets recorded.

| Mode | What's Stored | Use Case |
|---|---|---|
| **Full vault** (default) | Prompts + completions in your MinIO | Complete reconstruction |
| **Metadata only** | Model, tokens, timing, run_id | Lightweight audit, no content |
| **Hash only** | SHA-256 of request/response | Prove a call happened without storing what was said |
| **Selective redaction** | Content with PII/PHI stripped | Healthcare, fintech, enterprise |

*"We can prove what happened without exposing the data."* That's what makes this viable for regulated industries.

---

## The Ecosystem

15 repos, all tested, all with CI/CD, all Apache-2.0.

| Layer | Repos | What It Does |
|---|---|---|
| **Gateway** | `air-blackbox-gateway` (this repo) | Proxy + vault + AIR records + guardrails + trust |
| **SDK** | [`air-sdk-python`](https://github.com/nostalgicskinco/air-sdk-python) | Python integrations — OpenAI, LangChain, CrewAI |
| **Episode Ledger** | [`agent-episode-store`](https://github.com/nostalgicskinco/agent-episode-store) | Groups AIR records into replayable task-level episodes |
| **Eval Harness** | [`eval-harness`](https://github.com/nostalgicskinco/eval-harness) | Replays episodes, scores results, detects regressions |
| **Policy Engine** | [`agent-policy-engine`](https://github.com/nostalgicskinco/agent-policy-engine) | Risk-tiered autonomy, runtime enforcement |
| **Collector** | [`genai-semantic-normalizer`](https://github.com/nostalgicskinco/genai-semantic-normalizer), [`prompt-vault-processor`](https://github.com/nostalgicskinco/prompt-vault-processor), [`otel-processor-genai`](https://github.com/nostalgicskinco/opentelemetry-collector-processor-genai) | Normalize → vault → redact → metrics |
| **Platform** | [`air-platform`](https://github.com/nostalgicskinco/air-platform) | Docker Compose orchestration + integration tests |
| **Replay** | [`agent-vcr`](https://github.com/nostalgicskinco/agent-vcr), [`trace-regression-harness`](https://github.com/nostalgicskinco/trace-regression-harness) | Record/replay agent runs, policy assertions on traces |
| **Governance** | [`mcp-policy-gateway`](https://github.com/nostalgicskinco/mcp-policy-gateway), [`mcp-security-scanner`](https://github.com/nostalgicskinco/mcp-security-scanner), [`agent-tool-sandbox`](https://github.com/nostalgicskinco/agent-tool-sandbox), [`aibom-policy-engine`](https://github.com/nostalgicskinco/aibom-policy-engine), [`runtime-aibom-emitter`](https://github.com/nostalgicskinco/runtime-aibom-emitter) | Tool firewall, security scanning, sandboxing, AI bill of materials |
| **Trust** | `pkg/trust` (this repo) | HMAC audit chain, SOC 2 + ISO 27001 compliance, evidence export |

---

## The Value Ladder

```
Visibility (what happened)
  → Detection (something is wrong)
    → Prevention (stop it automatically)
      → Optimization (make it better)
        → Trust (prove it to regulators)
          → Autonomy (let the agent act, safely)
```

Each layer builds on the one below. You can't detect what you can't see. You can't prevent what you can't detect. You can't trust what you can't prove. And you can't grant autonomy without trust.

---

## What's Shipped

| Version | Capability | Status |
|---|---|---|
| v0.1 | Recording, replay, vault, OTel pipeline, 8 providers | ✅ |
| v0.1 | Non-blocking proxy with streaming, auth, timeout safety | ✅ |
| v0.4 | Runaway agent kill-switch, cost guardrails, loop detection | ✅ |
| v0.5 | Policy enforcement, PII blocking, tool allowlists, HITL approval | ✅ |
| v0.6 | Cross-agent analytics, model routing, failure taxonomy | ✅ |
| v0.7 | HMAC-SHA256 audit chain, SOC 2 + ISO 27001 reporting, evidence export | ✅ |
| v0.8 | Python SDK (OpenAI, LangChain, CrewAI), CI/CD across all repos | ✅ |

## What's Next

| Phase | Timeline | Focus |
|---|---|---|
| Foundation | Q1–Q2 2026 | Episode model, durable state, pause/resume |
| Risk-Tiered Autonomy | Q3–Q4 2026 | Cost-of-error gating, approval workflows, sandbox replay |
| Multi-Agent Orchestration | Q1–Q2 2027 | Planner/executor/critic, escalation policies, shared state |
| External Trust Wedge | Q3–Q4 2027 | Trust layer as add-on, onboarding templates, incident runbooks |
| Enterprise Scale | 2028–2029 | Multi-tenant isolation, provenance search, SCIM provisioning |

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Gateway listen address |
| `PROVIDER_URL` | `https://api.openai.com` | Upstream LLM provider |
| `VAULT_ENDPOINT` | `localhost:9000` | MinIO/S3 endpoint |
| `VAULT_ACCESS_KEY` | `minioadmin` | S3 access key |
| `VAULT_SECRET_KEY` | `minioadmin` | S3 secret key |
| `VAULT_BUCKET` | `air-runs` | S3 bucket name |
| `VAULT_USE_SSL` | `false` | TLS for S3 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTel collector gRPC |
| `RUNS_DIR` | `./runs` | AIR record directory |
| `TRUST_SIGNING_KEY` | *(none)* | HMAC-SHA256 signing key |

## AIR Record Format

Each run produces a `.air.json` file:

```json
{
  "version": "1.0.0",
  "run_id": "550e8400-e29b-41d4-a716-446655440000",
  "trace_id": "abc123...",
  "timestamp": "2025-02-14T10:30:00Z",
  "model": "gpt-4o-mini",
  "provider": "openai",
  "request_vault_ref": "vault://air-runs/550e8400.../request.json",
  "response_vault_ref": "vault://air-runs/550e8400.../response.json",
  "request_checksum": "sha256:a1b2c3...",
  "response_checksum": "sha256:d4e5f6...",
  "tokens": { "prompt": 25, "completion": 142, "total": 167 },
  "duration_ms": 1230,
  "status": "success"
}
```

## License

Apache-2.0. The open-source protocol layer will always be Apache-2.0.

The path to adoption: **Open protocol → common dependency → operational expectation → compliance requirement.**

See [LICENSE](LICENSE) for details. See [COMMERCIAL_LICENSE.md](COMMERCIAL_LICENSE.md) for future commercial governance services.
