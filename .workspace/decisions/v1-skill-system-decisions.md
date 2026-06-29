# Decision: v1 Skill System & Context Builder

Date: 2026-06-29
Status: accepted

## Context

v1 introduces skills (reusable procedural knowledge injected into the LLM context
before planning) while keeping the reducer pure and the runtime in control. The
hard constraint is that `BuildLLMMessages` and selection run inside the reducer,
which may not perform disk I/O, call an LLM, or start goroutines. Several design
choices had multiple valid options.

## Decision

1. The skill index and all bodies are loaded into memory once at startup (and on
   reload). The store is the only skill disk-I/O point; the reducer selects from
   an immutable in-memory snapshot, so selection stays pure.
2. Selection is a deterministic, LLM-free scoring function over substring matches
   (trigger > name > tag > related_tool > title), bounded by `MaxSelected` and a
   `MaxChars` budget, with `priority` then `id` tiebreaks.
3. The reducer emits the skill lifecycle events (`SkillSelected`/`SkillLoaded`/
   `SkillApplied`/`SkillSkipped`), carrying metadata only. The dispatcher only
   renders bodies into `Instructions`. Reducer-emitted events guarantee they
   precede `LLMCallStarted` and keep ordering correct.
4. Selection happens once per run; tool-result follow-up LLM calls reuse the
   run's `SelectedSkills` (no re-selection, no re-emission) for prompt stability.
5. The per-run selection is snapshotted once into `data/runs/{id}.json`, so the
   record survives a later index reload (`content_hash` is the drift marker).
6. `data/skills/**` is committed via a `.gitignore` re-include exception, while
   the rest of `data/` stays ignored runtime state.
7. The skill store lives in `internal/skill` (a specialized in-memory cache), not
   in the generic `internal/store`.
8. Both WS and HTTP skill APIs are served by one implementation on
   `RuntimeService`. `protocol` exposes plain wire DTOs and `gateway`/`protocol`
   do not import `skill`; the metadata→wire mapping lives in the app layer.
9. Skills are read-only over the API (list/get/reload). They are never executed
   and never edited by the runtime.
10. `RuntimeService.Close()` (graceful shutdown draining background goroutines)
    was added to fix a pre-existing test-quiescence flake; atomic snapshot writes
    were rejected because they worsened the `t.TempDir` cleanup race.

## Rationale Summary

- In-memory index + pure reducer selection is the only way to honor reducer
  purity while making selection deterministic and testable.
- Emitting events from the reducer (not the dispatcher) keeps the event order and
  the run snapshot coherent and metadata-only.
- One implementation behind two surfaces avoids drift between WS and HTTP; plain
  wire DTOs keep the contract stable as the internal store evolves.
- Quiescence (not atomic writes) is the correct fix because the root cause is
  background goroutines outliving tests, not torn writes.

## Alternatives Considered

- LLM-driven selection: rejected for v1 (nondeterministic, latency, cost);
  deterministic scoring is predictable and free.
- Dispatcher-emitted skill events: rejected; the dispatcher runs in a worker
  goroutine and cannot guarantee ordering before `LLMCallStarted`.
- Routing HTTP skill endpoints through `HandleClientRequest`: rejected in favor
  of a `gateway.Skills` interface for clean REST semantics (404/500).
- Atomic temp+rename snapshot writes: rejected; measured worse cleanup races.
- Free-text description matching: rejected for v1 to avoid spurious selections.

## Consequences

- Easier: deterministic selection tests, observable skill lifecycle, reducer
  purity intact, reliable `go test ./...` after quiescence.
- Harder: a mid-run reload cannot affect an in-flight run (documented limit);
  real-LLM behavior is verified via the smoke path, not asserted exactly.

## Follow-ups

- [ ] Real-LLM acceptance: `orbis ws smoke skill` and the documented prompts.
- [ ] Wire `RuntimeService.Close()` into HTTP server shutdown.
- [ ] Consider scoring by available tool names (`SelectionInput.ToolNames`).
- [ ] v1.5/v2: auto creation, learning loop, vector search, subagents, MCP,
      Level 2 assets.
