package skill

import (
	"encoding/json"
	"strings"

	"github.com/sangjinsu/orbis/internal/domain"
)

// RunFacts is a deterministic summary of a finished run, derived from the run
// snapshot and its persisted event log. It is the only input to candidate
// detection and proposal rendering — no LLM is consulted.
type RunFacts struct {
	RunID            string
	Completed        bool
	UserGoal         string
	ToolsUsed        []string // succeeded tool names, in event order
	ToolFailures     int      // failed or timed-out tool attempts
	RecoveredFailure bool     // a tool failure was followed by a success
	SkillsUsed       []domain.SkillRef
	SourceEventIDs   []string // events that informed the proposal
	// ExplicitRequest marks a user/developer-initiated proposal request; the
	// caller sets it (manual API true, auto hook false).
	ExplicitRequest bool
}

// toolEventPayload is the minimal wire shape of tool lifecycle event payloads
// (mirrors the runtime's tool_name field without importing the runtime package).
type toolEventPayload struct {
	ToolName string `json:"tool_name"`
}

type userMessagePayload struct {
	Text string `json:"text"`
}

// BuildRunFacts derives RunFacts for one run from its snapshot and the session
// event log. It is pure over its inputs: the same run and events always yield
// the same facts.
func BuildRunFacts(run domain.RunState, events []domain.Event) RunFacts {
	facts := RunFacts{
		RunID:      run.RunID,
		Completed:  run.Status == domain.RunCompleted,
		SkillsUsed: run.SelectedSkills,
	}
	failureSeen := false
	for _, event := range events {
		if event.RunID != run.RunID {
			continue
		}
		switch event.Type {
		case domain.EventUserMessageReceived:
			var payload userMessagePayload
			_ = json.Unmarshal(event.Payload, &payload)
			if facts.UserGoal == "" {
				facts.UserGoal = payload.Text
			}
			facts.SourceEventIDs = append(facts.SourceEventIDs, event.EventID)
		case domain.EventToolCallSucceeded:
			var payload toolEventPayload
			_ = json.Unmarshal(event.Payload, &payload)
			if payload.ToolName != "" {
				facts.ToolsUsed = append(facts.ToolsUsed, payload.ToolName)
			}
			if failureSeen {
				facts.RecoveredFailure = true
			}
			facts.SourceEventIDs = append(facts.SourceEventIDs, event.EventID)
		case domain.EventToolCallFailed, domain.EventToolCallTimedOut:
			facts.ToolFailures++
			failureSeen = true
			facts.SourceEventIDs = append(facts.SourceEventIDs, event.EventID)
		case domain.EventSkillApplied, domain.EventRunCompleted:
			facts.SourceEventIDs = append(facts.SourceEventIDs, event.EventID)
		}
	}
	return facts
}

// Detection reason codes, joined with commas in the detector's reason string.
const (
	reasonUsedTools        = "used_tools"
	reasonUsedSkills       = "used_skills"
	reasonRecoveredFailure = "recovered_tool_failure"
	reasonRepeatedTool     = "repeated_procedure"
	reasonExplicitRequest  = "explicit_request"
)

// DetectCandidate deterministically decides whether a run is a skill-proposal
// candidate and why. Only completed runs qualify; a completed run qualifies
// when it used tools or skills, recovered from a tool failure, repeated a tool
// procedure, or the proposal was explicitly requested. Detection never promotes
// anything — it only gates proposal creation.
func DetectCandidate(facts RunFacts) (bool, string) {
	if !facts.Completed {
		return false, "run did not complete"
	}
	reasons := make([]string, 0, 5)
	if len(facts.ToolsUsed) > 0 {
		reasons = append(reasons, reasonUsedTools)
	}
	if len(facts.SkillsUsed) > 0 {
		reasons = append(reasons, reasonUsedSkills)
	}
	if facts.RecoveredFailure {
		reasons = append(reasons, reasonRecoveredFailure)
	}
	if repeatsTool(facts.ToolsUsed) {
		reasons = append(reasons, reasonRepeatedTool)
	}
	if facts.ExplicitRequest {
		reasons = append(reasons, reasonExplicitRequest)
	}
	if len(reasons) == 0 {
		return false, "no candidate signal (no tools, skills, recovery, or explicit request)"
	}
	return true, strings.Join(reasons, ",")
}

// repeatsTool reports whether any tool name appears more than once.
func repeatsTool(tools []string) bool {
	seen := map[string]struct{}{}
	for _, name := range tools {
		if _, ok := seen[name]; ok {
			return true
		}
		seen[name] = struct{}{}
	}
	return false
}
