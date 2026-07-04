# Skill: runtime-debug

## Purpose

Debug Orbis runtime behavior through events, run state, logs, and the debug
webview.

## When to Use

Use this skill when the user reports a failed run, missing event, broken
WebSocket stream, unexpected `/debug` behavior, or asks about runtime event flow.

## Required Context

- Session ID and run ID, if available
- The WebSocket request that triggered the run
- Observed event sequence
- Relevant server logs or trace records
- Whether `/debug` and `/ws` are reachable

## Procedure

1. Confirm the server is healthy through `/healthz` or `/readyz`.
2. Reproduce with the smallest `session.message` request.
3. Check that an immediate ACK is returned before runtime processing finishes.
4. Inspect streamed events for the last successful transition.
5. Compare the observed sequence with the expected reducer and worker flow.
6. Use `/debug` to confirm prompt, ACK, event stream, final answer, and status.
7. Treat missing worker results as events that should be visible, not hidden errors.

## Related Tools

- session.message
- run.status
- events.list

## Verification

Runtime debugging is complete when:

- The failing transition or missing event is identified.
- The run ends in `COMPLETED`, `FAILED`, or `CANCELLED`.
- Any fix is verified through WebSocket events, not just logs.

## Pitfalls

- Do not bypass the runtime by calling workers directly from handlers.
- Do not debug only the final answer; inspect the event stream.
- Do not ignore cancellation or timeout context.
