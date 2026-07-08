package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
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
	Version    string `json:"version,omitempty"`
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

// appendSkillAudit writes one audit record for a proposal lifecycle transition,
// keyed by proposal and event type (each transition happens at most once).
func (s *RuntimeService) appendSkillAudit(eventType string, proposal skill.SkillProposal, actor, summary string) error {
	return s.appendSkillAuditRecord(fmt.Sprintf("audit_%s_%s", proposal.ProposalID, eventType), eventType, proposal, actor, summary)
}

// appendSkillAuditRecord writes one audit record with an explicit id, for
// transitions that can repeat (reviewer edits carry a revision-suffixed id).
// The summary is a short user-visible sentence; no secrets, no hidden reasoning.
func (s *RuntimeService) appendSkillAuditRecord(auditID, eventType string, proposal skill.SkillProposal, actor, summary string) error {
	if s.auditLog == nil {
		return nil
	}
	if actor == "" {
		actor = skill.ActorUnknown
	}
	record := skill.AuditRecord{
		AuditID:     auditID,
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

// Admin protection for mutating skill-learning operations. With no token
// configured the mutating endpoints are disabled entirely (the safe default);
// read endpoints stay open.
var (
	errAdminDisabled     = errors.New("admin endpoints are disabled: ORBIS_ADMIN_TOKEN is not configured")
	errInvalidAdminToken = errors.New("invalid admin token")
)

func (s *RuntimeService) requireAdmin(token string) error {
	if s.adminToken == "" {
		return errAdminDisabled
	}
	if token != s.adminToken {
		return errInvalidAdminToken
	}
	return nil
}

// proposalSummary maps a stored proposal to its wire summary.
func proposalSummary(p skill.SkillProposal) protocol.SkillProposalSummary {
	return protocol.SkillProposalSummary{
		ProposalID:       p.ProposalID,
		SourceRunID:      p.SourceRunID,
		SkillID:          p.SkillID,
		Title:            p.Title,
		Status:           string(p.Status),
		RationaleSummary: p.RationaleSummary,
		Version:          p.Version,
		ContentHash:      p.ContentHash,
		PromotedSkillID:  p.PromotedSkillID,
		Revision:         p.Revision,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

// proposalDetail maps a stored proposal to its full wire payload.
func proposalDetail(p skill.SkillProposal) protocol.SkillProposalDetailPayload {
	return protocol.SkillProposalDetailPayload{
		SkillProposalSummary: proposalSummary(p),
		Purpose:              p.Purpose,
		WhenToUse:            p.WhenToUse,
		RequiredContext:      p.RequiredContext,
		Procedure:            p.Procedure,
		RelatedTools:         p.RelatedTools,
		Verification:         p.Verification,
		Pitfalls:             p.Pitfalls,
		SourceEventIDs:       p.SourceEventIDs,
		Body:                 p.Body,
	}
}

// ListSkillProposals returns proposals as wire summaries, optionally filtered
// by status. It implements gateway.SkillLearning so HTTP and WS share one impl.
func (s *RuntimeService) ListSkillProposals(status string) (protocol.SkillProposalListPayload, error) {
	payload := protocol.SkillProposalListPayload{Proposals: []protocol.SkillProposalSummary{}}
	if s.proposals == nil {
		return payload, errSkillLearningDisabled
	}
	list, err := s.proposals.List(skill.SkillProposalStatus(status))
	if err != nil {
		return payload, err
	}
	for _, p := range list {
		payload.Proposals = append(payload.Proposals, proposalSummary(p))
	}
	return payload, nil
}

// GetSkillProposal returns one proposal, with found=false for unknown ids.
func (s *RuntimeService) GetSkillProposal(id string) (protocol.SkillProposalDetailPayload, bool, error) {
	if s.proposals == nil {
		return protocol.SkillProposalDetailPayload{}, false, errSkillLearningDisabled
	}
	p, err := s.proposals.Get(id)
	if errors.Is(err, skill.ErrProposalNotFound) {
		return protocol.SkillProposalDetailPayload{}, false, nil
	}
	if err != nil {
		return protocol.SkillProposalDetailPayload{}, false, err
	}
	return proposalDetail(p), true, nil
}

// CreateSkillProposal creates a proposal from a run for an admin-authenticated
// caller (gateway HTTP / WS after the token check).
func (s *RuntimeService) CreateSkillProposal(ctx context.Context, runID string) (protocol.SkillProposalDetailPayload, error) {
	p, err := s.CreateSkillProposalFromRun(ctx, runID, skill.ActorAdmin, true)
	if err != nil {
		return protocol.SkillProposalDetailPayload{}, err
	}
	return proposalDetail(p), nil
}

// UpdateSkillProposal applies a reviewer's structured-field edit for an
// admin-authenticated caller.
func (s *RuntimeService) UpdateSkillProposal(ctx context.Context, id string, fields protocol.SkillProposalUpdateRequest) (protocol.SkillProposalDetailPayload, error) {
	p, err := s.updateSkillProposal(ctx, id, fields, skill.ActorAdmin)
	if err != nil {
		return protocol.SkillProposalDetailPayload{}, err
	}
	return proposalDetail(p), nil
}

// ApproveSkillProposal approves and promotes a pending proposal.
func (s *RuntimeService) ApproveSkillProposal(ctx context.Context, id string) (protocol.SkillProposalDetailPayload, error) {
	p, err := s.approveSkillProposal(ctx, id, skill.ActorAdmin)
	if err != nil {
		return protocol.SkillProposalDetailPayload{}, err
	}
	return proposalDetail(p), nil
}

// RejectSkillProposal rejects a pending proposal.
func (s *RuntimeService) RejectSkillProposal(ctx context.Context, id, reason string) (protocol.SkillProposalDetailPayload, error) {
	p, err := s.rejectSkillProposal(ctx, id, skill.ActorAdmin, reason)
	if err != nil {
		return protocol.SkillProposalDetailPayload{}, err
	}
	return proposalDetail(p), nil
}

// approveSkillProposal drives the reviewed promotion flow:
// approve -> promote -> reload, emitting
// SkillProposalApproved -> SkillPromoted -> SkillIndexReloadRequested ->
// SkillIndexReloaded -> SkillAuditRecorded. On a promotion failure (e.g. a
// skill-id conflict) the proposal is marked failed and SkillPromotionFailed is
// emitted instead. Only pending proposals can be approved.
func (s *RuntimeService) approveSkillProposal(ctx context.Context, proposalID, actor string) (skill.SkillProposal, error) {
	if s.proposals == nil {
		return skill.SkillProposal{}, errSkillLearningDisabled
	}
	proposal, err := s.proposals.Get(proposalID)
	if err != nil {
		return skill.SkillProposal{}, err
	}
	// Resolve the source session up front so every lifecycle event can be
	// emitted; an unresolvable source run fails the operation before any change.
	run, err := s.store.LoadRun(ctx, proposal.SourceRunID)
	if err != nil {
		return skill.SkillProposal{}, fmt.Errorf("load source run: %w", err)
	}
	sessionID := run.SessionID
	if proposal.Status != skill.ProposalPending {
		return skill.SkillProposal{}, fmt.Errorf("proposal %q is %s, not pending", proposalID, proposal.Status)
	}

	now := s.now()
	proposal.Status = skill.ProposalApproved
	proposal.ApprovedAt = &now
	proposal.UpdatedAt = now
	if err := s.proposals.Update(proposal); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.appendSkillAudit(string(domain.EventSkillProposalApproved), proposal, actor, "proposal approved"); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_proposal_approved", domain.EventSkillProposalApproved, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status),
	}); err != nil {
		return skill.SkillProposal{}, err
	}

	if s.promoter == nil {
		return s.failPromotion(ctx, sessionID, proposal, actor, errors.New("skill promotion requires an active skills directory"))
	}
	meta, err := s.promoter.Promote(proposal, now)
	if err != nil {
		return s.failPromotion(ctx, sessionID, proposal, actor, err)
	}

	proposal.Status = skill.ProposalPromoted
	proposal.PromotedSkillID = meta.ID
	proposal.Version = meta.Version
	proposal.UpdatedAt = s.now()
	if err := s.proposals.Update(proposal); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.appendSkillAudit(string(domain.EventSkillPromoted), proposal, actor, "proposal promoted to skill "+meta.ID); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_promoted", domain.EventSkillPromoted, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: meta.ID, Status: string(proposal.Status), Version: meta.Version,
	}); err != nil {
		return skill.SkillProposal{}, err
	}

	// Reload the in-memory index so the promoted skill becomes selectable.
	if s.skills != nil {
		if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_index_reload_requested", domain.EventSkillIndexReloadRequested, skillLearningEventPayload{
			ProposalID: proposal.ProposalID, SkillID: meta.ID,
		}); err != nil {
			return skill.SkillProposal{}, err
		}
		if err := s.skills.Reload(); err != nil {
			return proposal, fmt.Errorf("skill promoted but index reload failed: %w", err)
		}
		if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_index_reloaded", domain.EventSkillIndexReloaded, skillLearningEventPayload{
			ProposalID: proposal.ProposalID, SkillID: meta.ID,
		}); err != nil {
			return skill.SkillProposal{}, err
		}
	}
	if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_audit_promoted", domain.EventSkillAuditRecorded, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: meta.ID, Status: string(proposal.Status), Reason: "promoted",
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	return proposal, nil
}

