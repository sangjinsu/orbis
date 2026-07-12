# AGENTS.md

## Project Identity

```text
Project Name: Orbis
Full Name: Orbis Agent Runtime
Repo Name: orbis
CLI Name: orbis
```

Orbis Agent Runtime is a Go-based runtime environment for long-running AI agents.

The runtime is inspired by the Node.js event loop, but it must be implemented in a Go-native way using goroutines, channels, `context.Context`, worker pools, and WebSocket event streams.

The project is not a clone of OpenClaw or Hermes.
OpenClaw and Hermes concepts may be adopted later by priority, but the first milestone is a small, reliable, event-loop-first runtime kernel.

---

## CLI Naming Conventions

The official CLI name is `orbis`.

Use `orbis` in commands, documentation, examples, package naming, and local development scripts.

Preferred commands:

```bash
orbis serve
orbis dev
orbis ws
orbis session create
orbis run status
```

During early development, running from source is acceptable:

```bash
go run ./cmd/orbis
```

Do not introduce old placeholder names such as `ael-runtime`, `agent-event-loop`, or `loop-agent-runtime` in code, docs, commands, or generated examples.

---

## Core Mission

Build an agent runtime where the runtime owns the loop.

The LLM must not directly control the execution loop.  
The runtime receives events, updates state, dispatches actions, and receives new events from workers.

Core rule:

```text
Event + Current State => New State + Actions
```

The reducer decides state transitions.  
Workers execute side effects.  
Workers return new events.  
WebSocket clients observe runtime progress through event streams.

---

## Repository Naming

The repository name is `orbis`.

Use this name in:

- README examples
- module documentation
- local clone examples
- issue templates
- release notes

Go module path:

```text
module github.com/sangjinsu/orbis
```

If the repository is moved to another organization later, update only the module path and import references. Do not change the project, full, or CLI names.

---

## Runtime Philosophy

Do not implement a normal synchronous agent loop like this:

```go
for {
    result := callLLM(...)
    if result.Final {
        return result
    }

    toolResult := callTool(...)
    append(toolResult)
}
```

Instead, implement this event-driven flow:

```text
WebSocket Client
  -> session.message request
  -> UserMessageReceived event
  -> Session Lane
  -> Reducer
  -> DispatchLLMCall action
  -> LLM Worker
  -> LLMResponseReceived event
  -> Reducer
  -> DispatchToolCall or EmitFinalAnswer
  -> Tool Worker or Final Response
  -> Runtime Events pushed over WebSocket
```

The runtime owns:

- event ordering
- session state
- run state
- cancellation
- timeout
- action dispatch
- observability
- WebSocket progress streaming

The LLM is only one worker inside the loop.

---

## Go Implementation Direction

Do not try to literally recreate the Node.js event loop.

Use Go-native runtime patterns:

```text
전체 시스템 = 병렬 처리
개별 세션 = 순서 보장
```

Recommended model:

```text
Gateway
  -> Event Queue
  -> Session Lane
  -> Reducer
  -> Action Dispatcher
  -> Worker Pool
  -> Result Event
  -> Session Lane
  -> WebSocket Event Stream
```

Use:

- goroutines for session lanes, workers, WebSocket read/write loops
- channels with explicit ownership for session event queues and worker handoff
- `select` for event/timer/cancel handling
- `context.Context` for cancellation and timeout
- interfaces for Store, Worker, Tool, LLM Provider
- `log/slog` for structured logs
- JSONL for initial event and trace persistence

---

## Architecture Direction

Orbis adopts a modular monolith architecture.

Keep the current v2.1 baseline as one deployable Go process, while separating package responsibilities clearly:

- `internal/domain` owns stable runtime types.
- `internal/runtime` owns reducer, lane, dispatcher, and loop coordination.
- `internal/worker` owns side-effect execution such as LLM, tool, and timer workers.
- `internal/gateway` owns HTTP and WebSocket boundaries.
- `internal/store` owns persistence interfaces and file-based implementations.
- `internal/protocol` owns wire DTOs when they diverge from domain types.

