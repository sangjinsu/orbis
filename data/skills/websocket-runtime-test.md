# Skill: websocket-runtime-test

## Purpose

Test the Orbis runtime through the WebSocket request/response/event stream.

## When to Use

Use this skill when the user wants to verify runtime communication, LLM
streaming, or session event flow over WebSocket.

## Required Context

- Server address
- Session ID
- WebSocket endpoint
- Expected event sequence

## Procedure

1. Connect to `/ws`.
2. Send `session.message`.
3. Confirm the immediate ACK.
4. Watch the runtime events.
5. Confirm `LLMCallStarted`.
6. Confirm `AssistantDelta` or `FinalAnswerEmitted`.
7. Confirm `RunCompleted`.

## Related Tools

- session.message
- run.status
- events.list

## Verification

The test succeeds only when:

- The ACK is immediate.
- Runtime events are streamed.
- The final run status is `COMPLETED`.

## Pitfalls

- Do not block the WebSocket handler while waiting for the LLM.
- Do not call the LLM directly from the WebSocket handler.
