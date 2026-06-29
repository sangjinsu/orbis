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

// SkillSummary is the wire view of a skill's index metadata, returned by the
// skill list/get APIs. It mirrors skill.Metadata as plain fields so the wire
// contract stays decoupled from the internal store representation.
type SkillSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Triggers     []string `json:"triggers,omitempty"`
	Version      string   `json:"version,omitempty"`
	Status       string   `json:"status,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	RelatedTools []string `json:"related_tools,omitempty"`
}

// SkillListPayload is the response to skill.list / GET /skills.
type SkillListPayload struct {
	Skills []SkillSummary `json:"skills"`
}

// SkillDetailPayload is the response to skill.get / GET /skills/{id}; it adds the
// loaded body and its derived hash and character count to the summary.
type SkillDetailPayload struct {
	SkillSummary
	Body        string `json:"body"`
	ContentHash string `json:"content_hash,omitempty"`
	Chars       int    `json:"chars,omitempty"`
}

// SkillReloadPayload is the response to skill.reload / POST /skills/reload; Count
// is the number of skills available after the reload.
type SkillReloadPayload struct {
	Count int `json:"count"`
}
