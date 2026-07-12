# Skill: tool-calling-policy

## Purpose

Explain how Orbis tool calls flow through LLM proposal, runtime policy, worker
execution, idempotency, timeout, retry, and result events.

## When to Use

Use this skill when the user wants to call a tool, or asks how tool calls should
be proposed, validated, retried, and observed.

## Required Context

- Available tool schemas and their toolsets
- The tool policy (allowed toolsets, timeout bounds)
- The idempotency key for the call
- Retry policy (max attempts, backoff)
- Denial-continuation budget (`ORBIS_TOOL_DENIAL_CONTINUATION_MAX`, default 2)

## Procedure

1. The LLM proposes a tool call; it never executes the tool itself.
2. The runtime validates the call against the tool policy.
3. A rejected call emits `ToolCallRejected` with an ordered reason code; the
   rejected tool never executes.
4. While the per-run denial budget remains, the reducer records the rejection
   as a tool-result message, emits `ToolCallDenialContinued`, and dispatches a
   follow-up LLM call so the LLM can replan with the denial reason.
5. With a zero or exhausted budget, record no tool-result message and emit
   terminal `RunFailed`.
6. An allowed call carries a stable idempotency key (`runID:tool:toolCallID`).
7. The Tool Worker deduplicates a previously succeeded call instead of re-running.
8. The Tool Worker executes within a timeout; a timeout emits `ToolCallTimedOut`.
9. Failures emit `ToolCallFailed`, then a visible retry may be scheduled.
10. A success emits `ToolCallSucceeded`, and the result is fed back to the LLM.

## Related Tools

- math.add
- echo
- time.now

## Verification

A tool call is handled correctly when:

- Policy runs before execution.
- Rejected tools never execute, and denial continuation is bounded per run.
- Reducers and WebSocket handlers never execute tools.
- The idempotency key is stable across retries.
- Every outcome (succeeded, failed, rejected, denial-continued, timed out) is
  visible as an event.

## Pitfalls

- Do not execute tools from the reducer or the WebSocket handler.
- Do not assume every policy denial is immediately terminal; honor the bounded
  continuation budget and return the denial to the LLM when budget remains.
- Do not fire a tool call without an idempotency key.
- Do not hide tool failures; surface them as events.
