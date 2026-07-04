# Skills (v1)

Orbis skills are **reusable procedural knowledge** loaded into the LLM context
before planning. A skill is not a tool: it never executes a side effect. The
distinction is strict:

- **Tool** = runtime-controlled side-effect execution (the LLM proposes, the Tool
  Worker runs it). See [tool-calling.md](tool-calling.md).
- **Skill** = procedural knowledge that helps the model plan and use tools. It is
  selected deterministically and injected as instructions.

Selection happens inside the reducer and does **not** call an LLM. Disk I/O is
confined to the skill store (load/reload); the reducer only reads an immutable
in-memory snapshot, so reducer purity is preserved.

## Storage (progressive disclosure)

Skills live under `data/skills/` (the only `data/` path committed to git):

- **Level 0 — `index.json`**: an array of metadata entries
  (`id`, `name`, `title`, `description`, `tags`, `triggers`, `path`, `version`,
  `status`, `priority`, `related_tools`). Parsed with the standard library only.
- **Level 1 — markdown bodies**: the file named by each entry's `path`. The body
  is the procedural knowledge injected into the prompt.

At load the store reads the index and every body once, computing each skill's
`content_hash` (sha256) and `chars` (`utf8.RuneCountInString`). A missing or
empty body, a missing `id`/`path`, or a duplicate `id` is a clear load error.
Level 2 (assets) is an extension point only in v1.

## Store

`internal/skill.Store` is a file-based, in-memory cache. `NewStore(dir)` loads
once; `Reload()` re-reads disk and atomically swaps the entries under a
`RWMutex` (on error the previous state is kept). It exposes `Snapshot()` (a copy,
for pure selection), `Body(id)` (for context building), and `List()`/`Get(id)`
(for the API). It implements the `Index`, `Bodies`, and catalog roles so the
reducer, dispatcher, and gateway each depend only on the capability they use.

## Selection

`skill.Select(snapshot, input, cfg)` is a pure, deterministic function (same
inputs → same output, no LLM). It scores each active skill against the lowercased
query text by substring match:

| Signal | Weight |
| --- | --- |
| trigger | 10 (×count) |
| name | 6 |
| tag | 5 (×count) |
| related tool | 4 (×count) |
| title | 3 |

Skills with a non-`active` status are skipped. Candidates are sorted by score,
then `priority` (desc), then `id` (asc). The top `MaxSelected` are kept within a
`MaxChars` budget — selection stops at the first skill that would overflow the
budget. A query with no positive signal selects nothing. Description text is
intentionally **not** free-text matched in v1, to keep selection predictable.

Selection runs **once per run** (in `reduceUserMessage`). Follow-up LLM calls
after a tool result reuse the run's `SelectedSkills` without re-selecting, so the
prompt stays stable across a run.

## Context integration

The reducer carries the chosen `[]domain.SkillRef` in the `DispatchLLMCall`
action payload (metadata only — never body text). At the worker boundary the
dispatcher resolves each ref's body from the in-memory store, wraps the bodies
in an `<orbis_skills>…</orbis_skills>` block (`skill.BuildContext`), and sets it
as `LLMRequest.Instructions`. No new disk I/O happens at dispatch.

## Run snapshot

The session lane records the run's selected skills once into
`data/runs/{runID}.json` (`SelectedSkills`). It is written the first time the run
has skills and then left untouched, so the run history reflects what was applied
even if the index is later reloaded.

## Events

The reducer emits skill lifecycle events (metadata only) before `LLMCallStarted`:

- `SkillSelected` — one per chosen skill, with `score` and `reason`.
- `SkillLoaded` — one per chosen skill, with `content_hash` and `chars`.
- `SkillApplied` — once, summarizing `skill_ids`, `count`, `total_chars`.
- `SkillSkipped` — once, when selection found no match.

`SkillIndexLoaded`, `SkillIndexSearchStarted`, and `SkillSelectionFailed` are
reserved (defined, not yet emitted), mirroring v0.2's `ToolCallProposed`.

## WebSocket event sequence

A run that selects a skill streams:

```
UserMessageReceived
RunStarted
RunStatusChanged
SkillSelected
SkillLoaded
SkillApplied
LLMCallStarted
LLMResponseReceived
FinalAnswerEmitted
RunCompleted
```

With no match, `SkillSkipped` replaces the Selected/Loaded/Applied trio.

## API (read-only + reload)

The skill catalog is exposed for inspection and reload — never execution. WS and
HTTP share one implementation on the runtime service.

WebSocket methods (request/response over `/ws`):

- `skill.list` → `{ skills: [...] }`
- `skill.get` (params `{ "skill_id": "..." }`) → summary + body, or an error
- `skill.reload` → `{ count }`

HTTP endpoints (registered only when skills are enabled):

- `GET /skills` → `200` `{ skills: [...] }`
- `GET /skills/{skillID}` → `200` detail, or `404` when unknown
- `POST /skills/reload` → `200` `{ count }`, or `500` on a reload error

## Configuration

- `ORBIS_SKILLS_ENABLED` (default `true`) — when false, the reducer skips
  selection entirely and behaves exactly like v0.2.
- `ORBIS_SKILLS_DIR` (default `data/skills`)
- `ORBIS_SKILLS_MAX_SELECTED` (default `3`)
- `ORBIS_SKILLS_MAX_CHARS` (default `12000`)
- `ORBIS_SKILLS_RELOAD_ON_START` (default `true`)

## Seed skills

`data/skills/` ships eight skills, ordered by priority:

- `websocket-runtime-test` (100) — how to test the runtime over WebSocket.
- `tool-calling-policy` (90) — the tool-calling policy and safe defaults.
- `go-reducer-pattern` (80) — how to implement a pure reducer.
- `web-search` (70) — how to plan safe web searches for current facts.
- `docs-lookup` (68) — how to verify SDK/API/CLI behavior against official docs.
- `github-search` (66) — how to search GitHub issues, PRs, releases, and history.
- `runtime-debug` (64) — how to debug Orbis event flow through `/debug` and streams.
- `test-plan` (62) — how to define verification and smoke-test steps.

## Limits (v1)

- A `reload` during an in-flight run does not change that run's already-selected
  skills (the run keeps its snapshot; `content_hash` is the drift record).
- Selection is substring-based; there is no embedding/vector search.
- `SelectionInput.ToolNames` can boost skills whose `related_tools` are enabled.
- Skills are never created or edited by the runtime.

## Non-goals / follow-ups

Auto skill creation, a self-learning loop, a skill-write approval UI, tool
search, subagents, vector search/embeddings, MCP, a multi-channel gateway,
Level 2 skill assets, and dynamic mid-run skill changes are out of scope for v1.
Wiring `RuntimeService.Close()` into HTTP server shutdown is also follow-up work.

## Manual test

```bash
go run ./cmd/orbis serve            # requires .env with a real LLM provider
go run ./cmd/orbis ws smoke skill   # drives a skill-selecting prompt via the real LLM
```

Or connect with `wscat -c ws://localhost:8080/ws` and send a `session.message`
whose text induces a skill, e.g. "WebSocket으로 Orbis 런타임 테스트 방법 알려줘"
(selects `websocket-runtime-test`) or "Use web_search to check the latest
release details" (selects `web-search`). Inspect the catalog with
`curl localhost:8080/skills` and `curl localhost:8080/skills/{id}`.