Do not split into microservices, distributed workers, external brokers, or separate deployables without an accepted milestone spec and decision record.

---

## GitHub Pull Request Conventions

All PR bodies must be written in Korean.

Use `.github/pull_request_template.md` for every PR body.
Keep command output and error text in their original language when needed, but write summaries, rationale, risk notes, and validation explanations in Korean.

---

## Current Shipped Scope: v2.1 + CLI

`main` has shipped the v0.1, v0.2, v1, v1.5, v2, and v2.1 milestones. The current baseline combines the event-loop-first runtime with reviewable skill learning, operational CLI surfaces, and interactive chat. The next product milestone has not been selected.

### Included

- Runtime kernel: one Go process, HTTP/WebSocket gateways, session lanes, pure reducer, action dispatcher, workers, cancellation, timeout, persistence, and observable event streams.
- Tool calling: runtime-owned validation, policy, idempotency, retry, tool execution, and result events.
- Skills: deterministic selection, bounded context injection, catalog APIs, and explicit reload.
- Reviewable learning: proposal creation, structured review edits, approve/reject, audited promotion, and no unreviewed automatic promotion.
- v2.1 hardening: learned-skill version bumps and archive, global live lifecycle feed, and named reviewer/admin token roles.
- Runtime debug visualizer for observing session activity.
- Cobra CLI surfaces: `orbis serve`, `orbis ws`, `orbis skills`, `orbis proposal`, `orbis watch`, and interactive `orbis chat`.
- `.workspace` source-of-truth records for specs, decisions, project memory, references, and history.

### Excluded Without an Accepted Milestone

Do not add these without an accepted milestone spec and decision record:

- unreviewed automatic skill promotion or self-modification
- vector or semantic skill search
- subagents
- MCP integration
- multi-channel messenger gateways or Slack/Telegram/Discord adapters
- distributed brokers or workers
- Kubernetes deployment
- full OpenClaw or Hermes compatibility
- durable kanban/task board or advanced long-term memory

---

## Runtime Phases

Use the Node.js event loop as conceptual inspiration.

The runtime does not need literal phase functions at first, but the responsibilities must remain clear.

```text
timers
  - retry backoff
  - sleep
  - scheduled resume
  - run timeout

pending
  - LLM result
  - tool result
  - worker error
  - approval result, later

prepare
  - load session state
  - build context
  - check budget
  - check policy
  - prepare action candidates

poll
  - HTTP input
  - WebSocket input
  - worker callback
  - timer event

check
  - dispatch LLM call
  - dispatch tool call
  - dispatch timer
  - emit final answer

close
  - complete run
  - fail run
  - cancel run
  - flush logs
  - release session lock
```

---

## Key Runtime Invariants

These rules are mandatory.

### 1. Reducers Must Be Pure

A reducer must not:

- call an LLM
- call a tool
- write to WebSocket directly
- send external messages
- mutate global state directly
- start goroutines

A reducer may only:

- inspect current state
- inspect an event
- produce next state
- produce actions
- produce derived internal events if needed

### 2. Workers Execute Side Effects

Side effects must be isolated in workers.

Examples:

- LLM calls happen in an LLM worker
- tool calls happen in a tool worker
- timers happen in a timer worker
- WebSocket delivery happens through a broker/gateway layer

### 3. Session State Must Be Serialized

Only one reducer may mutate a session at a time.

Workers may run concurrently, but worker results must return as events and be applied through the session lane.

### 4. Every Side Effect Needs an Idempotency Key

Any external or side-effecting action must include an idempotency key.

Examples:

- tool calls
- LLM calls
- message sends
- file writes
- external API calls

### 5. Everything Important Must Be Observable

Log or trace:

- event received
- reducer transition
- actions emitted
- worker started
- worker completed
- worker failed
- run completed
- run cancelled
- run failed

### 6. Cancellation Uses `context.Context`

Long-running workers must respect context cancellation.

