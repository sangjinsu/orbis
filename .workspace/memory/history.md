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
- [x] Wire `RuntimeService.Close()` into HTTP server shutdown — done in v1.5 (PR #29).
- [ ] v2: auto skill creation, learning loop, vector search, subagents, MCP.

## 2026-07-03: Orbis v1.5 Completed

Status: completed (merged to `main`; unit/race verified)

### Summary

v1.5 layered three follow-ups onto the v1 skill system, each as its own PR:
runtime graceful shutdown, agentic continuation after a tool-policy denial, and
tool-aware skill selection.

### Completed Scope

- PR #29 — graceful shutdown: `NewHTTPServer` returns the `RuntimeService`, and
  `orbis serve` handles SIGINT/SIGTERM by `server.Shutdown` then
  `RuntimeService.Close()`, which drains in-flight session-queue and dispatch
  goroutines. Verified live: SIGINT -> "orbis server shutting down" -> exit 0.
- PR #30 — tool-denial continuation: a policy-rejected tool no longer fails the
  run by default. The reducer records the denial as a tool result, emits
  `ToolCallDenialContinued`, and dispatches a follow-up LLM call to replan, bounded
  by a per-run `ToolDenialContinuations` counter
  (`ORBIS_TOOL_DENIAL_CONTINUATION_MAX`, default 2; 0 restores v0.2 fail-on-denial).
- PR #31 — tool-aware skill selection: `SelectionInput.ToolNames` (previously
  reserved) now boosts skills whose `related_tools` are enabled for the run
  (`scoreToolAvailable`, reason `tool_available`); the server passes the enabled
  tool schema names into `ReducerConfig.ToolNames`.

### Verification Evidence

Fresh main-branch verification (`9d2f2d4`):

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...    # 12/12 packages pass
git diff --check
```

The graceful-shutdown path was verified against a real server (SIGINT -> exit 0).
The denial-continuation and tool-aware-selection paths are deterministic and are
verified by unit tests; their real-LLM smokes were skipped this session because
`:8080` was held by an external process, and (for continuation) the default safe
toolset cannot induce a policy denial via the real LLM. The normal skill/tool
real-LLM paths were confirmed at v1 (#27, #29).

### Follow-ups

- [ ] Post-hoc real-LLM smoke of B/C once `:8080` is free.
- [x] v2 learning loop + reload auth — delivered (see the v2 record below).
- [ ] v3 candidates: vector search, subagents, MCP.

## 2026-07-04: Orbis v2 Reviewable Skill Learning Loop Completed

Status: completed (merged to `main`; unit/race verified; real-LLM manual
acceptance passed on an isolated port)

### Summary

v2 lets the runtime learn skills from completed runs **without ever
self-modifying unreviewed**: it derives deterministic Skill Proposals from run
data, a human approves or rejects them over admin-guarded APIs, and only an
explicit approval promotes a proposal into `data/skills/` (with provenance, an
audit trail, and an automatic index reload). The core invariant
`SkillProposalCreated != SkillPromoted` is enforced structurally — the proposal
lifecycle has no pending→promoted transition. The whole loop lives in the app
layer, so the reducer stays pure.

### Completed Scope

- PR #33 — proposal foundation: `SkillProposal` model + lifecycle
  (`CanTransition`), file `ProposalStore` under
  `data/skill_proposals/{pending,approved,rejected}` (bucket moves enforce
  legal transitions), JSONL `AuditLog` (`data/audit/skill_audit.jsonl`),
  versioning helpers (shared `contentHash`, reject-on-conflict), ten v2 event
  constants, `ORBIS_SKILL_LEARNING_ENABLED` / `ORBIS_SKILL_PROPOSALS_DIR` /
  `ORBIS_SKILL_AUDIT_PATH` / `ORBIS_ADMIN_TOKEN` (empty default) /
  `ORBIS_SKILL_AUTO_PROPOSE` (false default).
- PR #34 — proposal creation: deterministic `BuildRunFacts`/`DetectCandidate`
  (completed run + tools|skills|recovered-failure|repeated-procedure|explicit
  request; no LLM), deterministic `NewProposalFromRun` markdown rendering
  (ASCII slug ids, concise rationale, never hidden reasoning),
  `CreateSkillProposalFromRun` emitting SkillCandidateDetected →
  SkillProposalCreated → SkillReviewRequired plus an audit record, optional
  create-only auto-propose hook on RunCompleted.
- PR #35 — review + promotion: `Promoter` writes `data/skills/{id}.md` +
  `index.json` entries (version 1, `learned` tag, priority 50 below seeds,
  source proposal/run provenance), approve flow approve→promote→auto-reload
  with SkillProposalApproved → SkillPromoted → SkillIndexReloadRequested →
  SkillIndexReloaded → SkillAuditRecorded (conflict ⇒ proposal `failed` +
  SkillPromotionFailed), reject flow, WS
  `skill.proposal.list/get/create_from_run/approve/reject` + HTTP
  `GET /skill-proposals(?status=)`, `GET /skill-proposals/{id}`,
  `POST /runs/{runID}/skill-proposals`, `POST
  /skill-proposals/{id}/approve|reject`, and the admin gate (no token ⇒
  mutating endpoints disabled; wrong token ⇒ 401; reads open; the previously
  open v1 skills reload is now behind the same gate).
- PR #36 — docs: `docs/skill-learning.md`,
  `.workspace/.spec/v2-skill-learning-loop.md`,
  `.workspace/decisions/v2-skill-learning-decisions.md`, README v2 sections,
  and this record.

### Verification Evidence

Fresh branch verification on every PR:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
git diff --check
```

Real-LLM manual acceptance (isolated `:18080` because `:8080` is externally
held): `orbis ws smoke tool` → RunCompleted (used tools + skills) →
`POST /runs/run_smoke_msg/skill-proposals` (Bearer) → pending proposal with
detection rationale → approve without/with wrong token → 401 → approve with the
admin token → `promoted` v1 → `data/skill_proposals/approved/` 1 file, audit
JSONL 3 records (created/approved/promoted), `data/skills/index.json` gained
the learned skill (3 seeds + 1), and the reloaded `GET /skills` catalog served
all 4. Unit tests additionally prove the promoted skill is selectable via
tool-availability scoring.

### Follow-ups

- v2.1: multi-version promotion for existing skill ids; reviewer edits before
  approval; session-independent reload events.
- Replace the static admin token with real auth when the gateway grows
  authentication.
- v3 candidates: vector search, subagents, MCP.
