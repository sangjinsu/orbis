package domain

import (
	"encoding/json"
	"errors"
)

type ActionType string

const (
	ActionDispatchLLMCall  ActionType = "DispatchLLMCall"
	ActionDispatchToolCall ActionType = "DispatchToolCall"
	ActionScheduleTimer    ActionType = "ScheduleTimer"
	ActionEmitFinalAnswer  ActionType = "EmitFinalAnswer"
	ActionCancelWorker     ActionType = "CancelWorker"
)

type Action struct {
	ActionID       string          `json:"action_id"`
	SessionID      string          `json:"session_id"`
	RunID          string          `json:"run_id"`
	Type           ActionType      `json:"type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}

func (a Action) Validate() error {
	if a.IdempotencyKey == "" {
		return errors.New("idempotency key is required")
	}
	return nil
}