A cancelled run must not dispatch new side-effect actions.

---

## Core Domain Types

### Event

An event is a fact that already happened.

Examples:

- `UserMessageReceived`
- `RunStarted`
- `LLMCallStarted`
- `LLMResponseReceived`
- `ToolCallStarted`
- `ToolCallSucceeded`
- `ToolCallFailed`
- `TimerFired`
- `FinalAnswerEmitted`
- `RunCompleted`
- `RunFailed`
- `RunCancelled`

Suggested shape:

```go
type Event struct {
    EventID   string          `json:"event_id"`
    SessionID string          `json:"session_id"`
    RunID     string          `json:"run_id"`
    Type      EventType       `json:"type"`
    Seq       int64           `json:"seq"`
    CreatedAt time.Time       `json:"created_at"`
    Payload   json.RawMessage `json:"payload"`
}
```

### Action

An action is work the runtime wants a worker or gateway to perform.

Examples:

- `DispatchLLMCall`
- `DispatchToolCall`
- `ScheduleTimer`
- `EmitFinalAnswer`
- `CancelWorker`

Suggested shape:

```go
type Action struct {
    ActionID       string          `json:"action_id"`
    SessionID      string          `json:"session_id"`
    RunID          string          `json:"run_id"`
    Type           ActionType      `json:"type"`
    IdempotencyKey string          `json:"idempotency_key"`
    Payload        json.RawMessage `json:"payload"`
}
```

### Run Status

```go
type RunStatus string

const (
    RunIdle         RunStatus = "IDLE"
    RunQueued       RunStatus = "QUEUED"
    RunPreparing    RunStatus = "PREPARING"
    RunWaitingLLM   RunStatus = "WAITING_LLM"
    RunWaitingTool  RunStatus = "WAITING_TOOL"
    RunWaitingTimer RunStatus = "WAITING_TIMER"
    RunWaitingHuman RunStatus = "WAITING_HUMAN"
    RunCompleted    RunStatus = "COMPLETED"
    RunFailed       RunStatus = "FAILED"
    RunCancelled    RunStatus = "CANCELLED"
)
```

`WAITING_HUMAN` may be defined now but implemented later.

### Session

A session groups related events and runs.

A session should maintain:

- `session_id`
- current run id
- run status
- message history
- pending actions
- last event seq
- created timestamp
- updated timestamp

---

## WebSocket Runtime Testing

WebSocket support originated in v0.1 and remains a core part of the shipped baseline.

The gateway tests communication with the runtime and with an LLM through the runtime.

Do not make WebSocket handlers call the LLM directly.

Correct flow:

```text
WebSocket request
  -> validate
  -> convert to Event
  -> enqueue Event
  -> send immediate ACK
  -> runtime processes event asynchronously
  -> runtime broadcasts progress events
```

Incorrect flow:

```text
WebSocket request
  -> call LLM directly
  -> wait until completion
  -> send response
```

That incorrect flow must be avoided.

---

## WebSocket Protocol

Use a minimal request/response/event protocol.

### Client Request

```json
{
  "type": "req",
  "id": "req_001",
  "method": "session.message",
  "params": {
    "session_id": "session_001",
    "text": "안녕, Agent Event Loop 테스트 중이야."
  }
}
```

### Immediate Server ACK

```json
{
  "type": "res",
  "id": "req_001",
  "ok": true,
  "payload": {
    "session_id": "session_001",
    "run_id": "run_001"
  }
}
```

### Runtime Event

```json
{
  "type": "event",
  "event": "LLMCallStarted",
  "seq": 12,
  "session_id": "session_001",
  "run_id": "run_001",
  "payload": {}
}
```

### Streaming Assistant Delta

```json
{
  "type": "event",
  "event": "AssistantDelta",
  "seq": 13,
  "session_id": "session_001",
  "run_id": "run_001",
  "payload": {
    "delta": "안녕하세요"
  }
}
```

### Final Answer

