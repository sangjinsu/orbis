# v1 Skill System & Context Builder

## Status

Accepted

## Historical Supersession Note

This accepted spec preserves the v1 skill-system contract. Its three-seed
inventory is no longer a fixed catalog size because later approved proposals
can add learned skills. Its Non-Goals and `Close()` follow-ups describe the v1
boundary: v1.5 wired production shutdown and tool-aware selection, v2/v2.1
shipped reviewable learning and role-based reload auth, and v2.1 made snapshot
writes atomic.

## Purpose

Introduce skills: reusable procedural knowledge loaded into the LLM context
before planning. A skill is not a tool and never executes a side effect. Skill
selection is a pure, deterministic in-memory computation inside the reducer (no
LLM, no disk I/O); disk I/O is confined to the skill store. The dispatcher
renders selected bodies into the request instructions.

## Scope

- `internal/skill`: `Metadata`, `Entry`, `Index`/`Bodies` interfaces, file-based
  `Store` (load/reload/snapshot/body/list/get), `index.json` parsing, body loader
  (sha256 hash + rune count), deterministic `Select`, `BuildContext`, event
  payloads.
- `internal/domain/skill.go`: `SkillRef{ID,Name,Version,Path,ContentHash,Chars}`.
- `internal/runtime`: `ReducerConfig` skill fields, reducer selection + lifecycle
  events + `SelectedSkills`, dispatcher `Instructions` injection, lane run
  snapshot.
- `internal/app`: skill store wiring, `SkillCatalog`, WS skill methods,
  `gateway.Skills` implementation, `RuntimeService.Close()` graceful shutdown.
- `internal/gateway`: `Skills` interface, `WithSkills`, HTTP skill routes.
- `internal/protocol`: skill wire DTOs.
- `internal/config`: `ORBIS_SKILLS_*`.
- `data/skills/`: `index.json` + three seed markdown skills.

## Non-Goals

Auto skill creation, self-learning loop, skill-write approval UI, tool search,
subagents, vector search/embeddings, MCP, multi-channel gateway, Level 2 skill
assets (extension point only), dynamic mid-run skill changes, and production
server `Close()` wiring.

## Data Model

### Skill (`internal/skill`)

- `Metadata`: `id`, `name`, `title`, `description`, `tags`, `triggers`, `path`,
  `version`, `status`, `priority`, `related_tools` (mirrors `index.json`).
- `Entry`: `Metadata` + `Body`, `ContentHash` (sha256), `Chars` (rune count).
- `Index.Snapshot() []Entry` (immutable copy), `Bodies.Body(id) (string, bool)`.

### SkillRef (`internal/domain`)

`ID`, `Name`, `Version`, `Path`, `ContentHash`, `Chars` — the stable reference
shared by reducer, run snapshot, and dispatch payload (metadata only).

### Events

`SkillSelected` (score, reason), `SkillLoaded` (content_hash, chars),
`SkillApplied` (skill_ids, count, total_chars), `SkillSkipped`.
`SkillIndexLoaded`, `SkillIndexSearchStarted`, `SkillSelectionFailed` are reserved
(defined, not emitted; cf. `ToolCallProposed`).

### Storage (`data/skills/`)

`index.json` (Level 0 metadata array) + markdown bodies (Level 1). The store
loads both into memory once and on reload. `.gitignore` ignores `data/*` but
re-includes `data/skills/` so only seed skills are committed.

## Flow

```
UserMessageReceived
  -> reducer (reduceUserMessage): select from in-memory snapshot (pure, no I/O)
       set state.SelectedSkills; emit SkillSelected/Loaded per skill + SkillApplied
       (or SkillSkipped); put []SkillRef in DispatchLLMCall payload
  -> dispatcher (dispatchLLMCall): resolve bodies from store, BuildContext ->
       LLMRequest.Instructions; rest of the LLM flow unchanged
  -> lane (saveRunState): snapshot SelectedSkills once into data/runs/{id}.json
  -> tool result -> reduceToolCallSucceeded: reuse state.SelectedSkills (no re-select)
```

## Selection

Pure scoring on lowercased query text by substring match: trigger 10×n, name 6,
tag 5×n, related_tool 4×n, title 3. Non-`active` status skipped. Sort by score,
then priority (desc), then id (asc). Keep top `MaxSelected` within `MaxChars`
(stop at first overflow). No match → empty. Description is not free-text matched.
Selection once per run; reused on tool-result follow-ups.

## Configuration

`ORBIS_SKILLS_ENABLED` (true), `ORBIS_SKILLS_DIR` (`data/skills`),
`ORBIS_SKILLS_MAX_SELECTED` (3), `ORBIS_SKILLS_MAX_CHARS` (12000),
`ORBIS_SKILLS_RELOAD_ON_START` (true). Disabled → reducer skips selection
(byte-identical to v0.2); the app tests inject no store, so they stay disabled.

## API

WebSocket (via `HandleClientRequest`): `skill.list`, `skill.get`
(`{skill_id}`), `skill.reload`. HTTP (via `gateway.WithSkills`, enabled only):
`GET /skills`, `GET /skills/{skillID}` (404 unknown), `POST /skills/reload`
(500 on error). One implementation on `RuntimeService` (`ListSkills`/`GetSkill`/
`ReloadSkills`) backs both; `gateway` and `protocol` do not import `skill`.

## Reducer Purity

`BuildLLMMessages` and selection are pure functions called inside the reducer.
The reducer never touches disk, an LLM, or a goroutine. The store (load/reload)
is the only disk I/O; the dispatcher (worker boundary) is the only place bodies
are read for rendering.

## Runtime Quiescence

`RuntimeService.Close()` was added (during this milestone) to drain background
session-queue and dispatch goroutines via a `WaitGroup` gated by a `closing`
flag, so tests `defer Close()` and background store writes finish before
`t.TempDir` cleanup — removing a pre-existing flake. Atomic snapshot writes were
considered and rejected (they worsened the cleanup race).

## Edge Cases

- A `reload` mid-run does not alter an in-flight run's selected skills; the run's
  `content_hash` records what was applied.
- Skills disabled or store absent → selection skipped, `/skills` HTTP routes 404,
  WS `skill.*` returns a clear error / empty list (nil-safe).
- Unknown skill id → WS error, HTTP 404.

## Testing Requirements

Deterministic skill unit tests (index load, invalid index, missing/empty body,
selector trigger/tag/related-tool/maxSelected/maxChars/no-match/inactive,
BuildContext, seed-skill end-to-end). Reducer tests (select + events +
SelectedSkills, skip when disabled, SkillSkipped on no match, reuse on tool
success). Dispatcher Instructions injection. Lane run snapshot. Gateway HTTP
list/get/404/reload/500/not-registered. App WS list/get/reload + SkillApplied
publication. Manual/e2e uses the real LLM via `orbis ws smoke skill`.

## Open Questions

- Resolved in v1.5: enabled related tool names influence scoring through the
  `tool_available` reason.
- Resolved in v2/v2.1: reads remain open, while reload requires the `admin`
  role; see `v2.1-learning-loop-hardening.md`.
