# WebSocket Protocol

## Status

Draft

## Purpose

Define the minimal request, response, and event envelopes for v0.1 runtime testing.

## Client Request

```json
{
  "type": "req",
  "id": "req_001",
  "method": "session.message",
  "params": {
    "session_id": "session_001",
    "text": "안녕"
  }
}
```

## Server ACK

```json
{
  "type": "res",
  "id": "req_001",
  "ok": true,
  "payload": {
    "session_id": "session_001",
    "run_id": "run_req_001"
  }
}
```

## Runtime Event

```json
{
  "type": "event",
  "event": "LLMResponseReceived",
  "seq": 2,
  "session_id": "session_001",
  "run_id": "run_req_001",
  "payload": {}
}
```

## Implemented Methods

- `session.message`
- `session.subscribe`

## Smoke Test

The CLI smoke client uses the configured `.env` address and sends a real
`session.message` request through the WebSocket gateway:

```bash
go run ./cmd/orbis ws smoke
```

Expected event sequence for the successful LLM path:

```text
UserMessageReceived
LLMCallStarted
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```

For provider failures, the terminal sequence must include:

```text
LLMCallFailed
RunFailed
```

## Remaining Methods

- `session.create`
- `run.cancel`
- `run.status`
- `events.list`