```json
{
  "type": "event",
  "event": "FinalAnswerEmitted",
  "seq": 21,
  "session_id": "session_001",
  "run_id": "run_001",
  "payload": {
    "text": "안녕하세요. Agent Event Loop 테스트가 정상적으로 동작하고 있습니다."
  }
}
```

---

## Core WebSocket Methods (v0.1 Historical Baseline)

These methods formed the initial baseline and remain implemented:

- `session.create`
- `session.message`
- `session.subscribe`
- `run.cancel`
- `run.status`
- `events.list`

---

## Core Server Events (v0.1 Historical Baseline)

These events formed the initial baseline and remain implemented:

- `SessionCreated`
- `UserMessageReceived`
- `RunStarted`
- `RunStatusChanged`
- `LLMCallStarted`
- `LLMResponseReceived`
- `ToolCallStarted`
- `ToolCallSucceeded`
- `ToolCallFailed`
- `AssistantDelta`
- `FinalAnswerEmitted`
- `RunCompleted`
- `RunFailed`
- `RunCancelled`

---

## HTTP API

Keep HTTP small.

Suggested endpoints:

```text
POST /sessions
GET  /sessions/{sessionID}
POST /sessions/{sessionID}/messages
GET  /sessions/{sessionID}/events
GET  /runs/{runID}
POST /runs/{runID}/cancel
GET  /healthz
GET  /readyz
GET  /ws
```

`GET /ws` upgrades to WebSocket.

---

## Recommended Go Package Layout

Keep the current modular-monolith directory boundaries explicit.

```text
.
├── cmd/
│   └── orbis/
├── internal/
│   ├── app/
│   ├── auth/
│   ├── broker/
│   ├── config/
│   ├── domain/
│   ├── gateway/
│   ├── protocol/
│   ├── runtime/
│   ├── skill/
│   ├── store/
│   ├── tool/
│   └── worker/
├── data/
├── docs/
├── .workspace/
├── go.mod
├── go.sum
├── Makefile
└── AGENTS.md
```

`RuntimeService` in `internal/app` owns one event channel per session with explicit channel lifecycle ownership; there is no separate queue package in the current architecture.

Do not create unnecessary layers before they are needed.

---

## Dependency Guidance

Prefer the Go standard library first.

Recommended initial dependencies:

- WebSocket: `github.com/coder/websocket`
- Logging: `log/slog`
- HTTP: `net/http`
- JSON: `encoding/json`
- Configuration: `.env` loaded into environment variables at startup

Runtime configuration must be controlled through `.env` for local development.
Do not hard-code model names, API keys, base URLs, ports, or data paths.

Initial `.env` keys:

```text
ORBIS_ADDR=:8080
ORBIS_DATA_DIR=data
ORBIS_LLM_PROVIDER=openai
ORBIS_LLM_MODEL=<model>
OPENAI_API_KEY=<api-key>
OPENAI_BASE_URL=https://api.openai.com
```

`.env` must not be committed. Commit `.env.example` with safe placeholder values.

The current modular-monolith baseline continues to avoid heavy frameworks.

Do not introduce:

- Kubernetes clients
- ORM frameworks
- distributed workflow engines
- large dependency injection frameworks
- production message brokers
- full OpenTelemetry setup before core tests pass

These can be added later.

---

## Storage

The v0.1 baseline established file-based storage, which remains the current persistence model.

Use JSONL for events and traces:

```text
data/events/{session_id}.jsonl
data/traces/{run_id}.jsonl
```

Use JSON for latest snapshots:

```text
data/sessions/{session_id}.json
data/runs/{run_id}.json
```

The store interface must allow future migration to Postgres.

Suggested interface:

```go
type Store interface {
    AppendEvent(ctx context.Context, event Event) error

    LoadSession(ctx context.Context, sessionID string) (SessionState, error)
    SaveSession(ctx context.Context, state SessionState) error

    LoadRun(ctx context.Context, runID string) (RunState, error)
    SaveRun(ctx context.Context, state RunState) error
}
```

---

## LLM Worker

