# Changelog

All notable changes to **AIR Blackbox Gateway** will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] — 2026-02-22

- Initial release of the AIR Blackbox Gateway
- OpenAI-compatible reverse proxy with full request/response capture
- HMAC-SHA256 tamper-evident audit chain
- OpenTelemetry trace emission for every LLM call
- Prompt vault integration with MinIO for content offloading
- Docker Compose stack with Jaeger, MinIO, and OTel Collector
- Golden fixture test suite for multi-provider detection
- Interactive demo pages (air-demo.html, test-suite-demo.html)
- Dockerfile for container deployment
- GitHub Container Registry publishing via CI
