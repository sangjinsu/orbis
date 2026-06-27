package protocol

import (
	"encoding/json"

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
