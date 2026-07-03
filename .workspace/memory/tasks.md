# Task Memory

## Current Implementation Stack

- PR #1: project baseline instructions
- PR #2: Go module bootstrap and `.env` config loader
- PR #3: domain model and real LLM provider interface
- PR #4: reducer state transitions
- PR #5: modular monolith and Korean PR conventions
- PR #6: file store and memory queue
- PR #7: LLM dispatcher
- PR #8: session lane
- PR #9: WebSocket gateway
- PR #10: app server runtime wiring
- PR #11: WebSocket event broker
- PR #14: real LLM WebSocket smoke hardening
  - added `orbis ws smoke` for `.env`-configured WebSocket smoke testing
  - terminal runtime events now include `FinalAnswerEmitted`/`RunCompleted` or `LLMCallFailed`/`RunFailed`
  - app runtime uses a per-session event queue to preserve publish and reducer ordering
- current: v0.1 remaining runtime kernel hardening
  - LLM actions dispatch outside the session lane and stream `AssistantDelta`
  - `session.create`, `run.status`, `events.list`, and `run.cancel` are implemented
  - JSONL event replay is available through the store and WebSocket protocol
  - run timeout emits `TimerFired` and closes the run as `FAILED`
  - mock tool calls run through `DispatchToolCall`, `ToolCallStarted`, and `ToolCallSucceeded`
  - `RunStarted` and `RunStatusChanged` are emitted for user-message run start visibility

## v0.1 Status

Completed on 2026-06-27.

Fresh main-branch verification:

- `go test ./...`
- `go test -race ./...`
- `git diff --check`
- real OpenAI `.env` WebSocket smoke reached `RunCompleted`

See `.workspace/memory/history.md` for the v0.1 completion record.

## Post-v0.1 Follow-ups

- collect real usage feedback before adopting OpenClaw/Hermes advanced features
- keep real OpenAI `.env` WebSocket smoke as a release gate
- tool failure retry policy: implemented in v0.2 (see below)

## v0.2: Tool Calling Foundation

Tool calling is now a first-class runtime capability. The LLM only proposes tool
calls; the runtime validates, authorizes, dispatches, executes, observes, and
persists them. Tool failure retry policy (the post-v0.1 follow-up) is implemented
here.

- `internal/tool`: `Tool` interface, registry, toolsets, policy (deny-by-default
  for dangerous), retry policy, idempotency, mock tools.
- Tool Worker (`internal/worker/tool_worker.go`) is the only tool executor:
  policy -> dedup -> `context.WithTimeout` -> persist `data/tool_calls/{key}.json`.
- Reducer derives a stable idempotency key (`runID:tool:toolCallID`), records
  tool-call turns, decides retry vs failure, maps rejection to run failure, and
  splits `TimerFired` into `run_timeout` vs `tool_retry`.
- Dispatcher emits the full tool lifecycle and schedules retry timers without
  auto-failing the run; `context_builder.go` threads conversation context.
- Real LLM tool calling: OpenAI Responses provider sends tool schemas and parses
  `function_call` output. Verify with `orbis ws smoke tool`.
- Specs: `.spec/v0.2-tool-calling.md`, `decisions/v0.2-tool-calling-decisions.md`,
  `docs/tool-calling.md`. Config: `ORBIS_TOOLSETS`, `ORBIS_TOOL_TIMEOUT_*`,
  `ORBIS_TOOL_RETRY_*`, `ORBIS_WS_READ_TIMEOUT`.

## v0.2 Status

