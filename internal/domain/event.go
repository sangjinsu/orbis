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
	EventTimerFired             EventType = "TimerFired"
	EventAssistantDelta         EventType = "AssistantDelta"
	EventFinalAnswerEmitted     EventType = "FinalAnswerEmitted"
	EventRunCompleted           EventType = "RunCompleted"
	EventRunFailed              EventType = "RunFailed"
	EventRunCancelled           EventType = "RunCancelled"
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
