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
- done: v2 learning loop + reload auth (below). Remaining v3 candidates:
  vector search, subagents, MCP.

## v2: Reviewable Skill Learning Loop

The runtime derives Skill Proposals from completed runs; a human must approve a
proposal before it is promoted to an active skill. `SkillProposalCreated !=
SkillPromoted` is enforced structurally (no pending→promoted transition). The
whole loop lives in the app layer — the reducer stays pure.

- `internal/skill`: `SkillProposal` + lifecycle (`CanTransition`),
  `ProposalStore` (`data/skill_proposals/{pending,approved,rejected}`, bucket
  moves enforce transitions), JSONL `AuditLog`, versioning
  (shared `contentHash`, reject-on-conflict), deterministic detector
  (`BuildRunFacts`/`DetectCandidate`) and renderer (`NewProposalFromRun`),
  `Promoter` (md + index entry with version/`learned` tag/priority 50/
  provenance; bootstraps a missing index).
- `internal/app/skill_learning.go`: create-from-run (events Candidate/Created/
  ReviewRequired + audit), approve→promote→auto-reload (Approved/Promoted/
  IndexReloadRequested/IndexReloaded/AuditRecorded; conflict ⇒ failed +
  PromotionFailed), reject (Rejected/AuditRecorded), `requireAdmin`, WS
  handlers; optional create-only auto-propose hook (`ORBIS_SKILL_AUTO_PROPOSE`,
  default false).
- APIs (one impl, two surfaces): WS `skill.proposal.list/get/create_from_run/
  approve/reject`; HTTP `GET /skill-proposals(?status=)`,
  `GET /skill-proposals/{id}`, `POST /runs/{runID}/skill-proposals`,
  `POST /skill-proposals/{id}/approve|reject`. Admin gate: no token ⇒ mutating
  disabled (403); wrong ⇒ 401; reads open. v1 `skills reload` (HTTP+WS) is now
  admin-gated too.
- Specs: `.spec/v2-skill-learning-loop.md`,
  `decisions/v2-skill-learning-decisions.md`, `docs/skill-learning.md`.
  Config: `ORBIS_SKILL_LEARNING_ENABLED/PROPOSALS_DIR/AUDIT_PATH`,
  `ORBIS_ADMIN_TOKEN`, `ORBIS_SKILL_AUTO_PROPOSE`.
- PRs: #33 (foundation), #34 (creation/detection), #35 (review/promotion/APIs),
  #36 (docs).

## v2 Status

Completed on 2026-07-04 (PRs #33–#36 merged to `main`).

Fresh verification per PR: `gofmt -l .`, `go vet ./...`, `go test ./...`,
`go test -race ./...`, `git diff --check`. Real-LLM manual acceptance on an
isolated port: tool run → create proposal → 401 auth matrix → approve →
promoted v1 → audit trail (3 records), index 3 seeds + 1 learned, reloaded
catalog serves 4 skills; the learned skill is selectable via tool-availability
scoring (unit-tested).

## Post-v2 Follow-ups

- done: all four items shipped in v2.1 (below) — multi-version promotion,
  reviewer edits, session-independent lifecycle events, named-token auth.
- v3 candidates: vector search, subagents, MCP.

## v2.1: Learning Loop Hardening

Four features hardening the v2 loop, one PR each, plus two latent-defect
fixes. v2 invariants unchanged (`SkillProposalCreated != SkillPromoted`,
reducer purity, seed protection).

- PR #40 — multi-version promotion: re-promoting a learned skill id bumps the
  integer version in place, archives the old body to
  `data/skills/archive/{id}@{version}.md`, and refreshes the index entry.
  Seeds (empty `source_proposal_id`) still conflict; non-integer learned
  versions fail loudly into the retryable `failed` state. Also fixed: the
  body-exists check that let an orphan `.md` block retries forever, and
  non-atomic snapshot writes (`writeJSON` is now temp+rename — LoadRun could
  read a truncated snapshot mid-persist; safe post-`Close()`, the pre-Close
  rejection no longer applies).
- PR #44 (recreated #41) — reviewer edits: WS `skill.proposal.update` + HTTP
  `PATCH /skill-proposals/{id}` edit the eight body-composing fields of a
  pending proposal; `Rerender()` re-derives Body/ContentHash via the creation
  renderer; identity/rationale/body are immutable by wire-type construction;
  `Revision` keys per-edit audit/event ids (`SkillProposalUpdated`).
- PR #42 — global feed: broker `SubscribeGlobal`/`PublishGlobal`;
  `RuntimeService.publish` fans the 11 skill-learning event types out
  globally (whitelist in app); subscribe via `session.subscribe`
  `scope:"global"`. The standalone reload (previously silent) emits
  global-only Requested/Reloaded with `{actor, count}`. Live-only feed;
  read-open by conscious decision.
- PR #43 — named-token auth: `ORBIS_AUTH_TOKENS=name:role:token,...`, roles
  `reviewer`/`admin`, `internal/auth` leaf package, constant-time
  no-early-return matching, explicit `actor` params → audit shows who
  approved what. No tokens ⇒ disabled 403 / unknown ⇒ 401 / wrong role ⇒
  403; reads open. `ORBIS_ADMIN_TOKEN` merges as admin "admin";
  collisions fail config loading.
- Specs: `.spec/v2.1-learning-loop-hardening.md`,
  `decisions/v2.1-decisions.md`, `docs/skill-learning.md`. Config:
  `ORBIS_AUTH_TOKENS` (legacy `ORBIS_ADMIN_TOKEN` merged).

## v2.1 Status

Completed on 2026-07-08 (PRs #40, #44, #42, #43 merged to `main`; #44
recreated #41 after a stacked-merge branch deletion closed it — retarget
children before deleting a parent branch).

Fresh main verification: `gofmt -l .`, `go vet ./...`, `go test ./...
-count=1`, `go test -race ./...`, `git diff --check` all green. Real-LLM
combined acceptance on `:18080`: smoke tool run → 401 matrix → reviewer (bob)
created/edited (revision 1, body re-rendered)/approved ⇒ promoted v1 →
same-id re-approval ⇒ promoted v2 + `archive/{id}@1.md` + reloaded catalog
served the revised body → reload matrix bob 403 / alice 200 / legacy token
200 → audit actors recorded `bob`. See `history.md` for the full record.

## Post-v2.1 Follow-ups

- done: learning-loop CLI (`orbis skills|proposal|watch`) — the tmp_globalwatch
  demo script is superseded by `orbis watch`; delete it in a separate cleanup.
- consider reviewer-gating the global feed if payloads grow beyond metadata.
- token rotation/expiry or an external IdP when static tokens fall short.
- v2.2 candidates: multi-entry version history; proposal deletion/expiry.
- v3 candidates: vector search, subagents, MCP.
