# Decision: v2 Reviewable Skill Learning Loop

Date: 2026-07-04
Status: accepted

## Context

v2 lets the runtime learn skills from completed runs without ever
self-modifying unreviewed: proposals are created deterministically and a human
must approve each one before it becomes an active skill. Several design choices
had multiple valid options.

## Decision

1. The whole learning loop lives in the app layer (`RuntimeService` +
   `internal/skill` helpers). Creation, detection, promotion, audit, and reload
   are disk I/O, so none of it runs in the reducer; the reducer and dispatcher
   are untouched by v2.
2. Proposal creation and candidate detection are deterministic (run snapshot +
   event log in, same proposal out). No LLM authoring in v2; the proposal body
   is a rendered template and `rationale_summary` is a concise, user-visible
   sentence — hidden chain-of-thought is never stored.
3. Manual creation is the primary trigger; `ORBIS_SKILL_AUTO_PROPOSE` (default
   false) may create pending proposals from qualifying runs but can never
   approve or promote.
4. The lifecycle is enforced structurally: `CanTransition` has no
   pending→promoted path and the proposal store rejects illegal transitions, so
   "no promotion without approval" is a type/store-level guarantee, not a
   convention.
5. Promotion rejects an existing skill id (`ErrSkillConflict`) instead of
   versioning; the proposal is marked `failed` and may be retried. Multi-version
   promotion is deferred to v2.1.
6. Approval auto-reloads the in-memory index after promotion
   (SkillPromoted → SkillIndexReloaded), so a promoted skill is immediately
   selectable; the standalone protected reload endpoint remains available.
7. Mutating operations require a static admin bearer token
   (`ORBIS_ADMIN_TOKEN`); with no token configured they are **disabled
   entirely** (HTTP 403) rather than left open. Reads stay open. The previously
   open v1 skills reload is now behind the same gate (documented breaking
   change).
8. Promoted skills carry provenance (`source_proposal_id`, `source_run_id`,
   `created_at`) on `skill.Metadata`, get `tags:[learned]`, and priority 50 —
   below curated seeds — so learned knowledge never outranks hand-written
   guidance on ties. The content hash is not duplicated into the index: it is
   recorded on the proposal and re-derived at load.
9. Review buckets on disk are coarse (`pending/approved/rejected`, with
   promoted/failed staying under approved/); the JSON status field is the
   source of truth.
10. Proposal ids are deterministic per run (`prop_<runID>`), making duplicate
    creation requests idempotent-by-rejection.
11. Lifecycle events are emitted through `Enqueue` into the source run's
    session, reusing sequencing/persistence/broker publication; audit records
    are appended for every transition, with one `SkillAuditRecorded` event
    closing each mutating flow.

## Rationale Summary

- App-layer service is the only placement compatible with reducer purity.
- Determinism + human approval is what makes the loop *reviewable*: the value
  of v2 is the gate, not automated authoring.
- Disabling mutations without a token is the safe default for an unauthenticated
  gateway; a static bearer is enough for local development.
- Reject-on-conflict keeps the index writer trivial and auditable; versioning
  can be layered on later without changing the review gate.

## Alternatives Considered

- Reducer-emitted lifecycle events: rejected — creation/promotion are I/O and
  cannot originate in the reducer without breaking purity.
- LLM-drafted proposal bodies: rejected for v2 — nondeterministic, harder to
  review, and explicitly deferred by the spec unless pre-approved.
- Auto-promote behind a config flag: rejected — the spec forbids autonomous
  promotion outright.
- Versioned promotion on conflict: deferred (v2.1) in favor of reject-on-conflict.
- Leaving reload open as in v1: rejected — reload mutates what the LLM sees and
  belongs behind the same admin gate as promotion.

## Consequences

- Easier: fully deterministic tests for the whole loop; a real audit trail;
  learned skills immediately selectable after approval (tool-availability
  scoring already picks them up).
- Harder: same-goal re-learning hits id conflicts until versioning lands;
  reviewers cannot edit a proposal before approving (approve-or-reject only);
  v1 reload callers must now supply the admin token.

## Follow-ups

- [ ] v2.1: multi-version promotion; reviewer edits before approval.
- [ ] Session-independent (global) reload events.
- [ ] Real auth to replace the static admin token when the gateway grows
      authentication.
