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

## Procedure

1. The LLM proposes a tool call; it never executes the tool itself.
2. The runtime validates the call against the tool policy.
3. A rejected call emits `ToolCallRejected` with an ordered reason code.
4. An allowed call carries a stable idempotency key (`runID:tool:toolCallID`).
5. The Tool Worker deduplicates a previously succeeded call instead of re-running.
6. The Tool Worker executes within a timeout; a timeout emits `ToolCallTimedOut`.
7. Failures emit `ToolCallFailed`, then a visible retry may be scheduled.
8. A success emits `ToolCallSucceeded`, and the result is fed back to the LLM.

## Related Tools

- math.add
- echo
- time.now

## Verification

A tool call is handled correctly when:

- Policy runs before execution.
- The idempotency key is stable across retries.
- Every outcome (succeeded, failed, rejected, timed out) is visible as an event.

## Pitfalls

- Do not execute tools from the reducer or the WebSocket handler.
- Do not fire a tool call without an idempotency key.
- Do not hide tool failures; surface them as events.
