# WebSocket Protocol

## Status

Accepted — current behavior through the v2.1 + CLI baseline

## Purpose

Define the implemented request, response, and event envelopes used by the
current runtime, operational CLI, and interactive chat surfaces.

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
- `skill.list`
- `skill.get`
- `skill.reload`
- `skill.proposal.list`
- `skill.proposal.get`
- `skill.proposal.create_from_run`
- `skill.proposal.update`
- `skill.proposal.approve`
- `skill.proposal.reject`

Feature-specific payloads and authorization rules remain defined in
`v1-skill-system.md`, `v2-skill-learning-loop.md`, and
`v2.1-learning-loop-hardening.md`; they are not duplicated here.

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

### `session.subscribe`

Streams runtime events. With a `session_id` it delivers that session's
sequenced events. With `"scope": "global"` (v2.1) it delivers the
session-independent feed instead: skill-learning lifecycle events, including
the standalone reload's `SkillIndexReloadRequested`/`SkillIndexReloaded`.
Global-only events carry no `session_id` and `seq` 0 — the feed is live and
non-persisted; missed events are recovered through the read APIs. Any other
scope value is an error.

```json
{
  "type": "req",
  "id": "sub_001",
  "method": "session.subscribe",
  "params": { "scope": "global" }
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
