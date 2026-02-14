#!/usr/bin/env python3
"""
Demo: send a single LLM call through the AIR Blackbox Gateway.

Prerequisites:
    1. docker compose up --build
    2. export OPENAI_API_KEY=sk-...
    3. pip install openai

Usage:
    python examples/demo_agent.py
"""

import os
import sys

try:
    from openai import OpenAI
except ImportError:
    print("Install the OpenAI SDK: pip install openai")
    sys.exit(1)

gateway_url = os.environ.get("GATEWAY_URL", "http://localhost:8080")
api_key = os.environ.get("OPENAI_API_KEY", "")

if not api_key:
    print("Set OPENAI_API_KEY first")
    sys.exit(1)

# Point the OpenAI client at the gateway instead of api.openai.com.
client = OpenAI(base_url=f"{gateway_url}/v1", api_key=api_key)

print("=== AIR Blackbox Gateway Demo ===")
print(f"Gateway: {gateway_url}")
print()

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "system", "content": "You are a concise assistant."},
        {"role": "user", "content": "In one sentence, what is a flight recorder and why do aircraft have them?"},
    ],
    max_tokens=150,
)

print(f"Model:  {response.model}")
print(f"Tokens: {response.usage.total_tokens}")
print(f"Reply:  {response.choices[0].message.content}")
print()
print("Check the x-run-id header in your gateway logs for the AIR record.")
print("View traces at: http://localhost:16686 (Jaeger)")
