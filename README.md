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

Required local settings:

```text
ORBIS_ADDR=:8080
ORBIS_DATA_DIR=data
ORBIS_LLM_PROVIDER=openai
ORBIS_LLM_MODEL=<model>
ORBIS_RUN_TIMEOUT=2m
OPENAI_API_KEY=<api-key>
OPENAI_BASE_URL=https://api.openai.com
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

## WebSocket Methods

Connect to `ws://localhost:8080/ws` and send request envelopes:

```json
{"type":"req","id":"create_1","method":"session.create","params":{"session_id":"session_1"}}
```

```json
{"type":"req","id":"msg_1","method":"session.message","params":{"session_id":"session_1","text":"안녕"}}
```

```json
{"type":"req","id":"status_1","method":"run.status","params":{"run_id":"run_msg_1"}}
```

```json
{"type":"req","id":"events_1","method":"events.list","params":{"session_id":"session_1","after_seq":0,"limit":100}}
```

```json
{"type":"req","id":"cancel_1","method":"run.cancel","params":{"run_id":"run_msg_1"}}
```

## Development

```bash
make test
make run
make smoke
```
