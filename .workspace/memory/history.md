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

### Post-completion Hardening

Follow-on cleanup and stabilization merged to `main` after the completion record:

- PR #19: tidied the v0.2 tool-calling packages — removed the `store -> tool`
  coupling (moved key sanitization into `store`), de-duplicated
  `IsTerminalRunStatus` into `domain`, and extracted a shared tool-event payload
  builder in the dispatcher. Pure refactor, no behavior change.
- PR #20: fixed the flaky `TestRuntimeServicePublishesLLMStartedBeforeProviderCompletes`.
  Because `handleEvent` publishes to the broker before the session lane persists,
  observing `RunCompleted` on the broker did not guarantee the run record was on
  disk, so the test could return while the lane goroutine was still writing to
  `t.TempDir` — racing with `t.TempDir` cleanup (`directory not empty` under
  `-race`). The test now waits for the persisted terminal status, matching the
  sibling cancel/timeout tests. Test-only change; runtime behavior is unchanged.

### Follow-ups

- v1 skills: skill store/selection/auto-creation, tool search, subagents.
- Reconsider continuation-on-denial (feed the reason back to the LLM) once
  skills exist.
- Collect real usage feedback before adopting OpenClaw/Hermes advanced features.

## 2026-06-29: v1 Skill System & Context Builder Completed

Status: completed (all acceptance items met, including real-LLM verification on
`main`; the tool-name blocker was fixed in PR #27)

### Summary

Orbis v1 introduced skills — reusable procedural knowledge loaded into the LLM
context before planning. A skill is not a tool and never executes a side effect.
Selection is a pure, deterministic in-memory computation inside the reducer (no
LLM, no disk I/O); the store is the only skill disk I/O, and the dispatcher
renders selected bodies into the request instructions. Delivered as four PRs plus
one infrastructure fix.

### Completed Scope

- PR #22 — `internal/skill` package (Metadata/Entry, `Index`/`Bodies`, file store
  with load/reload/snapshot/body/list/get, deterministic `Select`,
  `<orbis_skills>` `BuildContext`, event payloads), `domain.SkillRef`, three seed
  skills under `data/skills/`, and `ORBIS_SKILLS_*` config.
- PR #23 — runtime integration: reducer selects once per run from an in-memory
  snapshot, emits `SkillSelected`/`SkillLoaded`/`SkillApplied` (or `SkillSkipped`)
  before `LLMCallStarted`, sets `SelectedSkills`; dispatcher injects bodies into
  `LLMRequest.Instructions`; lane snapshots the run's skills; `orbis ws smoke
  skill`. Disabled → byte-identical to v0.2.
- PR #24 — `RuntimeService.Close()` graceful shutdown draining background
  goroutines, removing a pre-existing test-quiescence flake (`go test ./...`).
- PR #25 — read-only skill gateway API: WS `skill.list`/`skill.get`/`skill.reload`
  and HTTP `GET /skills`, `GET /skills/{skillID}`, `POST /skills/reload`.
- PR #26 — docs (`docs/skills.md`, `.workspace/.spec/v1-skill-system.md`,
  `.workspace/decisions/v1-skill-system-decisions.md`) and this record.
- PR #27 — sanitize tool names for the OpenAI Responses API (`^[a-zA-Z0-9_-]+$`)
  at the provider boundary with a `function_call` response round-trip, unblocking
  all real-LLM runs.

### Verification Evidence

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
git diff --check
```

Live HTTP skill API (real server, seed data): `GET /skills` → 3 skills
(priority 100/90/80), `GET /skills/{id}` → body + content_hash, unknown → 404,
`POST /skills/reload` → `{"count":3}`.

Real-LLM acceptance on `main` (after PR #27) — both smokes reach `RunCompleted`:

`orbis ws smoke skill`:

```text
UserMessageReceived
RunStarted
RunStatusChanged
SkillSelected
SkillLoaded
SkillApplied
LLMCallStarted
AssistantDelta
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```

`orbis ws smoke tool` additionally drove a tool call through the round-trip
(`math_add` on the wire -> mapped back to `math.add` -> `ToolCallStarted` ->
`ToolCallSucceeded` -> `RunCompleted`).

### Blocker found and resolved (PR #27, not a skill issue)

The first `orbis ws smoke skill` run reached `LLMCallStarted` then
`LLMCallFailed` -> `RunFailed`. The OpenAI Responses API rejected the request
with `Invalid 'tools[1].name': ... pattern '^[a-zA-Z0-9_-]+$'`. The cause was
that mock tool names contain a dot (`time.now`, `math.add`, `mock.*`) and
`buildResponsesTools`/`buildResponsesInput` sent them verbatim. The dispatcher
advertises tool schemas on every LLM call, so this blocked all real-LLM runs
(tool and non-tool alike), independent of skills — skill selection, events,
ordering, and instruction injection all ran correctly up to the LLM call.
Fixed in PR #27 by sanitizing names to the pattern at the provider boundary and
mapping `function_call` responses back to the registered name; both smokes then
reached `RunCompleted` on `main`.

### Follow-ups

- [x] Fix real-LLM tool naming — done in PR #27 (provider-boundary sanitize +
  `function_call` response round-trip); `orbis ws smoke skill`/`tool` verified on
  `main`.
- [ ] Wire `RuntimeService.Close()` into HTTP server shutdown.
- [ ] v1.5/v2: auto skill creation, learning loop, vector search, subagents, MCP.
