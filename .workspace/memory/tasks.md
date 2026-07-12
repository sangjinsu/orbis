# Task Memory

## Current Baseline

- `main` has shipped v0.1, v0.2, v1, v1.5, v2, and v2.1.
- Runtime invariants remain pure reducers, ordered session mutation, worker-owned side effects, idempotency, context cancellation, and observable events.
- v2.1 provides reviewable proposal edits, versioned learned-skill promotion, a live global lifecycle feed, and named reviewer/admin roles.
- The Cobra CLI exposes runtime operation, skills, proposals, global watch, WebSocket smoke, and interactive chat surfaces.
- Milestone completion narratives live in [`history.md`](history.md).

## Current Maintenance

- Keep `AGENTS.md`, accepted specs, decisions, and project memory aligned with shipped behavior.
- Preserve historical milestone contracts while adding explicit supersession notes.
- Keep runtime, gateway, CLI, persistence, and concurrency regression coverage green.

## Next Candidates

- No next product milestone is selected.
- Multi-entry learned-skill version history and proposal retention remain candidates only.
- Global-feed authorization may be reconsidered if its payload scope expands.
- Token rotation or an external identity provider may be considered when static tokens no longer suffice.

## Deferred/Non-goals

- Unreviewed automatic promotion or self-modification.
- Vector or semantic search.
- Subagents and MCP integration.
- Multi-channel gateways and distributed brokers or workers.
- Kubernetes deployment and full OpenClaw or Hermes compatibility.

## Release Gates

- `gofmt -l .` and `go vet ./...` are clean.
- `go test ./... -count=1` and `go test -race ./...` pass.
- `git diff --check` passes.
- Real-provider WebSocket smoke covers the affected runtime path when credentials are available.
- CLI help, argument validation, and affected command behavior are exercised.
