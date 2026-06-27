# Task Memory

## Current Implementation Stack

- PR #1: project baseline instructions
- PR #2: Go module bootstrap and `.env` config loader
- PR #3: domain model and real LLM provider interface
- PR #4: reducer state transitions
- PR #5: modular monolith and Korean PR conventions
- PR #6: file store and memory queue
- PR #7: LLM dispatcher
- PR #8: session lane
- PR #9: WebSocket gateway
- PR #10: app server runtime wiring
- PR #11: WebSocket event broker
- PR #14: real LLM WebSocket smoke hardening
  - added `orbis ws smoke` for `.env`-configured WebSocket smoke testing
  - terminal runtime events now include `FinalAnswerEmitted`/`RunCompleted` or `LLMCallFailed`/`RunFailed`
  - app runtime uses a per-session event queue to preserve publish and reducer ordering
- current: v0.1 remaining runtime kernel hardening
  - LLM actions dispatch outside the session lane and stream `AssistantDelta`
  - `session.create`, `run.status`, `events.list`, and `run.cancel` are implemented
  - JSONL event replay is available through the store and WebSocket protocol
  - run timeout emits `TimerFired` and closes the run as `FAILED`
  - mock tool calls run through `DispatchToolCall`, `ToolCallStarted`, and `ToolCallSucceeded`
  - `RunStarted` and `RunStatusChanged` are emitted for user-message run start visibility

## v0.1 Status

Completed on 2026-06-27.

Fresh main-branch verification:

- `go test ./...`
- `go test -race ./...`
- `git diff --check`
- real OpenAI `.env` WebSocket smoke reached `RunCompleted`

See `.workspace/memory/history.md` for the v0.1 completion record.

## Post-v0.1 Follow-ups

- decide and implement tool failure retry policy
- collect real usage feedback before adopting OpenClaw/Hermes advanced features
