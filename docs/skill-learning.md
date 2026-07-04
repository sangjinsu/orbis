# Skill Learning (v2)

Orbis v2 adds a **reviewable skill learning loop**: the runtime can derive a
**Skill Proposal** from a completed run, but a proposal only becomes an active
skill through an explicit, admin-authenticated human approval. Nothing is ever
promoted automatically.

The three states are strictly distinct:

- **Skill** — active procedural knowledge under `data/skills/`, selected and
  injected as described in [skills.md](skills.md). Skills never execute side
  effects.
- **Skill Proposal** — a reviewable candidate under `data/skill_proposals/`.
  It is *not* an active skill: `SkillProposalCreated != SkillPromoted`.
- **Promoted skill** — a proposal that a human approved; promotion writes it
  into `data/skills/` with version and provenance and reloads the index.

## Lifecycle

```
RunCompleted
  -> SkillCandidateDetected      (deterministic detector, no LLM)
  -> SkillProposalCreated        (pending, under data/skill_proposals/pending/)
  -> SkillReviewRequired
       │
       ├─ approve (admin) ─▶ SkillProposalApproved -> SkillPromoted
       │                     -> SkillIndexReloadRequested -> SkillIndexReloaded
       │                     -> SkillAuditRecorded
       │     └─ on failure ─▶ SkillPromotionFailed -> SkillAuditRecorded
       │                       (proposal marked failed; may be retried)
       └─ reject (admin)  ─▶ SkillProposalRejected -> SkillAuditRecorded
```

The status machine has **no pending → promoted path**: only a pending proposal
can be approved or rejected, and only an approved (or previously failed)
proposal can be promoted. This is enforced by the proposal store itself.

## Proposal creation

Proposals are created from a run's persisted data — the run snapshot, the
session event log, and the selected skills. Creation is **deterministic**: no
LLM is consulted, and no hidden reasoning is stored. The proposal carries the
reviewable sections (purpose, when-to-use, procedure, related tools,
verification, pitfalls), a rendered markdown body (exactly what would be
promoted), a concise `rationale_summary`, and the source run/event ids.

Two triggers exist:

- **Manual** (primary): `POST /runs/{runID}/skill-proposals` or WS
  `skill.proposal.create_from_run` — admin-gated.
- **Auto-propose** (optional, `ORBIS_SKILL_AUTO_PROPOSE`, default **false**): a
  post-run hook creates a pending proposal from qualifying completed runs. It
  only ever creates; it never approves or promotes.

The detector qualifies a completed run when it used tools or skills, recovered
from a tool failure, repeated a tool procedure, or the proposal was explicitly
requested. Non-candidates are refused (manual) or silently skipped (auto).

Proposal ids are deterministic per run (`prop_<runID>`), so repeated requests
for the same run are rejected as duplicates.

## Review queue

Proposals live under `data/skill_proposals/` in review buckets:

```
data/skill_proposals/
├── pending/     # awaiting review
├── approved/    # approved, promoted, or failed (status field disambiguates)
└── rejected/
```

The JSON `status` field is the source of truth; the directory is the coarse
queue. Statuses: `pending`, `approved`, `rejected`, `promoted`, `failed`.

## Approval, rejection, promotion

Approving a pending proposal (admin-gated) runs the full flow: the proposal is
marked approved, the promoter writes `data/skills/{skill_id}.md` and appends an
`index.json` entry, the proposal is marked promoted, and the in-memory skill
index reloads so the new skill is immediately selectable. Rejection marks the
proposal rejected and stops there.

A promoted skill's index entry records:

- `version` (initial `1`), `status: active`, `tags: [learned]`
- `priority: 50` — below the curated seed skills, so a learned skill never
  outranks hand-written guidance on ties
- provenance: `source_proposal_id`, `source_run_id`, `created_at`
- the content hash is recorded on the promoted proposal and re-derived as the
  entry's hash at load (same body, same derivation)

**Versioning/conflicts:** v2 rejects promotion when the skill id already exists
(`ErrSkillConflict`); the proposal is marked `failed` and can be retried after
the conflict is resolved. Multi-version promotion is future work.

## Audit

Every lifecycle transition appends a JSONL record to
`data/audit/skill_audit.jsonl`:

