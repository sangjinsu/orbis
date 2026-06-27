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

## 2026-06-27: v0.2 Tool Calling Foundation Completed

Status: completed

### Summary

Orbis v0.2 made tool calling a first-class runtime capability on `main` after
PR #17 was merged. The LLM may only propose tool calls; the runtime validates,
authorizes, dispatches, executes, observes, and persists them as events. Only
the Tool Worker executes tools — reducers and WebSocket handlers never do.

### Completed Scope

- New `internal/tool` package: `Tool` interface, registry, toolsets, policy,
  retry policy, idempotency helpers, and mock tools (echo, time.now, math.add,
  mock.fail_once, mock.sleep, mock.dangerous).
- Tool Worker is the only tool executor: policy check -> idempotency dedup ->
  `context.WithTimeout` execution -> persisted record.
- Toolsets gate execution; the default is `safe` only and dangerous tools are
  denied by default.
- Policy runs before execution with ordered reason codes and emits
  `ToolCallRejected` on denial.
- Every tool call carries a stable idempotency key (`runID:tool:toolCallID`);
  a previously succeeded call is deduplicated instead of re-run.
- Tool calls run with a timeout; timeout emits `ToolCallTimedOut`.
- Visible retry: `ToolCallFailed`/`ToolCallTimedOut` -> `ToolCallRetryScheduled`
  -> `ScheduleTimer` -> `TimerFired` -> `ToolCallRetried` -> retry attempt.
- Reducer decides retry vs failure deterministically and splits `TimerFired`
  into `run_timeout` and `tool_retry`.
- Tool call records are inspectable under `data/tool_calls/`.
- Real LLM tool calling: the OpenAI Responses provider advertises tool schemas
  as flattened function definitions, threads conversation context as
  `function_call` / `function_call_output` items, and parses `function_call`
  output.
- WebSocket subscribers receive the full tool lifecycle.
- WebSocket read timeout is now configurable (post-v0.1 hardening), defaulting
  to disabled.

### Verification Evidence

Fresh verification on `main`:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
git diff --check
go run ./cmd/orbis ws smoke tool
```

Tool smoke event sequence:

```text
UserMessageReceived
RunStarted
RunStatusChanged
LLMCallStarted
LLMResponseReceived
ToolCallStarted
ToolCallSucceeded
LLMCallStarted
FinalAnswerEmitted
RunCompleted
```

### Follow-ups

- v1 skills: skill store/selection/auto-creation, tool search, subagents.
- Reconsider continuation-on-denial (feed the reason back to the LLM) once
  skills exist.
- Collect real usage feedback before adopting OpenClaw/Hermes advanced features.
