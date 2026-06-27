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
- `ToolCallFailed` moves a run to `FAILED`.
- `TimerFired` moves a non-terminal run to `FAILED` and emits `RunFailed`.
- `RunCancelled` moves a run to `CANCELLED`.
- Cancelled runs do not emit new side-effect actions.

## Remaining Transitions

- retry policy after tool failure
