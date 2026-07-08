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

**Versioning/conflicts:** promoting a proposal whose skill id already exists
bumps the learned skill in place (v2.1): the version increments (`"1"` → `"2"`),
the new body replaces `{skill_id}.md`, and the previous body is preserved as
`archive/{skill_id}@{old_version}.md`. The index keeps one entry per id — its
identity, path, tags, and priority are unchanged while the title, description,
related tools, and provenance refresh to describe the new version. Two cases
still fail the promotion (the proposal is marked `failed` and can be retried):

- the existing entry has no learned provenance (`source_proposal_id` is empty)
  — curated seed skills are never replaced (`ErrSkillConflict`)
- the existing learned entry carries a non-integer version — a corrupt entry
  fails loudly instead of guessing a next version

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
- Reviewer edit (v2.1): `SkillProposalUpdated` → `SkillAuditRecorded`
- Approval: `SkillProposalApproved` → `SkillPromoted` →
  `SkillIndexReloadRequested` → `SkillIndexReloaded` → `SkillAuditRecorded`
- Rejection: `SkillProposalRejected` → `SkillAuditRecorded`
- Promotion failure: `SkillPromotionFailed` → `SkillAuditRecorded`

Payloads carry metadata only (`proposal_id`, `skill_id`, `status`, `version`,
`reason`, `error`) — never proposal bodies.

**Global feed (v2.1):** the same lifecycle events also fan out to a
session-independent feed, so a reviewer does not need to know which run a
proposal came from. Subscribe with `session.subscribe` and
`{"scope": "global"}` (no `session_id`). The feed is a live stream: events are
persisted only on the source run's session, and missed global events are
recovered through `GET /skill-proposals`, not replay. The standalone reload
(`POST /skills/reload`, WS `skill.reload`) — previously silent — now emits
`SkillIndexReloadRequested` → `SkillIndexReloaded` on the global feed only
(payload: `actor`, `count`), since it has no session; the approve flow's
reload events stay on the source session and the global feed as before.

## HTTP endpoints

```
GET   /skill-proposals?status=pending        # list (open)
GET   /skill-proposals/{proposalID}          # detail incl. body (open), 404 unknown
POST  /runs/{runID}/skill-proposals          # create from run (admin), 201
PATCH /skill-proposals/{proposalID}          # edit a pending proposal (admin)
POST  /skill-proposals/{proposalID}/approve  # approve + promote + reload (admin)
POST  /skill-proposals/{proposalID}/reject   # reject, body {"reason": "..."} (admin)
POST  /skills/reload                         # reload the index (admin as of v2)
```

## WebSocket methods

```
skill.proposal.list             params: {"status": "..."}            (open)
skill.proposal.get              params: {"proposal_id": "..."}       (open)
skill.proposal.create_from_run  params: {"run_id","token"}           (admin)
skill.proposal.update           params: {"proposal_id","token",<fields>} (admin)
skill.proposal.approve          params: {"proposal_id","token"}      (admin)
skill.proposal.reject           params: {"proposal_id","reason","token"} (admin)
skill.reload                    params: {"token"}                    (admin as of v2)
```

## Reviewer edits (v2.1)

A pending proposal's structured fields can be edited before the approve/reject
decision: `title`, `purpose`, `when_to_use`, `required_context`, `procedure`,
`related_tools`, `verification`, `pitfalls`. Fields omitted from the request
are unchanged; provided fields replace the stored value (lists may be cleared
with `[]`, but `title` may not be emptied). The markdown body and content hash
are always re-derived server-side through the same renderer that created the
proposal, so an edited proposal can never drift from what promotion writes.

```
PATCH /skill-proposals/{proposalID}
Authorization: Bearer <token>
{"title": "Sharper title", "procedure": ["step one", "step two"]}
```

Not editable: `skill_id` and `source_run_id` (identity), `rationale_summary`
(the detection record), and `body` (always rendered). Each edit increments the
proposal's `revision`, appends a revision-keyed audit record naming the edited
fields, and emits `SkillProposalUpdated`. Only pending proposals can be
edited — approved, rejected, promoted, and failed proposals are immutable.

## Authentication (v2.1: named tokens + roles)

Mutating operations require a named bearer token with a sufficient role:

```
ORBIS_AUTH_TOKENS=alice:admin:atok,bob:reviewer:rtok
```

- Each entry is `name:role:token` (comma-separated; the token part may contain
  `:` but not `,`). Names and tokens must be unique.
- Roles: `reviewer` covers the proposal review flow (create_from_run, update,
  approve, reject); `admin` covers everything plus `skills reload`.
- The token's **name** is recorded as the audit `actor` and in the global
  reload event payload, so the trail shows who approved what.
- HTTP: `Authorization: Bearer <token>`; WS: `token` in each request's params.
- **With no tokens configured (the default), mutating endpoints are disabled
  entirely** — 403 over HTTP, a clear error over WS. Reads stay open.
- An unknown token is 401; a valid token without the required role is 403.
- Tokens are compared in constant time without early return, so response
  timing does not reveal which entry matched.

The legacy `ORBIS_ADMIN_TOKEN` still works: it is merged in as an admin-role
token named `admin`. Setting both is fine unless the name `admin` or the token
value collides — that fails config loading loudly. For local development set
`ORBIS_ADMIN_TOKEN=dev-orbis-admin` or a full `ORBIS_AUTH_TOKENS`.

## Configuration

- `ORBIS_SKILL_LEARNING_ENABLED` (default `true`) — disables the whole loop.
- `ORBIS_SKILL_PROPOSALS_DIR` (default `data/skill_proposals`)
- `ORBIS_SKILL_AUDIT_PATH` (default `data/audit/skill_audit.jsonl`)
- `ORBIS_AUTH_TOKENS` (default empty; `name:role:token` entries)
- `ORBIS_ADMIN_TOKEN` (legacy; merged as admin token named `admin`. Both empty
  = mutating endpoints disabled)
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
  no LLM authoring (reviewer edits of the structured fields landed in v2.1).
- Lifecycle events are persisted on the source run's session only; the global
  feed (v2.1) is a live stream without persistence or replay.
- Tokens are static credentials configured at startup (named, with roles, as
  of v2.1); there is no token rotation, expiry, or external identity provider.

## Non-goals

Fully automatic promotion, automatic long-term memory writes, LLM-based
autonomous detection, subagents, tool search, vector search/embeddings, MCP,
multi-channel gateways, and unreviewed self-modification remain out of scope.