The v0.1 baseline established a real LLM provider from the start, and the real provider remains the operational default.

The real provider is configured through `.env`.
The first provider target is OpenAI-compatible HTTP behind the `LLMProvider` interface.
The runtime must not depend directly on provider-specific details outside the worker/provider package.

The worker must support:

1. final answer
2. streaming delta simulation from provider output when provider streaming is not yet implemented
3. timeout
4. cancellation
5. provider error as runtime event

The mock LLM remains available only for deterministic tests and explicit local fallback.
The mock LLM must support deterministic scenarios:

1. final answer only
2. tool call then final answer
3. tool failure retry
4. timeout
5. cancellation
6. streaming delta simulation

Suggested interface:

```go
type LLMProvider interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan LLMStreamEvent, error)
}
```

The runtime should not depend directly on a specific LLM vendor.

---

## Tool Worker

The shipped tool worker includes deterministic built-in mock tools for tests and runtime exercises.

Initial tools:

- `echo`
- `time.now`
- `math.add`
- `mock.fail_once`

Tool call results must return as events.

Never call tools directly from the reducer.

Suggested tool interface:

```go
type Tool interface {
    Name() string
    Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}
```

---

## Timer Worker

The timer worker supports:

- one-shot delay
- run timeout
- retry backoff

The timer worker emits `TimerFired`.

---

## WebSocket Broker

The WebSocket broker broadcasts runtime events to subscribed clients.

Broker responsibilities:

- manage session subscribers
- broadcast runtime events
- remove disconnected clients
- avoid blocking the runtime on slow clients
- support graceful shutdown

The broker must not mutate session state.

---

## Session Lane Pattern

Use a session actor/lane pattern.

```text
session_id 하나당 logical actor
actor는 자기 session event를 순서대로 처리
state mutation은 actor 내부에서만 수행
```

This may be implemented with goroutines and channels.

Example shape:

```go
type SessionLane struct {
    sessionID string
    events    chan Event
    reducer   Reducer
    store     Store
    dispatch  Dispatcher
}
```

The exact implementation may differ, but the invariant must hold:

```text
same session -> ordered state mutation
different sessions -> concurrent processing allowed
```

---

## Reducer Pattern

The reducer is the most important test target.

Suggested interface:

```go
type Reducer interface {
    Apply(ctx context.Context, state SessionState, event Event) (ReduceResult, error)
}

type ReduceResult struct {
    NextState SessionState
    Actions   []Action
    Events    []Event
}
```

Reducer behavior must be deterministic.

---

## Dispatcher Pattern

The dispatcher routes actions to workers.

Examples:

```text
DispatchLLMCall -> LLM worker queue
DispatchToolCall -> Tool worker queue
ScheduleTimer -> Timer worker
EmitFinalAnswer -> Broker/event stream
```

The dispatcher should not mutate session state directly.

---

## Error Handling

Errors that affect runtime state must become events.

Examples:

- `LLMCallFailed`
- `ToolCallFailed`
- `TimerFailed`
- `RunFailed`
- `RunCancelled`

Use Go errors internally, but persistent runtime failures must be visible as events.

---

## Observability

Use `log/slog` for structured logs.

Every event processing cycle should log:

- event id
- session id
- run id
- event type
- previous state
- next state
- actions emitted
- duration
- error if any

WebSocket subscribers should receive runtime events for a session.

---

## Testing Strategy

Write tests around the reducer first.

Minimum tests:

- user message creates a run and dispatches LLM action
- LLM final response completes run
- LLM tool call dispatches tool action
- tool success dispatches next LLM action
- tool failure marks retry or failed state
- run cancellation prevents new side effects
- events for the same session are applied in order
- WebSocket request returns immediate ACK
- WebSocket subscriber receives runtime events
- real LLM response flow emits `AssistantDelta`

Use table-driven tests.

Run before committing:

```bash
gofmt -w .
go test ./...
```

If a Makefile exists, also support:

```bash
make test
make lint
make run
```

---

