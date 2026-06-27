package domain

import "time"

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
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
}
