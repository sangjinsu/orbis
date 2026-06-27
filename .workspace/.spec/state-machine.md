# State Machine

## Status

Draft

## Purpose

Define run state transitions for v0.1.

## Current Implemented Transitions

- `UserMessageReceived` moves a run to `WAITING_LLM` and emits `DispatchLLMCall`.
- `LLMResponseReceived` moves a run to `COMPLETED` and emits `EmitFinalAnswer`.
- `RunCancelled` moves a run to `CANCELLED`.
- Cancelled runs do not emit new side-effect actions.

## Remaining Transitions

- tool call requested
- tool succeeded
- tool failed and retry policy
- timer fired
- run timeout