```json
{"audit_id":"...","event_type":"SkillPromoted","proposal_id":"...","skill_id":"...","source_run_id":"...","actor":"admin","timestamp":"...","summary":"proposal promoted to skill ..."}
```

Actors are `system`, `admin`, `developer`, or `unknown`. Audit records carry a
short user-visible summary only — never secrets or hidden reasoning.

## Events

WebSocket subscribers of the source run's session observe the lifecycle:

- Creation: `SkillCandidateDetected` → `SkillProposalCreated` → `SkillReviewRequired`
- Approval: `SkillProposalApproved` → `SkillPromoted` →
  `SkillIndexReloadRequested` → `SkillIndexReloaded` → `SkillAuditRecorded`
- Rejection: `SkillProposalRejected` → `SkillAuditRecorded`
- Promotion failure: `SkillPromotionFailed` → `SkillAuditRecorded`

Payloads carry metadata only (`proposal_id`, `skill_id`, `status`, `reason`,
`error`) — never proposal bodies.

## HTTP endpoints

```
GET  /skill-proposals?status=pending        # list (open)
GET  /skill-proposals/{proposalID}          # detail incl. body (open), 404 unknown
POST /runs/{runID}/skill-proposals          # create from run (admin), 201
POST /skill-proposals/{proposalID}/approve  # approve + promote + reload (admin)
POST /skill-proposals/{proposalID}/reject   # reject, body {"reason": "..."} (admin)
POST /skills/reload                         # reload the index (admin as of v2)
```

## WebSocket methods

```
skill.proposal.list             params: {"status": "..."}            (open)
skill.proposal.get              params: {"proposal_id": "..."}       (open)
skill.proposal.create_from_run  params: {"run_id","token"}           (admin)
skill.proposal.approve          params: {"proposal_id","token"}      (admin)
skill.proposal.reject           params: {"proposal_id","reason","token"} (admin)
skill.reload                    params: {"token"}                    (admin as of v2)
```

## Admin protection

Mutating operations require the static admin token:

- HTTP: `Authorization: Bearer <ORBIS_ADMIN_TOKEN>`; WS: `token` in params.
- **With no token configured (the default), mutating endpoints are disabled
  entirely** — 403 over HTTP, a clear error over WS. Reads stay open.
- A wrong token is 401 / an `invalid admin token` error.
- This also applies to the previously open v1 skills reload.

For local development set `ORBIS_ADMIN_TOKEN=dev-orbis-admin`.

## Configuration

- `ORBIS_SKILL_LEARNING_ENABLED` (default `true`) — disables the whole loop.
- `ORBIS_SKILL_PROPOSALS_DIR` (default `data/skill_proposals`)
- `ORBIS_SKILL_AUDIT_PATH` (default `data/audit/skill_audit.jsonl`)
- `ORBIS_ADMIN_TOKEN` (default empty = mutating endpoints disabled)
- `ORBIS_SKILL_AUTO_PROPOSE` (default `false`; creates pending proposals only)

## Manual test

```bash
go run ./cmd/orbis serve            # .env with a real provider + ORBIS_ADMIN_TOKEN
go run ./cmd/orbis ws smoke tool    # completes a run that used tools + skills

curl -X POST http://localhost:8080/runs/run_smoke_msg/skill-proposals \
  -H "Authorization: Bearer dev-orbis-admin"
curl http://localhost:8080/skill-proposals?status=pending
curl -X POST http://localhost:8080/skill-proposals/prop_run_smoke_msg/approve \
  -H "Authorization: Bearer dev-orbis-admin"
curl http://localhost:8080/skills    # promoted skill served by the reloaded catalog
```

Then inspect `data/skill_proposals/`, `data/skills/index.json`, and
`data/audit/skill_audit.jsonl`.

## Limitations (v2)

- Proposal bodies are deterministic templates rendered from run data; there is
  no LLM authoring and no edit-before-approve (future work).
- Same-id promotion is rejected instead of versioned (multi-version is future
  work).
- Lifecycle events are scoped to the source run's session; the standalone
  `/skills/reload` emits no events.
- The admin token is a single static bearer for local development, not a full
  auth system.

## Non-goals

Fully automatic promotion, automatic long-term memory writes, LLM-based
autonomous detection, subagents, tool search, vector search/embeddings, MCP,
multi-channel gateways, and unreviewed self-modification remain out of scope.
