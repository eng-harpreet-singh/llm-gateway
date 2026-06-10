cat > README.md << 'EOF'
# llm-gateway

A high-performance, multi-provider LLM gateway written in Go.

## Why Go?

Most LLM gateways (LiteLLM and similar) are written in Python. Under high
concurrency, Python gateways add hundreds of microseconds to milliseconds
of tail latency — and in agent workflows, where one user action triggers
several LLM calls, that cost compounds fast.

This gateway is built in Go for **predictable p99 latency under load**,
benchmarked head-to-head against LiteLLM (see `/benchmarks`).

## Features

- [ ] Multi-provider routing (OpenAI, Anthropic, Ollama)
- [ ] Smart complexity-based routing (cheap model first, escalate on signal)
- [ ] Per-tenant rate limiting (tokens/min + requests/min, Redis-backed)
- [ ] Streaming SSE proxy (pass-through provider streaming events)
- [ ] Cost attribution per tenant (Postgres ledger)
- [ ] OpenTelemetry tracing, with cost as a span attribute
- [ ] Grafana dashboard (req/s, p50/p95/p99, cost per tenant, provider mix)
- [ ] Load-test report (k6, benchmarked vs LiteLLM)

## Architecture

(diagram coming — see /docs)

## Status

🚧 Active development. Built as a portfolio project demonstrating
production-grade AI infrastructure engineering in Go.

## Getting started

```bash
cp .env.example .env   # add your API keys
go run ./cmd/gateway
curl localhost:8080/healthz
```
EOF