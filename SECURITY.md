# Security Policy

## Reporting Vulnerabilities

If you discover a security vulnerability in AIR Blackbox Gateway, please report it responsibly.

**Email:** jason.j.shotwell@gmail.com
**Subject line:** `[SECURITY] AIR Blackbox Gateway — <brief description>`

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

Please **do not** open a public GitHub issue for security vulnerabilities.

## What Data AIR Blackbox Gateway Stores

The gateway records AI system interactions. Here is exactly what is stored and where:

| Data | Where It Goes | Who Controls It |
|---|---|---|
| Raw prompts & completions | **Your** MinIO/S3 vault (never leaves your infrastructure) | You |
| Vault references (URIs, not content) | OTel traces → your collector → your Jaeger/Grafana | You |
| Model name, token counts, timing | OTel span attributes | You |
| Run ID, trace ID, timestamps | `.air.json` record files on your filesystem | You |
| SHA-256 checksums of request/response | `.air.json` record files | You |

## What AIR Blackbox Gateway Does NOT Do

- **No phone-home.** The gateway makes zero network calls except to your configured upstream LLM provider and your own infrastructure (MinIO, OTel Collector).
- **No telemetry.** We do not collect usage data, crash reports, or analytics.
- **No cloud dependency.** Everything runs on your infrastructure. There is no SaaS component.
- **No content in traces.** OTel spans contain vault references, not raw prompts or completions. Your observability stack never sees sensitive content.
- **No credential storage.** API keys are passed through to the upstream provider and are not written to AIR records or vault storage.

## Threat Model

AIR Blackbox Gateway sits in the request path between your AI agent and the LLM provider. The primary security considerations are:

1. **Vault access** — MinIO/S3 credentials control access to stored prompts and completions. Protect these credentials with the same rigor as your LLM API keys.
2. **AIR record files** — These contain vault references and checksums, not raw content. However, metadata (model names, timestamps, token counts) may still be sensitive in some contexts. Apply appropriate filesystem permissions.
3. **Network position** — The gateway terminates your agent's API call and forwards it. It sees the full request and response in transit. Deploy it in the same trust boundary as your agent.

## Supported Versions

| Version | Supported |
|---|---|
| Latest on `main` | Yes |
| Older commits | Best effort |

## Responsible Disclosure

We follow coordinated disclosure. If you report a vulnerability, we will:

1. Acknowledge receipt within 48 hours
2. Provide an estimated timeline for a fix
3. Credit you in the release notes (unless you prefer anonymity)
4. Not pursue legal action against good-faith security researchers
