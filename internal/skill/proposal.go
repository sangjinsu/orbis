package skill

import (
	"fmt"
	"strings"
	"time"
)

// SkillProposalStatus is the review state of a skill proposal. A proposal is
// never an active skill: only an explicit approval followed by promotion turns
// it into one (SkillProposalCreated != SkillPromoted).
type SkillProposalStatus string

const (
	ProposalPending  SkillProposalStatus = "pending"
	ProposalApproved SkillProposalStatus = "approved"
	ProposalRejected SkillProposalStatus = "rejected"
	ProposalPromoted SkillProposalStatus = "promoted"
	ProposalFailed   SkillProposalStatus = "failed"
)

// SkillProposal is a reviewable candidate skill derived from a run. It carries
// the structured sections a reviewer needs plus the rendered markdown Body that
// would become the skill file on promotion. RationaleSummary is a concise,
// user-visible summary only — hidden chain-of-thought is never stored.
type SkillProposal struct {
	ProposalID       string              `json:"proposal_id"`
	SourceRunID      string              `json:"source_run_id"`
	SourceEventIDs   []string            `json:"source_event_ids,omitempty"`
	Title            string              `json:"title"`
	SkillID          string              `json:"skill_id"`
	Purpose          string              `json:"purpose,omitempty"`
	WhenToUse        string              `json:"when_to_use,omitempty"`
	RequiredContext  []string            `json:"required_context,omitempty"`
	Procedure        []string            `json:"procedure,omitempty"`
	RelatedTools     []string            `json:"related_tools,omitempty"`
	Verification     []string            `json:"verification,omitempty"`
	Pitfalls         []string            `json:"pitfalls,omitempty"`
	RationaleSummary string              `json:"rationale_summary,omitempty"`
	Body             string              `json:"body"`
	Status           SkillProposalStatus `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	ApprovedAt       *time.Time          `json:"approved_at,omitempty"`
	RejectedAt       *time.Time          `json:"rejected_at,omitempty"`
	PromotedSkillID  string              `json:"promoted_skill_id,omitempty"`
	Version          string              `json:"version,omitempty"`
	ContentHash      string              `json:"content_hash,omitempty"`
}

// Validate checks the fields every stored proposal must carry.
func (p SkillProposal) Validate() error {
	if strings.TrimSpace(p.ProposalID) == "" {
		return fmt.Errorf("proposal_id is required")
	}
	if strings.TrimSpace(p.SourceRunID) == "" {
		return fmt.Errorf("proposal %q: source_run_id is required", p.ProposalID)
	}
	if strings.TrimSpace(p.SkillID) == "" {
		return fmt.Errorf("proposal %q: skill_id is required", p.ProposalID)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("proposal %q: title is required", p.ProposalID)
	}
	if strings.TrimSpace(p.Body) == "" {
		return fmt.Errorf("proposal %q: body is required", p.ProposalID)
	}
	switch p.Status {
	case ProposalPending, ProposalApproved, ProposalRejected, ProposalPromoted, ProposalFailed:
		return nil
	default:
		return fmt.Errorf("proposal %q: unknown status %q", p.ProposalID, p.Status)
	}
}

// CanTransition reports whether a proposal may move from one review state to
// another. It encodes the reviewable lifecycle: only a pending proposal can be
// approved or rejected, only an approved (or previously failed) proposal can be
// promoted, and rejected/promoted are terminal. There is no path from pending
// straight to promoted, which is what makes automatic promotion impossible.
func CanTransition(from, to SkillProposalStatus) bool {
	switch from {
	case ProposalPending:
		return to == ProposalApproved || to == ProposalRejected
	case ProposalApproved:
		return to == ProposalPromoted || to == ProposalFailed
	case ProposalFailed:
		return to == ProposalPromoted || to == ProposalFailed
	default:
		return false
	}
}
