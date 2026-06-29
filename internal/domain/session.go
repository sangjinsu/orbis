package domain

import (
	"encoding/json"
	"time"
)

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	// Tool linkage, set when a message participates in a tool call so the LLM
	// context builder can reconstruct function_call / function_call_output turns.
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolArgs   json.RawMessage `json:"tool_args,omitempty"`
}

type SessionState struct {
	SessionID      string    `json:"session_id"`
	CurrentRunID   string    `json:"current_run_id"`
	RunStatus      RunStatus `json:"run_status"`
	MessageHistory []Message `json:"message_history"`
	PendingActions []Action  `json:"pending_actions"`
	LastEventSeq   int64     `json:"last_event_seq"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	// SelectedSkills records the skills the reducer chose for the current run so
	// follow-up LLM calls (after tool results) reuse the same set without
	// re-selecting, keeping the prompt stable across a run.
	SelectedSkills []SkillRef `json:"selected_skills,omitempty"`
}
