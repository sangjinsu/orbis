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

- `session.create`
- `session.message`
- `session.subscribe`
- `run.cancel`
- `run.status`
- `events.list`

### `session.create`

Creates a session snapshot and emits `SessionCreated`.

```json
{
  "type": "req",
  "id": "create_001",
  "method": "session.create",
  "params": {
    "session_id": "session_001"
  }
}
```

### `run.status`

Returns the latest run snapshot.

```json
{
  "type": "req",
  "id": "status_001",
  "method": "run.status",
  "params": {
    "run_id": "run_req_001"
  }
}
```

### `events.list`

Replays events from JSONL storage.

```json
{
  "type": "req",
  "id": "events_001",
  "method": "events.list",
  "params": {
    "session_id": "session_001",
    "after_seq": 0,
    "limit": 100
  }
}
```

### `run.cancel`

Cancels the active run context and emits `RunCancelled`.

```json
{
  "type": "req",
  "id": "cancel_001",
  "method": "run.cancel",
  "params": {
    "run_id": "run_req_001"
  }
}
```

## Smoke Test

The CLI smoke client uses the configured `.env` address and sends a real
`session.message` request through the WebSocket gateway:

```bash
go run ./cmd/orbis ws smoke
```

Expected event sequence for the successful LLM path:

```text
UserMessageReceived
RunStarted
RunStatusChanged
LLMCallStarted
AssistantDelta
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```

For provider failures, the terminal sequence must include:

```text
RunStarted
RunStatusChanged
LLMCallFailed
RunFailed
```

Tool-call path:

```text
UserMessageReceived
RunStarted
RunStatusChanged
LLMCallStarted
LLMResponseReceived
ToolCallStarted
ToolCallSucceeded
LLMCallStarted
AssistantDelta
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```