// failPromotion marks an approved proposal as failed and records why. The
// proposal can be retried later (failed -> promoted is a legal transition).
func (s *RuntimeService) failPromotion(ctx context.Context, sessionID string, proposal skill.SkillProposal, actor string, cause error) (skill.SkillProposal, error) {
	proposal.Status = skill.ProposalFailed
	proposal.UpdatedAt = s.now()
	if err := s.proposals.Update(proposal); err != nil {
		return skill.SkillProposal{}, fmt.Errorf("promotion failed (%v) and status update failed: %w", cause, err)
	}
	if err := s.appendSkillAudit(string(domain.EventSkillPromotionFailed), proposal, actor, "promotion failed: "+cause.Error()); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_promotion_failed", domain.EventSkillPromotionFailed, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Error: cause.Error(),
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, sessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_audit_promotion_failed", domain.EventSkillAuditRecorded, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Reason: "promotion_failed",
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	return proposal, fmt.Errorf("promote proposal %q: %w", proposal.ProposalID, cause)
}

// updateSkillProposal applies a reviewer's structured-field edit to a pending
// proposal. Only the fields that compose the rendered body are editable; Body
// and ContentHash are re-derived through the creation renderer so an edited
// proposal can never drift from what promotion would write. Identity fields
// (SkillID, SourceRunID) and the detection RationaleSummary stay immutable.
// Emits SkillProposalUpdated -> SkillAuditRecorded with revision-unique ids.
func (s *RuntimeService) updateSkillProposal(ctx context.Context, proposalID string, fields protocol.SkillProposalUpdateRequest, actor string) (skill.SkillProposal, error) {
	if s.proposals == nil {
		return skill.SkillProposal{}, errSkillLearningDisabled
	}
	proposal, err := s.proposals.Get(proposalID)
	if err != nil {
		return skill.SkillProposal{}, err
	}
	// Resolve the source session up front (same order as approve/reject) so an
	// unresolvable source run fails the operation before any change.
	run, err := s.store.LoadRun(ctx, proposal.SourceRunID)
	if err != nil {
		return skill.SkillProposal{}, fmt.Errorf("load source run: %w", err)
	}
	if proposal.Status != skill.ProposalPending {
		return skill.SkillProposal{}, fmt.Errorf("proposal %q is %s, not pending", proposalID, proposal.Status)
	}

	edited := applyProposalEdit(&proposal, fields)
	if len(edited) == 0 {
		return skill.SkillProposal{}, fmt.Errorf("no editable fields provided")
	}
	if strings.TrimSpace(proposal.Title) == "" {
		return skill.SkillProposal{}, fmt.Errorf("proposal %q: title cannot be cleared", proposalID)
	}
	proposal.Rerender()
	proposal.Revision++
	proposal.UpdatedAt = s.now()
	// Same-status rewrite: the store validates but requires no transition. The
	// Get -> mutate -> Update sequence is not atomic across callers, which the
	// single-admin review model accepts.
	if err := s.proposals.Update(proposal); err != nil {
		return skill.SkillProposal{}, err
	}

	summary := "proposal updated: " + strings.Join(edited, ", ")
	auditID := fmt.Sprintf("audit_%s_%s_r%d", proposal.ProposalID, domain.EventSkillProposalUpdated, proposal.Revision)
	if err := s.appendSkillAuditRecord(auditID, string(domain.EventSkillProposalUpdated), proposal, actor, summary); err != nil {
		return skill.SkillProposal{}, err
	}
	eventID := fmt.Sprintf("%s:skill_proposal_updated:%d", proposal.SourceRunID, proposal.Revision)
	if err := s.emitSkillLearningEvent(ctx, run.SessionID, proposal.SourceRunID, eventID, domain.EventSkillProposalUpdated, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Reason: summary,
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, run.SessionID, proposal.SourceRunID, eventID+":audit", domain.EventSkillAuditRecorded, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Reason: "updated",
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	return proposal, nil
}

// applyProposalEdit copies each provided (non-nil) field onto the proposal and
// returns the wire names of the fields it set, for the audit summary.
func applyProposalEdit(p *skill.SkillProposal, fields protocol.SkillProposalUpdateRequest) []string {
	var edited []string
	if fields.Title != nil {
		p.Title = *fields.Title
		edited = append(edited, "title")
	}
	if fields.Purpose != nil {
		p.Purpose = *fields.Purpose
		edited = append(edited, "purpose")
	}
	if fields.WhenToUse != nil {
		p.WhenToUse = *fields.WhenToUse
		edited = append(edited, "when_to_use")
	}
	if fields.RequiredContext != nil {
		p.RequiredContext = *fields.RequiredContext
		edited = append(edited, "required_context")
	}
	if fields.Procedure != nil {
		p.Procedure = *fields.Procedure
		edited = append(edited, "procedure")
	}
	if fields.RelatedTools != nil {
		p.RelatedTools = *fields.RelatedTools
		edited = append(edited, "related_tools")
	}
	if fields.Verification != nil {
		p.Verification = *fields.Verification
		edited = append(edited, "verification")
	}
	if fields.Pitfalls != nil {
		p.Pitfalls = *fields.Pitfalls
		edited = append(edited, "pitfalls")
	}
	return edited
}

// rejectSkillProposal rejects a pending proposal, emitting
// SkillProposalRejected -> SkillAuditRecorded.
func (s *RuntimeService) rejectSkillProposal(ctx context.Context, proposalID, actor, reason string) (skill.SkillProposal, error) {
	if s.proposals == nil {
		return skill.SkillProposal{}, errSkillLearningDisabled
	}
	proposal, err := s.proposals.Get(proposalID)
	if err != nil {
		return skill.SkillProposal{}, err
	}
	run, err := s.store.LoadRun(ctx, proposal.SourceRunID)
	if err != nil {
		return skill.SkillProposal{}, fmt.Errorf("load source run: %w", err)
	}
	if proposal.Status != skill.ProposalPending {
		return skill.SkillProposal{}, fmt.Errorf("proposal %q is %s, not pending", proposalID, proposal.Status)
	}

	now := s.now()
	proposal.Status = skill.ProposalRejected
	proposal.RejectedAt = &now
	proposal.UpdatedAt = now
	if err := s.proposals.Update(proposal); err != nil {
		return skill.SkillProposal{}, err
	}
	summary := "proposal rejected"
	if reason != "" {
		summary += ": " + reason
	}
	if err := s.appendSkillAudit(string(domain.EventSkillProposalRejected), proposal, actor, summary); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, run.SessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_proposal_rejected", domain.EventSkillProposalRejected, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Reason: reason,
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	if err := s.emitSkillLearningEvent(ctx, run.SessionID, proposal.SourceRunID, proposal.SourceRunID+":skill_audit_rejected", domain.EventSkillAuditRecorded, skillLearningEventPayload{
		ProposalID: proposal.ProposalID, SkillID: proposal.SkillID, Status: string(proposal.Status), Reason: "rejected",
	}); err != nil {
		return skill.SkillProposal{}, err
	}
	return proposal, nil
}

// WebSocket method params for the skill-learning loop. Mutating methods carry
// the admin token in params.
type skillProposalListParams struct {
	Status string `json:"status"`
}

type skillProposalGetParams struct {
	ProposalID string `json:"proposal_id"`
}

type skillProposalCreateParams struct {
	RunID string `json:"run_id"`
	Token string `json:"token"`
}

type skillProposalActionParams struct {
	ProposalID string `json:"proposal_id"`
	Reason     string `json:"reason"`
	Token      string `json:"token"`
}

type skillProposalUpdateParams struct {
	ProposalID string `json:"proposal_id"`
	Token      string `json:"token"`
	protocol.SkillProposalUpdateRequest
}

func (s *RuntimeService) handleSkillProposalList(_ context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalListParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.list params: %w", err)
		}
	}
	payload, err := s.ListSkillProposals(params.Status)
	if err != nil {
		return nil, err
	}
	return marshalPayload(payload)
}