Completed on 2026-06-27 (PR #17 merged to `main`).

Fresh main-branch verification:

- `go test ./...`
- `go test -race ./...`
- `git diff --check`
- real OpenAI `.env` WebSocket tool smoke reached `RunCompleted` via a tool call

See `.workspace/memory/history.md` for the v0.2 completion record.

## Post-v0.2 Follow-ups

- v1 skills: skill store/selection/auto-creation, tool search, subagents.
- reconsider continuation-on-denial (feed reason back to LLM) once skills exist.

## v1: Skill System & Context Builder

Skills are reusable procedural knowledge injected into the LLM context before
planning. A skill is not a tool and never executes a side effect. Selection is a
pure, deterministic in-memory computation inside the reducer (no LLM, no disk
I/O); the store is the only skill disk I/O; the dispatcher renders bodies.

- `internal/skill`: `Metadata`/`Entry`, `Index`/`Bodies`, file store
  (load/reload/snapshot/body/list/get), deterministic `Select` (trigger > name >
  tag > related_tool > title, priority/id tiebreak, MaxSelected + MaxChars),
  `BuildContext` (`<orbis_skills>`), event payloads. `domain.SkillRef`.
- Reducer (`ReducerConfig` skill fields): selects once per run, emits
  `SkillSelected`/`SkillLoaded`/`SkillApplied` (or `SkillSkipped`), sets
  `SelectedSkills`, puts refs in `DispatchLLMCall`; reuses on tool-result
  follow-ups. Dispatcher injects bodies into `LLMRequest.Instructions`. Lane
  snapshots `SelectedSkills` into `data/runs/{id}.json`.
- Gateway API (read-only): WS `skill.list`/`skill.get`/`skill.reload`; HTTP
  `GET /skills`, `GET /skills/{skillID}`, `POST /skills/reload` (`gateway.WithSkills`).
  `gateway`/`protocol` do not import `skill`; mapping lives in app.
- `RuntimeService.Close()` graceful shutdown (drains background goroutines) was
  added to remove a pre-existing test-quiescence flake.
- Seed: `data/skills/index.json` + `websocket-runtime-test` (100),
  `tool-calling-policy` (90), `go-reducer-pattern` (80).
- Specs: `.spec/v1-skill-system.md`, `decisions/v1-skill-system-decisions.md`,
  `docs/skills.md`. Config: `ORBIS_SKILLS_ENABLED/DIR/MAX_SELECTED/MAX_CHARS/RELOAD_ON_START`.
- Provider boundary sanitizes tool names to `^[a-zA-Z0-9_-]+$` for the OpenAI
  Responses API and maps `function_call` responses back to the registered name
  (registry/policy/events keep dotted names like `math.add`).
- PRs: #22 (skill pkg), #23 (runtime integration), #24 (quiescence), #25 (gateway
  API), #26 (docs), #27 (Responses API tool-name sanitize).

## v1 Status

Completed on 2026-06-29. Fresh main-branch verification:

- `go test ./...`, `go test -race ./...`, `git diff --check`
- live HTTP skill API smoke (3 seed skills, get/reload/404)
- real-LLM acceptance: `orbis ws smoke skill` and `orbis ws smoke tool` both
  reached `RunCompleted` (the tool smoke via a `math.add` round-trip call)

The earlier real-LLM blocker (OpenAI Responses API rejecting dotted tool names)
was fixed in PR #27 by sanitizing names at the provider boundary with a
`function_call` response round-trip. See `history.md` for the v1 completion
record.

## Post-v1 Follow-ups

- done: real-LLM tool naming fixed in PR #27 (provider-boundary sanitize +
  round-trip); v1 real-LLM acceptance passed on `main`.
- done: `RuntimeService.Close()` wired into server shutdown in v1.5 (PR #29).

## v1.5: Runtime Shutdown, Denial Continuation, Tool-Aware Selection

Three follow-ups on the v1 skill system, each a separate PR (all merged to `main`).

- PR #29 — graceful shutdown: `NewHTTPServer` returns the `RuntimeService`;
  `orbis serve` runs `server.Shutdown` then `RuntimeService.Close()` on
  SIGINT/SIGTERM to drain background session-queue and dispatch goroutines.
- PR #30 — tool-denial continuation: policy-rejected tools no longer fail the run
  by default. The reducer records the denial as a tool result, emits
  `ToolCallDenialContinued`, and dispatches a follow-up LLM call to replan, bounded
  by a per-run `ToolDenialContinuations` counter
  (`ORBIS_TOOL_DENIAL_CONTINUATION_MAX`, default 2; 0 = v0.2 fail-on-denial).
- PR #31 — tool-aware skill selection: enabled tool names
  (`ReducerConfig.ToolNames` -> `SelectionInput.ToolNames`) boost skills whose
  `related_tools` are enabled (`scoreToolAvailable`, reason `tool_available`).

## v1.5 Status

Completed on 2026-07-03 (PRs #29·#30·#31 merged to `main`, `9d2f2d4`).

Fresh main-branch verification: `go test ./...`, `go test -race ./...` (12/12),
`gofmt -l .`, `git diff --check`. Graceful shutdown verified against a real server
(SIGINT -> exit 0). The denial-continuation and tool-aware-selection real-LLM
smokes were deferred (`:8080` externally held; the default safe toolset cannot
induce a policy denial via the real LLM) and are covered by deterministic unit
tests. See `history.md` for the v1.5 completion record.

## Post-v1.5 Follow-ups

- post-hoc real-LLM smoke of denial-continuation / tool-aware selection once
  `:8080` is free.
- v2: auto skill creation, learning loop, vector search, subagents, MCP,
  `reload` auth separation.