## Manual WebSocket Test

Server:

```bash
go run ./cmd/orbis
```

Client:

```bash
wscat -c ws://localhost:8080/ws
```

Message:

```json
{
  "type": "req",
  "id": "req_001",
  "method": "session.message",
  "params": {
    "session_id": "session_001",
    "text": "안녕, Agent Event Loop 테스트 중이야."
  }
}
```

Expected sequence:

```text
res:req_001
SessionCreated, if needed
UserMessageReceived
RunStarted
LLMCallStarted
AssistantDelta, optional
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```


---

## `.workspace` LLM Wiki

This project uses `.workspace` as a project-local LLM wiki.

`.workspace` is not application runtime state. It is a documentation and coordination layer for humans and coding agents working on this repository.

Use `.workspace` to keep project knowledge stable across tasks, especially when the work involves architectural decisions, implementation rationale, specs, and reference documents.

### Goals

`.workspace` must contain:

1. decision rationale written by the LLM or coding agent
2. task and project memory
3. project specs stored under `.workspace/.spec`
4. foundational reference documents

### Required Directory Layout

Use this structure:

```text
.workspace
├── README.md
├── decisions
│   └── YYYY-MM-DD-short-title.md
├── memory
│   ├── project.md
│   ├── tasks.md
│   ├── assumptions.md
│   ├── glossary.md
│   └── known-issues.md
├── .spec
│   ├── runtime-v0.1.md
│   ├── websocket-protocol.md
│   ├── event-model.md
│   ├── state-machine.md
│   ├── llm-worker.md
│   ├── tool-worker.md
│   ├── storage.md
│   └── observability.md
├── references
│   ├── openclaw.md
│   ├── hermes.md
│   ├── node-event-loop.md
│   └── go-concurrency.md
└── scratch
    └── YYYY-MM-DD.md
```

If a file is not needed yet, do not create empty noise files. Create files when they become useful.

### Meaning of Each Folder

#### `.workspace/decisions`

Store concise decision rationale records.

This folder answers:

```text
Why did we choose this design?
What alternatives were considered?
What trade-off did we accept?
What should future agents not re-litigate without new evidence?
```

Do not store private hidden chain-of-thought. Store only a clean, user-visible rationale summary that is useful for future maintainers.

Decision file template:

```markdown
# Decision: <short title>

Date: YYYY-MM-DD
Status: proposed | accepted | superseded

## Context

What problem were we solving?

## Decision

What did we decide?

## Rationale Summary

Why does this decision make sense?
Keep this concise and reproducible.

## Alternatives Considered

- Option A: reason rejected
- Option B: reason rejected

## Consequences

What becomes easier?
What becomes harder?

## Follow-ups

- [ ] Next action
```

#### `.workspace/memory`

Store stable project memory.

Use this for facts that should survive across tasks but are not formal specs.

Examples:

- project direction
- current milestone
- implementation status
- naming decisions
- user preferences for this project
- known constraints
- open questions
- recurring terminology

Recommended files:

```text
project.md       - stable project identity and goals
tasks.md         - current and upcoming work
assumptions.md   - assumptions that need validation
glossary.md      - domain terms and naming
known-issues.md  - bugs, risks, and unresolved design problems
```

Memory must be concise. Do not paste long logs, raw chats, secrets, or full external documents into memory.

#### `.workspace/.spec`

Store project specifications.

Specs are the source of truth for implementation. When code and specs disagree, either update the code or explicitly update the spec with a decision record.

Initial specs should cover:

```text
runtime-v0.1.md        - scope, goals, non-goals, done criteria
websocket-protocol.md  - request/response/event protocol
event-model.md         - event envelope and event types
action-model.md        - action envelope and action types
state-machine.md       - run/session state transitions
llm-worker.md          - mock and real LLM worker behavior
tool-worker.md         - mock tool registry and execution rules
storage.md             - JSONL store, snapshots, future Postgres path
observability.md       - logs, traces, WebSocket runtime events
```

Spec files should include:

