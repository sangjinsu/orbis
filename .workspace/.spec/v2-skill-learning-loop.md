# v2 Skill Learning Loop

## Status

Accepted

## Historical Supersession Note

This accepted spec preserves the v2 learning-loop contract. The v2 gaps for a
single static admin, edit-before-approve, multi-version promotion, and
session-independent lifecycle events were resolved by v2.1. See
`v2.1-learning-loop-hardening.md` for current behavior.

## Purpose

Introduce a safe, reviewable learning loop for skills. The runtime derives
Skill Proposals from completed runs; a proposal only becomes an active skill
through an explicit, admin-authenticated human approval. The core invariant is
`SkillProposalCreated != SkillPromoted`: nothing is promoted automatically, and
the proposal lifecycle has no pending→promoted transition.

## Scope

- `internal/skill`: `SkillProposal` model + `SkillProposalStatus` +
  `CanTransition` lifecycle; file-based `ProposalStore`
  (`data/skill_proposals/{pending,approved,rejected}`); JSONL `AuditLog`;
  versioning helpers (shared `contentHash`, `normalizeVersion`,
  `ErrSkillConflict`); deterministic `BuildRunFacts`/`DetectCandidate`
  detector; deterministic `NewProposalFromRun` renderer; `Promoter` writing
  `data/skills/{id}.md` + `index.json` entries with provenance.
- `internal/app/skill_learning.go`: the learning service on `RuntimeService` —
  create-from-run, approve (promote + reload), reject, list/get; audit appends;
  lifecycle event emission via `Enqueue`; `requireAdmin`; WS handlers.
- `internal/app/runtime.go`: config/fields wiring, WS method dispatch, optional
  auto-propose hook on `RunCompleted` (tracked goroutine; create-only).
- `internal/gateway`: `SkillLearning` interface, `WithSkillLearning`,
  `WithAdmin`, proposal routes, admin gate on `/skills/reload`.
- `internal/protocol`: proposal wire DTOs.
- `internal/config`: `ORBIS_SKILL_LEARNING_ENABLED`, `ORBIS_SKILL_PROPOSALS_DIR`,
  `ORBIS_SKILL_AUDIT_PATH`, `ORBIS_ADMIN_TOKEN`, `ORBIS_SKILL_AUTO_PROPOSE`.
- `internal/domain`: ten v2 skill-learning event constants.

## Non-Goals

Fully automatic promotion, automatic long-term memory writes, LLM-based
autonomous detection, subagents, tool search, multi-channel gateway,
distributed workers/MSA/K8s, MCP, vector DB/embeddings, unreviewed
self-modification, edit-before-approve, multi-version promotion.

## Data Model

### SkillProposal (`internal/skill/proposal.go`)

`proposal_id`, `source_run_id`, `source_event_ids`, `title`, `skill_id`,
`purpose`, `when_to_use`, `required_context`, `procedure`, `related_tools`,
`verification`, `pitfalls`, `rationale_summary` (concise, user-visible; never
hidden reasoning), rendered markdown `body` (what promotion writes),
`status`, timestamps (`created/updated/approved/rejected`),
`promoted_skill_id`, `version`, `content_hash` (sha256 of body, same
derivation as the loader).

Statuses: `pending`, `approved`, `rejected`, `promoted`, `failed`.
Transitions: pending→approved|rejected; approved→promoted|failed;
failed→promoted|failed; rejected/promoted terminal. Enforced by
`CanTransition` and by `ProposalStore.Update`.

### Storage

```
data/skill_proposals/{pending,approved,rejected}/{sanitized_id}.json
data/audit/skill_audit.jsonl
data/skills/{skill_id}.md + index.json   (promotion target)
```

The JSON status field is the source of truth; the bucket is the review queue
(promoted/failed stay under approved/). File names are sanitized with a short
hash suffix (mirrors tool-call records). Runtime data never goes to
`.workspace`.

### Promoted skill index entry

`id=name=skill_id`, `title`, `description=purpose`, `tags:[learned]`,
`path:{id}.md`, `version` (initial "1"), `status:active`, `priority:50`
(below curated seeds), `related_tools`, and provenance
(`source_proposal_id`, `source_run_id`, `created_at`) on `skill.Metadata`
(empty for seeds). Content hash is recorded on the proposal and re-derived as
`Entry.ContentHash` at load — not duplicated in the index.

### Audit record

