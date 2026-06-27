# Orbis Agent Runtime

Orbis is a Go-based, event-loop-first runtime for long-running AI agents.

The runtime owns session state, event ordering, action dispatch, cancellation,
observability, and WebSocket progress streaming. The LLM is a worker inside the
loop, not the loop controller.

## Quick Start

Configure runtime settings through `.env`:

```bash
cp .env.example .env
```

Start the server:

```bash
go run ./cmd/orbis serve
```

Run the WebSocket smoke client against the configured `ORBIS_ADDR`:

```bash
go run ./cmd/orbis ws smoke
```

The smoke client sends a `session.message` request, prints ACK/event names, and
exits successfully only after `RunCompleted`.

## Development

```bash
make test
make run
make smoke
```