```markdown
# <Spec Name>

## Status
Draft | Accepted | Deprecated

## Purpose

## Scope

## Non-Goals

## Data Model

## Flow

## Edge Cases

## Testing Requirements

## Open Questions
```

#### `.workspace/references`

Store foundational reference notes.

This folder is for summarized reference material, not raw dumps.

Examples:

- OpenClaw concepts to adopt later
- Hermes concepts to adopt later
- Node.js event loop concepts
- Go concurrency patterns
- WebSocket protocol references

Each reference note should include:

```markdown
# Reference: <name>

## Why This Matters

## Key Ideas

## Concepts to Borrow

## Concepts Not to Borrow Yet

## Links or Source Notes
```

#### `.workspace/scratch`

Temporary working notes.

Scratch files are allowed during exploration, but important conclusions must be promoted to one of:

- `.workspace/decisions`
- `.workspace/memory`
- `.workspace/.spec`
- `.workspace/references`

Do not treat scratch as source of truth.

### Source of Truth Order

When files conflict, use this precedence:

```text
1. AGENTS.md
2. .workspace/.spec
3. .workspace/decisions
4. .workspace/memory
5. .workspace/references
6. .workspace/scratch
```

If the conflict is meaningful, create or update a decision record.

### Required Agent Workflow

Before starting a non-trivial task, read:

```text
AGENTS.md
.workspace/README.md, if present
.workspace/memory/project.md, if present
relevant files under .workspace/.spec
relevant decision records
```

After completing a non-trivial task, update `.workspace` when appropriate:

```text
- update .workspace/memory/tasks.md when task status changes
- update .workspace/.spec when behavior or protocol changes
- add a decision record when a meaningful architecture choice is made
- update references when OpenClaw/Hermes concepts are analyzed
- promote useful scratch notes into specs, memory, or decisions
```

### What Must Not Be Stored in `.workspace`

Do not store:

- API keys
- credentials
- private tokens
- personal secrets
- raw hidden chain-of-thought
- huge raw logs
- generated build artifacts
- dependency caches
- vendored third-party documents

Use `.gitignore` rules if any `.workspace` subfolder should remain local-only.

### Recommended `.workspace/README.md`

Create this file when initializing the repository:

```markdown
# .workspace

This folder is the project-local LLM wiki for Orbis Agent Runtime.

It contains:

- decision rationale summaries
- project memory
- formal specs under `.spec`
- foundational reference notes
- temporary scratch notes

It does not contain application runtime state, secrets, or hidden chain-of-thought.

Source of truth order:

1. AGENTS.md
2. .workspace/.spec
3. .workspace/decisions
4. .workspace/memory
5. .workspace/references
6. .workspace/scratch
```

### Recommended Initial `.workspace` Files

For this project, initialize at least:

```text
.workspace/README.md
.workspace/memory/project.md
.workspace/memory/tasks.md
.workspace/.spec/runtime-v0.1.md
.workspace/.spec/websocket-protocol.md
.workspace/.spec/event-model.md
.workspace/.spec/state-machine.md
.workspace/references/openclaw.md
.workspace/references/hermes.md
.workspace/references/node-event-loop.md
.workspace/references/go-concurrency.md
```

### Recommended Current `.workspace/memory/project.md` Content

Suggested starting content:

```markdown
# Project Memory: Orbis Agent Runtime

## Identity

Orbis Agent Runtime is a Go-based event-loop-first runtime for long-running AI agents.

## Current Baseline

v2.1 runtime and reviewable skill-learning behavior are shipped, together with operational Cobra CLI and interactive chat surfaces.

## Core Principle

The runtime owns the loop. The LLM is only one worker in the loop.

## Architecture

Use Go-native concurrency:

- goroutines
- channels
- select
- context.Context
- worker pools
- session lane ordering
- WebSocket event streams

## Next Milestone

No next product milestone has been selected.

## Current Non-Goals

- unreviewed automatic skill promotion
- vector search
- subagents
- MCP integration
- multi-channel messenger gateways
- distributed brokers or workers
- Kubernetes deployment
```

