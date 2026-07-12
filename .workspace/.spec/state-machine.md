# State Machine

## Status

Accepted — current behavior through the v2.1 + CLI baseline

## Purpose

Define the current run-state transitions while preserving the runtime
invariants established by the v0.1 and v0.2 milestones.

## Current Implemented Transitions

- `UserMessageReceived` moves a run to `WAITING_LLM`, emits `RunStarted` and
  `RunStatusChanged`, and dispatches `DispatchLLMCall`.
- `LLMResponseReceived` with final text moves a run to `COMPLETED` and emits
  `EmitFinalAnswer`; a response with `tool_call` moves it to `WAITING_TOOL` and
  dispatches `DispatchToolCall`.
- `ToolCallSucceeded` moves a run back to `WAITING_LLM` and dispatches the next
  LLM call.
- `ToolCallFailed` or `ToolCallTimedOut` with a retryable error and attempts
  remaining moves the run to `WAITING_TIMER`, emits `ToolCallRetryScheduled`,
  and schedules a `tool_retry` timer. Exhausted or non-retryable failures move
  the run to `FAILED` and emit `RunFailed`.
- `TimerFired` for `tool_retry` moves the run to `WAITING_TOOL`, emits
  `ToolCallRetried`, and redispatches the same idempotent call at the next
  attempt. A run-timeout timer moves a non-terminal run to `FAILED`.

## Policy-Denial Continuation

`ToolCallRejected` defaults to bounded LLM continuation. While the per-run
budget remains, the reducer records the denial as a tool-result message,
increments `ToolDenialContinuations`, moves to `WAITING_LLM`, emits
`ToolCallDenialContinued`, and dispatches a follow-up LLM call so the model can
replan without the denied tool.

`ORBIS_TOOL_DENIAL_CONTINUATION_MAX` controls the budget and defaults to `2`.
After the budget is exhausted, the run moves to `FAILED` and emits `RunFailed`.
Setting the value to `0` restores the v0.2 fail-on-first-denial behavior.

## Terminal Cancellation Invariant

`RunCancelled` moves the run to `CANCELLED`. Once cancelled, later runtime
events are state no-ops and the reducer emits no new side-effect actions;
workers and retry timers also observe the cancelled `context.Context`.

See `v0.2-tool-calling.md` for the historical tool-calling contract and its
supersession note.
