# Project History

## 2026-06-27: v0.1 Runtime Kernel Completed

Status: completed

### Summary

Orbis v0.1 reached the runtime-kernel milestone on `main` after PR #15 was merged.

The release establishes a modular monolith Go runtime where WebSocket requests become runtime events, session lanes serialize state mutation, reducers produce actions, workers execute side effects, and clients observe progress through WebSocket event streams.

### Completed Scope

- Server starts with `go run ./cmd/orbis serve`.
- WebSocket clients can connect through `GET /ws`.
- `session.message` creates `UserMessageReceived` and starts a run.
- Reducer emits `DispatchLLMCall`, `RunStarted`, and `RunStatusChanged`.
- Real OpenAI-compatible LLM provider works from `.env` settings.
- LLM output streams as `AssistantDelta` before `LLMResponseReceived`.
- Final responses emit `FinalAnswerEmitted` and `RunCompleted`.
- Mock tool-call flow runs through `DispatchToolCall`, `ToolCallStarted`, and `ToolCallSucceeded`.
- `run.cancel` emits `RunCancelled` and cancels active run context.
- Run timeout emits `TimerFired` and terminal `RunFailed`.
- `session.create`, `run.status`, and `events.list` are implemented.
- Events are persisted to JSONL and replayable through `events.list`.
- Session and run snapshots are persisted as JSON.
- WebSocket subscribers receive progress events.

### Verification Evidence

Fresh verification on `main`:

```bash
go test ./...
go test -race ./...
git diff --check
go run ./cmd/orbis serve
go run ./cmd/orbis ws smoke
```

Smoke event sequence:

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

### Follow-ups

- Treat tool failure retry policy as post-v0.1 work unless a release blocker is found.
- Keep OpenClaw/Hermes advanced features out until the kernel has real usage feedback.