---

## OpenClaw and Hermes Adoption Priority

Keep extension points ready, but require an accepted milestone spec and decision record before adding any not-yet-shipped concept.

### P0: Already Reflected in the Shipped Baseline

OpenClaw-inspired:

- session lane ordering
- runtime event stream
- immediate ACK + async progress events
- cancellation and timeout direction
- named-role mutation auth and a live global skill lifecycle feed

Hermes-inspired:

- prompt stability principle
- bounded memory placeholder
- interruptible execution principle
- platform-agnostic core principle
- skills as bounded procedural context
- reviewable, explicitly approved skill promotion

### P1: Candidates Requiring an Accepted Milestone

OpenClaw-inspired:

- steering queue
  - `steer`
  - `followup`
  - `collect`
  - `interrupt`
- context report
- WebSocket protocol expansion
- broader gateway auth/pairing
- global concurrency caps

Hermes-inspired:

- frozen memory snapshot
- profile isolation
- execution backend abstraction

### P2: Add Later

Hermes-inspired:

- tool search
- subagent guardrails

OpenClaw-inspired:

- session tools
- multi-agent session visibility
- multi-channel gateway adapters

### P3: Research Later

- durable kanban/task board
- dreaming/memory promotion
- self-improving skill loop
- MCP integration
- Kubernetes worker scaling
- dashboard
- mobile node

---

## Coding Style

Use idiomatic Go.

Guidelines:

- keep packages small
- avoid global mutable state
- pass `context.Context`
- prefer explicit interfaces
- avoid premature abstractions
- keep domain types in `internal/domain`
- keep protocol DTOs separate from domain models when they diverge
- make reducer behavior deterministic
- avoid goroutine leaks
- always handle channel ownership clearly
- use race detector for concurrency-sensitive changes when possible

---

## Forbidden Shortcuts

Do not:

- put LLM calls inside reducers
- put tool calls inside reducers
- skip event logging
- store secrets or raw hidden chain-of-thought in `.workspace`
- mutate session state from WebSocket handlers
- wait for full LLM completion before sending WebSocket ACK
- allow concurrent state mutation for the same session
- add OpenClaw/Hermes advanced features without an accepted milestone spec and decision record
- hide tool failures
- fire side effects without idempotency keys
- create background goroutines without cancellation
- add large frameworks without a clear reason

---

## Historical v0.1 Implementation Order (Completed)

This completed sequence is preserved as the v0.1 implementation record, not as the active roadmap:

1. domain types
2. store interface and JSONL store
3. in-memory event queue
4. session lane
5. reducer
6. dispatcher
7. real LLM worker
8. mock tool worker
9. timer worker
10. broker
11. HTTP API
12. WebSocket protocol
13. WebSocket runtime event streaming
14. tests
15. `.workspace` initialization
16. documentation

---

## Historical v0.1 Done Criteria (Completed)

These criteria were satisfied for the shipped v0.1 milestone and are preserved as its completion contract:

1. The server starts with one command.
2. A client can connect through WebSocket.
3. A user message becomes `UserMessageReceived`.
4. The runtime creates a run.
5. The reducer dispatches an LLM action.
6. Real LLM provider can emit a final answer when `.env` contains valid provider settings.
7. The runtime can emit `AssistantDelta` events.
8. LLM flow can request a mock tool.
9. Mock tool result returns as an event.
10. The run reaches `COMPLETED`, `FAILED`, or `CANCELLED`.
11. Events are saved to JSONL.
12. Session state is saved.
13. WebSocket subscribers receive progress events.
14. `go test ./...` passes.

---

## Project Summary

Orbis is an event-loop-first agent runtime written in Go.

It should feel like a small runtime kernel, not a chatbot script.

The runtime owns:

- state
- scheduling
- cancellation
- timeout
- event ordering
- action dispatch
- observability
- WebSocket communication

The LLM is only one worker in the loop.
