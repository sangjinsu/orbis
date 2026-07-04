package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
)

// errSkillLearningDisabled is returned when the learning loop is not wired
// (ORBIS_SKILL_LEARNING_ENABLED=false or no proposal store configured).
var errSkillLearningDisabled = errors.New("skill learning is not enabled")

// skillLearningEventPayload is the metadata-only payload of skill-learning
// lifecycle events. It never carries proposal bodies or hidden reasoning.
type skillLearningEventPayload struct {
	ProposalID string `json:"proposal_id"`
	SkillID    string `json:"skill_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

// CreateSkillProposalFromRun derives a reviewable skill proposal from a
// completed run. It reads the run snapshot and the session event log, runs the
// deterministic candidate detector, renders the proposal (no LLM), persists it
// as pending, appends an audit record, and emits the creation lifecycle events
// (SkillCandidateDetected -> SkillProposalCreated -> SkillReviewRequired). It
// never promotes: a proposal only becomes an active skill through an explicit
// approval.
func (s *RuntimeService) CreateSkillProposalFromRun(ctx context.Context, runID, actor string, explicit bool) (skill.SkillProposal, error) {
	if s.proposals == nil {
		return skill.SkillProposal{}, errSkillLearningDisabled
	}
	if runID == "" {
		return skill.SkillProposal{}, fmt.Errorf("run_id is required")
	}

	run, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return skill.SkillProposal{}, fmt.Errorf("load run: %w", err)
	}
	events, err := s.store.ListEvents(ctx, run.SessionID, store.ListEventsOptions{})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return skill.SkillProposal{}, fmt.Errorf("list run events: %w", err)
	}

	facts := skill.BuildRunFacts(run, events)
	facts.ExplicitRequest = explicit
	candidate, reason := skill.DetectCandidate(facts)
	if !candidate {
		return skill.SkillProposal{}, fmt.Errorf("run %s is not a skill candidate: %s", runID, reason)
	}

	proposalID := "prop_" + runID
	proposal := skill.NewProposalFromRun(proposalID, facts, reason, s.now())
	if err := s.proposals.Create(proposal); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.appendSkillAudit(string(domain.EventSkillProposalCreated), proposal, actor, "proposal created: "+reason); err != nil {
		return skill.SkillProposal{}, err
	}

	payload := skillLearningEventPayload{
		ProposalID: proposal.ProposalID,
		SkillID:    proposal.SkillID,
		Status:     string(proposal.Status),
		Reason:     reason,
	}
	for _, step := range []struct {
		suffix string
		typ    domain.EventType
	}{
		{":skill_candidate", domain.EventSkillCandidateDetected},
		{":skill_proposal_created", domain.EventSkillProposalCreated},
		{":skill_review_required", domain.EventSkillReviewRequired},
	} {
		if err := s.emitSkillLearningEvent(ctx, run.SessionID, runID, runID+step.suffix, step.typ, payload); err != nil {
			return skill.SkillProposal{}, err
		}
	}
	return proposal, nil
}

// appendSkillAudit writes one audit record for a proposal lifecycle transition.
// The summary is a short user-visible sentence; no secrets, no hidden reasoning.
func (s *RuntimeService) appendSkillAudit(eventType string, proposal skill.SkillProposal, actor, summary string) error {
	if s.auditLog == nil {
		return nil
	}
	if actor == "" {
		actor = skill.ActorUnknown
	}
	record := skill.AuditRecord{
		AuditID:     fmt.Sprintf("audit_%s_%s", proposal.ProposalID, eventType),
		EventType:   eventType,
		ProposalID:  proposal.ProposalID,
		SkillID:     proposal.SkillID,
		SourceRunID: proposal.SourceRunID,
		Actor:       actor,
		Timestamp:   s.now(),
		Summary:     summary,
	}
	if err := s.auditLog.Append(record); err != nil {
		return fmt.Errorf("append skill audit: %w", err)
	}
	return nil
}

// emitSkillLearningEvent enqueues one metadata-only lifecycle event so it is
// sequenced, persisted, and published to WebSocket subscribers like any other
// runtime event.
func (s *RuntimeService) emitSkillLearningEvent(ctx context.Context, sessionID, runID, eventID string, typ domain.EventType, payload skillLearningEventPayload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", typ, err)
	}
	return s.Enqueue(ctx, domain.Event{
		EventID:   eventID,
		SessionID: sessionID,
		RunID:     runID,
		Type:      typ,
		CreatedAt: s.now(),
		Payload:   encoded,
	})
}
