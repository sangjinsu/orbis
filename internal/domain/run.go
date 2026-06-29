package domain

import "time"

type RunStatus string

const (
	RunIdle         RunStatus = "IDLE"
	RunQueued       RunStatus = "QUEUED"
	RunPreparing    RunStatus = "PREPARING"
	RunWaitingLLM   RunStatus = "WAITING_LLM"
	RunWaitingTool  RunStatus = "WAITING_TOOL"
	RunWaitingTimer RunStatus = "WAITING_TIMER"
	RunWaitingHuman RunStatus = "WAITING_HUMAN"
	RunCompleted    RunStatus = "COMPLETED"
	RunFailed       RunStatus = "FAILED"
	RunCancelled    RunStatus = "CANCELLED"
)

type RunState struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Status    RunStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// SelectedSkills is the per-run snapshot of skills applied to the run's LLM
	// context. Written once when the run first selects skills and then left
	// untouched, so it records what was used even if the index later reloads.
	SelectedSkills []SkillRef `json:"selected_skills,omitempty"`
}

// IsTerminalRunStatus reports whether a run has reached a final state and can no
// longer transition or dispatch new side effects.
func IsTerminalRunStatus(status RunStatus) bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled
}
