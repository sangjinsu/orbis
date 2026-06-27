package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

func TestReducerUserMessageDispatchesLLMCall(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1",
		RunStatus: domain.RunIdle,
		CreatedAt: now,
		UpdatedAt: now,
	}
	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"hello"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.CurrentRunID != "run_1" {
		t.Fatalf("CurrentRunID = %q, want run_1", result.NextState.CurrentRunID)
	}
	if result.NextState.RunStatus != domain.RunWaitingLLM {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunWaitingLLM)
	}
	if len(result.NextState.MessageHistory) != 1 || result.NextState.MessageHistory[0].Content != "hello" {
		t.Fatalf("MessageHistory = %#v, want one user hello message", result.NextState.MessageHistory)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(result.Actions))
	}
	action := result.Actions[0]
	if action.Type != domain.ActionDispatchLLMCall {
		t.Fatalf("action type = %q, want %q", action.Type, domain.ActionDispatchLLMCall)
	}
	if action.IdempotencyKey == "" {
		t.Fatal("action idempotency key is empty")
	}
	var payload DispatchLLMCallPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		t.Fatalf("unmarshal action payload: %v", err)
	}
	if payload.Input != "hello" {
		t.Fatalf("payload input = %q, want hello", payload.Input)
	}
}

func TestReducerLLMResponseDispatchesFinalAnswer(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:    "session_1",
		CurrentRunID: "run_1",
		RunStatus:    domain.RunWaitingLLM,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	event := domain.Event{
		EventID:   "evt_2",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventLLMResponseReceived,
		Seq:       2,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"hi","provider_response_id":"resp_1"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunCompleted {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunCompleted)
	}
	if len(result.NextState.MessageHistory) != 1 || result.NextState.MessageHistory[0].Content != "hi" {
		t.Fatalf("MessageHistory = %#v, want one assistant hi message", result.NextState.MessageHistory)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Type != domain.ActionEmitFinalAnswer {
		t.Fatalf("action type = %q, want %q", result.Actions[0].Type, domain.ActionEmitFinalAnswer)
	}
	var payload FinalAnswerPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal final answer payload: %v", err)
	}
	if payload.Text != "hi" {
		t.Fatalf("final text = %q, want hi", payload.Text)
	}
}

func TestReducerCancellationPreventsNewSideEffects(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:    "session_1",
		CurrentRunID: "run_1",
		RunStatus:    domain.RunCancelled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	event := domain.Event{
		EventID:   "evt_3",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventLLMResponseReceived,
		Seq:       3,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"late answer","provider_response_id":"resp_2"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunCancelled {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunCancelled)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions len = %d, want 0", len(result.Actions))
	}
}

func TestReducerLLMCallFailedMarksRunFailed(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:    "session_1",
		CurrentRunID: "run_1",
		RunStatus:    domain.RunWaitingLLM,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	event := domain.Event{
		EventID:   "evt_failed",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventLLMCallFailed,
		Seq:       3,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"error":"provider timeout"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunFailed)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions len = %d, want 0", len(result.Actions))
	}
}

func TestReducerRunCompletedAndRunFailedEventsAreIdempotent(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		name string
		typ  domain.EventType
		want domain.RunStatus
	}{
		{name: "completed", typ: domain.EventRunCompleted, want: domain.RunCompleted},
		{name: "failed", typ: domain.EventRunFailed, want: domain.RunFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := domain.SessionState{
				SessionID:    "session_1",
				CurrentRunID: "run_1",
				RunStatus:    domain.RunWaitingLLM,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			event := domain.Event{
				EventID:   "evt_" + tc.name,
				SessionID: "session_1",
				RunID:     "run_1",
				Type:      tc.typ,
				Seq:       4,
				CreatedAt: now,
				Payload:   json.RawMessage(`{}`),
			}

			result, err := reducer.Apply(context.Background(), state, event)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if result.NextState.RunStatus != tc.want {
				t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, tc.want)
			}
		})
	}
}
