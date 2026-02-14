#!/usr/bin/env bash
##
## Demo: send a single LLM call through the AIR Blackbox Gateway.
##
## Prerequisites:
##   1. docker compose up --build
##   2. export OPENAI_API_KEY=sk-...
##
## Usage:
##   ./examples/demo_agent.sh
##

set -euo pipefail

GATEWAY="${GATEWAY_URL:-http://localhost:8080}"

echo "=== AIR Blackbox Gateway Demo ==="
echo "Gateway: $GATEWAY"
echo ""

# Send a chat completion through the gateway.
RESPONSE=$(curl -s -i "$GATEWAY/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${OPENAI_API_KEY}" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "system", "content": "You are a concise assistant."},
      {"role": "user", "content": "In one sentence, what is a flight recorder and why do aircraft have them?"}
    ],
    "max_tokens": 150
  }')

# Extract the run ID from response headers.
RUN_ID=$(echo "$RESPONSE" | grep -i "x-run-id" | awk '{print $2}' | tr -d '\r')

echo "Response headers:"
echo "$RESPONSE" | sed '/^\r$/q'
echo ""

echo "Response body:"
echo "$RESPONSE" | sed '1,/^\r$/d'
echo ""

if [ -n "$RUN_ID" ]; then
  echo ""
  echo "=== Run recorded ==="
  echo "Run ID: $RUN_ID"
  echo "AIR record: runs/${RUN_ID}.air.json"
  echo ""
  echo "Next steps:"
  echo "  1. View traces: open http://localhost:16686 (Jaeger)"
  echo "  2. View vault:  open http://localhost:9001 (MinIO Console)"
  echo "  3. Replay run:  go run ./cmd/replayctl replay runs/${RUN_ID}.air.json"
else
  echo "WARNING: no x-run-id header found — is the gateway running?"
fi
