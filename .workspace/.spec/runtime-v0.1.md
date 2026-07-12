# Runtime v0.1

## Status

Accepted — historical baseline (completed 2026-06-27)

## Purpose

Define the first Orbis runtime kernel: a modular monolith where WebSocket requests become runtime events, reducers produce actions, workers execute side effects, and progress streams back over WebSocket.

## Scope

- one Go process
- `.env`-controlled configuration
- real OpenAI-compatible LLM provider from v0.1
- mock provider/tool behavior for tests
- file-based JSONL event logs and JSON snapshots
- session lane ordering
- WebSocket immediate ACK and event streaming

## Non-Goals

- distributed broker
- Kubernetes deployment
- MCP integration
- OpenClaw/Hermes compatibility
- multi-channel adapters
- durable kanban/task board

## Flow

```text
WebSocket request
  -> protocol validation
  -> immediate ACK
  -> domain event
  -> session lane
  -> reducer
  -> actions
  -> dispatcher / worker
  -> result events
  -> session lane
  -> broker
  -> WebSocket subscribers
```

## Testing Requirements

- `go test ./...`
- `go test -race ./...` for concurrency-sensitive slices
- manual WebSocket test against `go run ./cmd/orbis serve`
