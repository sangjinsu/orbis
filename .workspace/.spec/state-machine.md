# State Machine

## Status

Draft

## Purpose

Define run state transitions for v0.1.

## Current Implemented Transitions

- `UserMessageReceived` moves a run to `WAITING_LLM` and emits `DispatchLLMCall`.
- `UserMessageReceived` also emits `RunStarted` and `RunStatusChanged`.
- `LLMResponseReceived` with final text moves a run to `COMPLETED` and emits `EmitFinalAnswer`.
- `LLMResponseReceived` with `tool_call` moves a run to `WAITING_TOOL` and emits `DispatchToolCall`.
- `ToolCallSucceeded` moves a run back to `WAITING_LLM` and emits `DispatchLLMCall`.
- `RunCancelled` moves a run to `CANCELLED`.
- Cancelled runs do not emit new side-effect actions.

## Tool Calling Transitions (v0.2)

- `ToolCallRejected` moves a run to `FAILED` and emits `RunFailed` (policy denial).
- `ToolCallFailed`/`ToolCallTimedOut` with a retryable error and remaining
  attempts move a run to `WAITING_TIMER`, emit `ToolCallRetryScheduled`, and emit
  a `ScheduleTimer` action with `kind=tool_retry`.
- `ToolCallFailed`/`ToolCallTimedOut` with no remaining attempts (or a
  non-retryable error) move a run to `FAILED` and emit `RunFailed`.
- `TimerFired` with `kind=tool_retry` moves a run to `WAITING_TOOL`, emits
  `ToolCallRetried`, and re-emits `DispatchToolCall` with `attempt+1` and the
  same idempotency key.
- `TimerFired` with `kind=run_timeout` (or no kind) moves a non-terminal run to
  `FAILED` and emits `RunFailed`.

See `v0.2-tool-calling.md` for the full tool-calling specification.