`audit_id`, `event_type`, `proposal_id`, `skill_id`, `source_run_id`,
`actor` (system|admin|developer|unknown), `timestamp`, `summary`. One record
per lifecycle transition (created, approved, promoted or promotion-failed,
rejected). No secrets, no hidden reasoning.

### Events

`SkillCandidateDetected`, `SkillProposalCreated`, `SkillReviewRequired`,
`SkillProposalApproved`, `SkillProposalRejected`, `SkillPromoted`,
`SkillPromotionFailed`, `SkillIndexReloadRequested`, `SkillIndexReloaded`,
`SkillAuditRecorded`. Emitted by the app-layer service (never the reducer)
through `Enqueue`, so they are sequenced, persisted, and published to the
source run's session subscribers. Payloads are metadata only.

## Flow

```
manual: POST /runs/{id}/skill-proposals | WS skill.proposal.create_from_run (admin)
auto:   RunCompleted hook (ORBIS_SKILL_AUTO_PROPOSE=false by default; tracked goroutine)
  -> load run snapshot + session event log (store)
  -> BuildRunFacts -> DetectCandidate (deterministic; completed + tools|skills|
     recovered-failure|repeated-procedure|explicit-request)
  -> NewProposalFromRun (deterministic markdown; ASCII slug skill id
     learned-<goal>-<runhash>) -> ProposalStore.Create (pending)
  -> audit(created) -> emit Candidate/Created/ReviewRequired

approve (admin): pending -> approved (audit+event)
  -> Promoter.Promote: conflict check (index) -> write body -> append index
     -> promoted (audit+event) -> skills.Reload()
     (ReloadRequested/Reloaded events) -> AuditRecorded event
  on promote error: proposal failed (audit) -> PromotionFailed + AuditRecorded

reject (admin): pending -> rejected (audit) -> Rejected + AuditRecorded
```

## Reducer Purity

The entire loop lives in the app layer: proposal creation, detection,
promotion, audit, and reload are disk I/O and are therefore never performed by
the reducer. The reducer and dispatcher are untouched by v2.

## APIs

HTTP: `GET /skill-proposals(?status=)`, `GET /skill-proposals/{id}` (404),
`POST /runs/{runID}/skill-proposals` (201), `POST
/skill-proposals/{id}/approve|reject`, `POST /skills/reload`. WS:
`skill.proposal.list/get/create_from_run/approve/reject`, `skill.reload`.
One implementation on `RuntimeService` serves both; `gateway`/`protocol` do
not import `skill`.

## Admin Protection

Mutating operations (proposal create/approve/reject and skills reload — the
latter previously open in v1) require `ORBIS_ADMIN_TOKEN`: HTTP
`Authorization: Bearer <token>`, WS `token` param. **No token configured ⇒
mutating endpoints disabled entirely** (HTTP 403 / WS error); wrong token ⇒
401 / error. Reads stay open. Actors: HTTP/WS mutations = `admin`; auto hook =
`system`.

## Edge Cases

- Duplicate proposal for a run: deterministic id `prop_<runID>` ⇒ rejected as
  already-exists.
- Non-candidate run: manual create errors with the detector reason; auto hook
  skips silently.
- Promotion conflict (existing skill id) or unsafe id: proposal → `failed`
  (retryable), `SkillPromotionFailed` emitted; index untouched.
- Skills disabled (no promoter/catalog): approval fails promotion cleanly.
- Source run unresolvable: approve/reject fail before any mutation.
- Runtime closing: late lifecycle events are dropped by `Enqueue`
  (errRuntimeClosed), surfaced as an operation error.

## Testing Requirements

Deterministic unit tests: proposal store create/get/list/transitions (illegal
transitions rejected — no pending→promoted), audit JSONL, versioning/conflict,
detector table, facts-from-events, renderer determinism + ASCII slug,
promoter (bootstrap/append/conflict/unsafe id). Service tests: create-from-run
(events + audit + duplicate), auto-propose on/off, approve end-to-end (event
order, files, reload, learned skill selectable via tool availability, re-approve
rejected), reject flow, conflict→failed, WS/HTTP admin matrices. Regression:
v1 selection, v0.2 tools, v1.5 shutdown all green. Manual/e2e against the real
LLM: run → propose → approve → promoted skill served after reload.

## Open Questions

- Resolved in v2.1: learned skills support in-place integer version bumps with
  archived prior bodies.
- Resolved in v2.1: reviewers can edit pending proposals before approval.
- Resolved in v2.1: skill lifecycle and standalone reload events fan out to a
  live global feed.
