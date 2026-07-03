package domain

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventSessionCreated         EventType = "SessionCreated"
	EventUserMessageReceived    EventType = "UserMessageReceived"
	EventRunStarted             EventType = "RunStarted"
	EventRunStatusChanged       EventType = "RunStatusChanged"
	EventLLMCallStarted         EventType = "LLMCallStarted"
	EventLLMResponseReceived    EventType = "LLMResponseReceived"
	EventLLMCallFailed          EventType = "LLMCallFailed"
	EventToolCallProposed       EventType = "ToolCallProposed"
	EventToolCallStarted        EventType = "ToolCallStarted"
	EventToolCallSucceeded      EventType = "ToolCallSucceeded"
	EventToolCallFailed         EventType = "ToolCallFailed"
	EventToolCallRejected       EventType = "ToolCallRejected"
	EventToolCallRetryScheduled EventType = "ToolCallRetryScheduled"
	EventToolCallRetried        EventType = "ToolCallRetried"
	EventToolCallDeduplicated   EventType = "ToolCallDeduplicated"
	EventToolCallTimedOut       EventType = "ToolCallTimedOut"
	// EventToolCallDenialContinued is emitted when a policy-rejected tool call does
	// not fail the run: the denial is fed back to the LLM to replan (v1.5).
	EventToolCallDenialContinued EventType = "ToolCallDenialContinued"
	EventTimerFired              EventType = "TimerFired"
	EventAssistantDelta          EventType = "AssistantDelta"
	EventFinalAnswerEmitted      EventType = "FinalAnswerEmitted"
	EventRunCompleted            EventType = "RunCompleted"
	EventRunFailed               EventType = "RunFailed"
	EventRunCancelled            EventType = "RunCancelled"

	// Skill lifecycle (v1). The reducer emits these from a pure in-memory
	// selection before LLMCallStarted: one SkillSelected and one SkillLoaded per
	// chosen skill, a single SkillApplied summary, or one SkillSkipped when
	// selection found no match. They carry metadata only (never body text).
	EventSkillSelected EventType = "SkillSelected"
	EventSkillLoaded   EventType = "SkillLoaded"
	EventSkillApplied  EventType = "SkillApplied"
	EventSkillSkipped  EventType = "SkillSkipped"

	// Skill learning (v2). The reviewable skill learning loop emits these from
	// the app-layer learning service (never the reducer): proposals are created
	// from runs, reviewed by a human, and only an explicit approval promotes one
	// to an active skill. Payloads carry metadata only.
	EventSkillCandidateDetected    EventType = "SkillCandidateDetected"
	EventSkillProposalCreated      EventType = "SkillProposalCreated"
	EventSkillReviewRequired       EventType = "SkillReviewRequired"
	EventSkillProposalApproved     EventType = "SkillProposalApproved"
	EventSkillProposalRejected     EventType = "SkillProposalRejected"
	EventSkillPromoted             EventType = "SkillPromoted"
	EventSkillPromotionFailed      EventType = "SkillPromotionFailed"
	EventSkillIndexReloadRequested EventType = "SkillIndexReloadRequested"
	EventSkillIndexReloaded        EventType = "SkillIndexReloaded"
	EventSkillAuditRecorded        EventType = "SkillAuditRecorded"

	// Reserved skill-index observability event types. Defined now for a stable
	// vocabulary but not yet emitted, mirroring EventToolCallProposed.
	EventSkillIndexLoaded        EventType = "SkillIndexLoaded"
	EventSkillIndexSearchStarted EventType = "SkillIndexSearchStarted"
	EventSkillSelectionFailed    EventType = "SkillSelectionFailed"
)

type Event struct {
	EventID   string          `json:"event_id"`
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id"`
	Type      EventType       `json:"type"`
	Seq       int64           `json:"seq"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}
