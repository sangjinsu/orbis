package protocol

import (
	"encoding/json"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

type ClientRequest struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type ServerResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type RuntimeEvent struct {
	Type      string          `json:"type"`
	Event     string          `json:"event"`
	Seq       int64           `json:"seq"`
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id"`
	Payload   json.RawMessage `json:"payload"`
}

type AckPayload struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id"`
}

type SessionPayload struct {
	SessionID string `json:"session_id"`
}

type RunStatusPayload struct {
	RunID     string           `json:"run_id"`
	SessionID string           `json:"session_id"`
	Status    domain.RunStatus `json:"status"`
}

type EventsListPayload struct {
	Events []RuntimeEvent `json:"events"`
}

// SkillSummary is the wire view of a skill's index metadata, returned by the
// skill list/get APIs. It mirrors skill.Metadata as plain fields so the wire
// contract stays decoupled from the internal store representation.
type SkillSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Triggers     []string `json:"triggers,omitempty"`
	Version      string   `json:"version,omitempty"`
	Status       string   `json:"status,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	RelatedTools []string `json:"related_tools,omitempty"`
}

// SkillListPayload is the response to skill.list / GET /skills.
type SkillListPayload struct {
	Skills []SkillSummary `json:"skills"`
}

// SkillDetailPayload is the response to skill.get / GET /skills/{id}; it adds the
// loaded body and its derived hash and character count to the summary.
type SkillDetailPayload struct {
	SkillSummary
	Body        string `json:"body"`
	ContentHash string `json:"content_hash,omitempty"`
	Chars       int    `json:"chars,omitempty"`
}

// SkillReloadPayload is the response to skill.reload / POST /skills/reload; Count
// is the number of skills available after the reload.
type SkillReloadPayload struct {
	Count int `json:"count"`
}

// SkillProposalSummary is the wire view of a reviewable skill proposal (v2).
// A proposal is never an active skill: it must be explicitly approved and
// promoted first.
type SkillProposalSummary struct {
	ProposalID       string    `json:"proposal_id"`
	SourceRunID      string    `json:"source_run_id"`
	SkillID          string    `json:"skill_id"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	RationaleSummary string    `json:"rationale_summary,omitempty"`
	Version          string    `json:"version,omitempty"`
	ContentHash      string    `json:"content_hash,omitempty"`
	PromotedSkillID  string    `json:"promoted_skill_id,omitempty"`
	Revision         int       `json:"revision,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SkillProposalUpdateRequest is a reviewer's partial edit of a pending
// proposal's structured fields (v2.1). A nil field is left unchanged; a
// non-nil field replaces the stored value. Only the fields that compose the
// rendered body are editable — the body and content hash are re-derived
// server-side, and identity fields (skill_id, source_run_id) and the detection
// rationale are not editable at all.
type SkillProposalUpdateRequest struct {
	Title           *string   `json:"title,omitempty"`
	Purpose         *string   `json:"purpose,omitempty"`
	WhenToUse       *string   `json:"when_to_use,omitempty"`
	RequiredContext *[]string `json:"required_context,omitempty"`
	Procedure       *[]string `json:"procedure,omitempty"`
	RelatedTools    *[]string `json:"related_tools,omitempty"`
	Verification    *[]string `json:"verification,omitempty"`
	Pitfalls        *[]string `json:"pitfalls,omitempty"`
}

// SkillProposalListPayload is the response to skill.proposal.list /
// GET /skill-proposals.
type SkillProposalListPayload struct {
	Proposals []SkillProposalSummary `json:"proposals"`
}

// SkillProposalDetailPayload adds the reviewable sections and the markdown body
// that would become the skill file on promotion.
type SkillProposalDetailPayload struct {
	SkillProposalSummary
	Purpose         string   `json:"purpose,omitempty"`
	WhenToUse       string   `json:"when_to_use,omitempty"`
	RequiredContext []string `json:"required_context,omitempty"`
	Procedure       []string `json:"procedure,omitempty"`
	RelatedTools    []string `json:"related_tools,omitempty"`
	Verification    []string `json:"verification,omitempty"`
	Pitfalls        []string `json:"pitfalls,omitempty"`
	SourceEventIDs  []string `json:"source_event_ids,omitempty"`
	Body            string   `json:"body"`
}
