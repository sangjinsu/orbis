# Tool Calling (v0.2)

Orbis treats tool calling as a runtime capability, not an LLM capability. The
LLM may only *propose* a tool call. The runtime validates, authorizes,
dispatches, executes, observes, and persists every call as events. Reducers and
WebSocket handlers never execute tools — only the Tool Worker does.

## Flow

```
LLMResponseReceived (tool_call proposal)
  -> Reducer            : run -> WAITING_TOOL, emit DispatchToolCall (attempt 1)
  -> Dispatcher         : emit ToolCallStarted, hand off to Tool Worker
  -> Tool Worker        : policy check -> dedup check -> run with timeout -> persist record
  -> ToolCallRejected   : denied tool never executes
       -> budget remains: record a tool-result message, emit ToolCallDenialContinued,
                          dispatch a follow-up LLM call to replan
       -> max is 0 or budget exhausted: record no tool-result message; emit terminal RunFailed
  -> ToolCallSucceeded  : Reducer dispatches the next LLM call
  -> next LLM call       -> FinalAnswerEmitted -> RunCompleted
```

The runtime owns the loop; the LLM is just one worker in it.

## Registry

Tools are registered in an in-memory `Registry` (`internal/tool`). It rejects
duplicate names, returns a clear error for unknown tools, lists tools by enabled
toolset, and exports JSON-Schema tool definitions for the LLM provider. The v0.2
mock tools are: `echo`, `time.now`, `math.add`, `mock.fail_once`, `mock.sleep`,
and `mock.dangerous` (registered but denied by default).

## Toolsets

Each tool belongs to a toolset: `safe`, `read`, `write`, `network`, `runtime`,
`dangerous`. Only enabled toolsets may run. The default is `safe` only
(`ORBIS_TOOLSETS=safe`). Dangerous tools are denied by default and require
explicit opt-in.

## Policy

Before any execution, the policy checks, in order:

1. tool exists (`unknown_tool`)
2. toolset enabled (`toolset_not_allowed`)
3. side-effect level permitted (`side_effect_denied`)
4. idempotency key present when required (`missing_idempotency_key`)
5. approval requirement placeholder (`approval_required`)
6. timeout within max (`timeout_exceeds_max`)
7. args are valid JSON (`invalid_args`)

A denied call emits `ToolCallRejected` with the session, run, tool call id,
tool name, reason code, and message. Denied tools never execute. While the
per-run `ORBIS_TOOL_DENIAL_CONTINUATION_MAX` budget remains (default `2`), the
reducer records the rejection as a tool-result message, emits
`ToolCallDenialContinued`, and dispatches a follow-up LLM call to replan without
the denied tool. With a value of `0`, or after the budget is spent, the reducer
records no tool-result message and transitions directly to terminal
`RunFailed`.

## Idempotency

Every tool call carries a stable idempotency key (`runID:tool:toolCallID`) that
does not change across retries. The Tool Worker persists a record under
`data/tool_calls/{sanitized_key}.json` with status, attempts, result, and error.
If a prior call with the same key already **succeeded**, the tool is not run
again — the worker emits `ToolCallDeduplicated` and re-emits the cached success.
`running` and `failed` records still execute so retries work.

## Timeout

The Tool Worker runs each tool with `context.WithTimeout` using the call's
timeout, the tool's metadata timeout, or `ORBIS_TOOL_TIMEOUT_DEFAULT`. On
timeout it cancels execution and emits `ToolCallTimedOut` (reason `timeout`)
without blocking the session lane. `mock.sleep` demonstrates this.

## Retry

Retries are visible as events. On a retryable failure with attempts remaining,
the reducer moves the run to `WAITING_TIMER`, emits `ToolCallRetryScheduled`, and
schedules a backoff timer. When it fires, the reducer emits `ToolCallRetried` and
re-dispatches the tool with `attempt+1` and the same idempotency key. Retry is
configurable: `ORBIS_TOOL_RETRY_MAX_ATTEMPTS`, `ORBIS_TOOL_RETRY_INITIAL_DELAY`,
`ORBIS_TOOL_RETRY_MAX_DELAY`, `ORBIS_TOOL_RETRY_BACKOFF`. `mock.fail_once`
demonstrates a retry that fails once then succeeds.

## WebSocket Event Sequences

Successful tool call:

```
UserMessageReceived
RunStarted
LLMCallStarted
LLMResponseReceived
ToolCallStarted
ToolCallSucceeded
LLMCallStarted
FinalAnswerEmitted
RunCompleted
```

Retry:

```
ToolCallStarted
ToolCallFailed
ToolCallRetryScheduled
TimerFired
ToolCallRetried
ToolCallStarted
ToolCallSucceeded
```

Policy denial with continuation budget remaining:

```
ToolCallStarted
ToolCallRejected
ToolCallDenialContinued
LLMCallStarted
LLMResponseReceived
```

Policy denial with a zero or exhausted continuation budget:

```
ToolCallStarted
ToolCallRejected
RunFailed
```

## Real LLM Tool Calling

The OpenAI Responses provider sends the enabled tool schemas as flattened
function definitions, rebuilds the conversation (including `function_call` and
`function_call_output` items) so the model has full context, and parses
`function_call` output into a proposed tool call.

## Manual Test

```bash
go run ./cmd/orbis serve            # requires .env with a real LLM provider
go run ./cmd/orbis ws smoke tool    # drives a tool-calling prompt via the real LLM
```

Or connect with `wscat -c ws://localhost:8080/ws` and send a `session.message`
whose text induces a tool, e.g. "Use the math.add tool to add 1 and 2." Inspect
persisted records under `data/tool_calls/`.