func (s *RuntimeService) handleSkillProposalGet(_ context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalGetParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.get params: %w", err)
		}
	}
	if params.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	detail, found, err := s.GetSkillProposal(params.ProposalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("skill proposal %q not found", params.ProposalID)
	}
	return marshalPayload(detail)
}

func (s *RuntimeService) handleSkillProposalCreate(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalCreateParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.create_from_run params: %w", err)
		}
	}
	if err := s.requireAdmin(params.Token); err != nil {
		return nil, err
	}
	if params.RunID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	detail, err := s.CreateSkillProposal(ctx, params.RunID)
	if err != nil {
		return nil, err
	}
	return marshalPayload(detail)
}

func (s *RuntimeService) handleSkillProposalUpdate(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalUpdateParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.update params: %w", err)
		}
	}
	if err := s.requireAdmin(params.Token); err != nil {
		return nil, err
	}
	if params.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	detail, err := s.UpdateSkillProposal(ctx, params.ProposalID, params.SkillProposalUpdateRequest)
	if err != nil {
		return nil, err
	}
	return marshalPayload(detail)
}

func (s *RuntimeService) handleSkillProposalApprove(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalActionParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.approve params: %w", err)
		}
	}
	if err := s.requireAdmin(params.Token); err != nil {
		return nil, err
	}
	if params.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	detail, err := s.ApproveSkillProposal(ctx, params.ProposalID)
	if err != nil {
		return nil, err
	}
	return marshalPayload(detail)
}

func (s *RuntimeService) handleSkillProposalReject(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillProposalActionParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.proposal.reject params: %w", err)
		}
	}
	if err := s.requireAdmin(params.Token); err != nil {
		return nil, err
	}
	if params.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	detail, err := s.RejectSkillProposal(ctx, params.ProposalID, params.Reason)
	if err != nil {
		return nil, err
	}
	return marshalPayload(detail)
}
