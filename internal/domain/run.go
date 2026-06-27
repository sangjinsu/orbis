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
}
