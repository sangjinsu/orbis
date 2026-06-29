package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/skill"
)

// fakeSkillIndex is an in-memory skill.Index for reducer and lane tests.
type fakeSkillIndex struct {
	entries []skill.Entry
}

func (f fakeSkillIndex) Snapshot() []skill.Entry { return f.entries }

// wsSkillIndex returns an index with a single websocket-triggered skill used to
// exercise selection deterministically.
func wsSkillIndex() fakeSkillIndex {
	return fakeSkillIndex{entries: []skill.Entry{
		{
			Metadata: skill.Metadata{
				ID:       "ws-test",
				Name:     "ws",
				Title:    "WebSocket Runtime Test",
				Triggers: []string{"websocket"},
				Version:  "1",
				Priority: 100,
				Status:   "active",
			},
			Body:        "WebSocket runtime test body",
			ContentHash: "hash-ws",
			Chars:       27,
		},
	}}
}

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
	if len(result.Events) != 2 {
		t.Fatalf("events len = %d, want 2", len(result.Events))
	}
	if result.Events[0].Type != domain.EventRunStarted || result.Events[1].Type != domain.EventRunStatusChanged {
		t.Fatalf("events = %q, %q; want RunStarted, RunStatusChanged", result.Events[0].Type, result.Events[1].Type)
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

func TestReducerLLMResponseWithToolCallDispatchesTool(t *testing.T) {
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
		EventID:   "evt_tool",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventLLMResponseReceived,
		Seq:       2,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"tool_call":{"tool_call_id":"call_1","name":"echo","args":{"text":"hello"}}}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunWaitingTool {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunWaitingTool)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Type != domain.ActionDispatchToolCall {
		t.Fatalf("action = %q, want %q", result.Actions[0].Type, domain.ActionDispatchToolCall)
	}
	var payload DispatchToolCallPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool action payload: %v", err)
	}
	if payload.Name != "echo" || payload.ToolCallID != "call_1" || string(payload.Args) != `{"text":"hello"}` {
		t.Fatalf("tool payload = %#v args=%s, want echo call_1", payload, payload.Args)
	}
}

func TestReducerToolSuccessDispatchesNextLLMCall(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:    "session_1",
		CurrentRunID: "run_1",
		RunStatus:    domain.RunWaitingTool,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	event := domain.Event{
		EventID:   "evt_tool_success",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventToolCallSucceeded,
		Seq:       3,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"tool_call_id":"call_1","name":"echo","result":{"text":"hello"}}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunWaitingLLM {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunWaitingLLM)
	}
	if len(result.NextState.MessageHistory) != 1 || result.NextState.MessageHistory[0].Role != "tool" {
		t.Fatalf("MessageHistory = %#v, want one tool message", result.NextState.MessageHistory)
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != domain.ActionDispatchLLMCall {
		t.Fatalf("actions = %#v, want one DispatchLLMCall", result.Actions)
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

func TestReducerToolFailureSchedulesRetry(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:    "session_1",
		CurrentRunID: "run_1",
		RunStatus:    domain.RunWaitingTool,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	failed, _ := json.Marshal(ToolCallEventPayload{
		ToolCallID:     "call_1",
		Name:           "mock.fail_once",
		IdempotencyKey: "run_1:tool:call_1",
		Attempt:        1,
		MaxAttempts:    2,
		Retryable:      true,
	})
	event := domain.Event{
		EventID: "evt_tool_failed", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventToolCallFailed, Seq: 4, CreatedAt: now, Payload: failed,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunWaitingTimer {
		t.Fatalf("RunStatus = %q, want WAITING_TIMER", result.NextState.RunStatus)
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventToolCallRetryScheduled {
		t.Fatalf("events = %#v, want one ToolCallRetryScheduled", result.Events)
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != domain.ActionScheduleTimer {
		t.Fatalf("actions = %#v, want one ScheduleTimer", result.Actions)
	}
	var timer ScheduleTimerPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &timer); err != nil {
		t.Fatalf("unmarshal timer payload: %v", err)
	}
	if timer.Kind != "tool_retry" || timer.ToolCall == nil || timer.ToolCall.Attempt != 2 {
		t.Fatalf("timer payload = %#v, want tool_retry attempt 2", timer)
	}
	if timer.ToolCall.IdempotencyKey != "run_1:tool:call_1" {
		t.Fatalf("retry idempotency key = %q, want stable run_1:tool:call_1", timer.ToolCall.IdempotencyKey)
	}
}

func TestReducerToolFailureExhaustedFailsRun(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingTool,
		CreatedAt: now, UpdatedAt: now,
	}
	failed, _ := json.Marshal(ToolCallEventPayload{
		ToolCallID: "call_1", Name: "mock.fail_once", IdempotencyKey: "run_1:tool:call_1",
		Attempt: 2, MaxAttempts: 2, Retryable: true,
	})
	event := domain.Event{
		EventID: "evt_tool_failed_final", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventToolCallFailed, Seq: 5, CreatedAt: now, Payload: failed,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %q, want FAILED", result.NextState.RunStatus)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions len = %d, want 0", len(result.Actions))
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventRunFailed {
		t.Fatalf("events = %#v, want one RunFailed", result.Events)
	}
}

func TestReducerToolRejectedFailsRun(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingTool,
		CreatedAt: now, UpdatedAt: now,
	}
	rejected, _ := json.Marshal(ToolCallEventPayload{
		ToolCallID: "call_1", Name: "mock.dangerous", ReasonCode: "toolset_not_allowed", Error: "denied",
	})
	event := domain.Event{
		EventID: "evt_tool_rejected", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventToolCallRejected, Seq: 4, CreatedAt: now, Payload: rejected,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %q, want FAILED", result.NextState.RunStatus)
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventRunFailed {
		t.Fatalf("events = %#v, want one RunFailed", result.Events)
	}
}

func TestReducerToolRejectedContinuesWhenBudgetRemains(t *testing.T) {
	reducer := NewReducer(ReducerConfig{ToolDenialContinuationMax: 2})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingTool,
		CreatedAt: now, UpdatedAt: now,
	}
	rejected, _ := json.Marshal(ToolCallEventPayload{
		ToolCallID: "call_1", Name: "mock.dangerous", ReasonCode: "toolset_not_allowed", Error: "denied",
	})
	event := domain.Event{
		EventID: "evt_tool_rejected", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventToolCallRejected, Seq: 4, CreatedAt: now, Payload: rejected,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunWaitingLLM {
		t.Fatalf("RunStatus = %q, want WAITING_LLM", result.NextState.RunStatus)
	}
	if result.NextState.ToolDenialContinuations != 1 {
		t.Fatalf("ToolDenialContinuations = %d, want 1", result.NextState.ToolDenialContinuations)
	}
	if len(result.NextState.MessageHistory) != 1 || result.NextState.MessageHistory[0].Role != "tool" {
		t.Fatalf("history = %#v, want one tool denial message", result.NextState.MessageHistory)
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventToolCallDenialContinued {
		t.Fatalf("events = %#v, want one ToolCallDenialContinued", result.Events)
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != domain.ActionDispatchLLMCall {
		t.Fatalf("actions = %#v, want one DispatchLLMCall", result.Actions)
	}
}

func TestReducerToolRejectedFailsWhenBudgetExhausted(t *testing.T) {
	reducer := NewReducer(ReducerConfig{ToolDenialContinuationMax: 2})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingTool,
		ToolDenialContinuations: 2, // budget already spent
		CreatedAt:               now, UpdatedAt: now,
	}
	rejected, _ := json.Marshal(ToolCallEventPayload{
		ToolCallID: "call_1", Name: "mock.dangerous", ReasonCode: "toolset_not_allowed",
	})
	event := domain.Event{
		EventID: "evt_tool_rejected_final", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventToolCallRejected, Seq: 5, CreatedAt: now, Payload: rejected,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %q, want FAILED", result.NextState.RunStatus)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions len = %d, want 0", len(result.Actions))
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventRunFailed {
		t.Fatalf("events = %#v, want one RunFailed", result.Events)
	}
}

func TestReducerToolRetryTimerDispatchesToolCall(t *testing.T) {
	reducer := Reducer{}
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingTimer,
		CreatedAt: now, UpdatedAt: now,
	}
	timerPayload, _ := json.Marshal(TimerFiredPayload{
		Kind: "tool_retry",
		ToolCall: &DispatchToolCallPayload{
			ToolCallID: "call_1", Name: "mock.fail_once", IdempotencyKey: "run_1:tool:call_1",
			Attempt: 2, MaxAttempts: 2,
		},
	})
	event := domain.Event{
		EventID: "evt_retry_timer", SessionID: "session_1", RunID: "run_1",
		Type: domain.EventTimerFired, Seq: 6, CreatedAt: now, Payload: timerPayload,
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.NextState.RunStatus != domain.RunWaitingTool {
		t.Fatalf("RunStatus = %q, want WAITING_TOOL", result.NextState.RunStatus)
	}
	if len(result.Events) != 1 || result.Events[0].Type != domain.EventToolCallRetried {
		t.Fatalf("events = %#v, want one ToolCallRetried", result.Events)
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != domain.ActionDispatchToolCall {
		t.Fatalf("actions = %#v, want one DispatchToolCall", result.Actions)
	}
	if result.Actions[0].IdempotencyKey != "run_1:tool:call_1" {
		t.Fatalf("retry action key = %q, want stable run_1:tool:call_1", result.Actions[0].IdempotencyKey)
	}
	var call DispatchToolCallPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &call); err != nil {
		t.Fatalf("unmarshal retry tool payload: %v", err)
	}
	if call.Attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", call.Attempt)
	}
}

func TestReducerTimerFiredFailsRunAndEmitsRunFailed(t *testing.T) {
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
		EventID:   "timer_run_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventTimerFired,
		Seq:       3,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"reason":"run_timeout"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.NextState.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %q, want %q", result.NextState.RunStatus, domain.RunFailed)
	}
	if len(result.Events) != 1 {
		t.Fatalf("events len = %d, want 1", len(result.Events))
	}
	if result.Events[0].Type != domain.EventRunFailed {
		t.Fatalf("derived event = %q, want %q", result.Events[0].Type, domain.EventRunFailed)
	}
}

func TestReducerUserMessageSelectsSkillsAndEmitsLifecycleEvents(t *testing.T) {
	reducer := NewReducer(ReducerConfig{
		SkillsEnabled: true,
		SkillIndex:    wsSkillIndex(),
		SkillSelect:   skill.SelectConfig{MaxSelected: 3, MaxChars: 12000},
	})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{SessionID: "session_1", RunStatus: domain.RunIdle, CreatedAt: now, UpdatedAt: now}
	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"how do I run a websocket runtime test?"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if len(result.NextState.SelectedSkills) != 1 || result.NextState.SelectedSkills[0].ID != "ws-test" {
		t.Fatalf("SelectedSkills = %#v, want one ws-test", result.NextState.SelectedSkills)
	}

	wantTypes := []domain.EventType{
		domain.EventRunStarted,
		domain.EventRunStatusChanged,
		domain.EventSkillSelected,
		domain.EventSkillLoaded,
		domain.EventSkillApplied,
	}
	if len(result.Events) != len(wantTypes) {
		t.Fatalf("events len = %d, want %d (%#v)", len(result.Events), len(wantTypes), result.Events)
	}
	for i, want := range wantTypes {
		if result.Events[i].Type != want {
			t.Fatalf("events[%d] = %q, want %q", i, result.Events[i].Type, want)
		}
	}

	var selected skill.SkillEventPayload
	if err := json.Unmarshal(result.Events[2].Payload, &selected); err != nil {
		t.Fatalf("unmarshal SkillSelected payload: %v", err)
	}
	if selected.SkillID != "ws-test" || selected.Score <= 0 || selected.Reason == "" {
		t.Fatalf("SkillSelected payload = %#v, want ws-test with score and reason", selected)
	}

	var loaded skill.SkillEventPayload
	if err := json.Unmarshal(result.Events[3].Payload, &loaded); err != nil {
		t.Fatalf("unmarshal SkillLoaded payload: %v", err)
	}
	if loaded.ContentHash != "hash-ws" || loaded.Chars != 27 {
		t.Fatalf("SkillLoaded payload = %#v, want hash-ws/27", loaded)
	}

	var applied skill.SkillAppliedPayload
	if err := json.Unmarshal(result.Events[4].Payload, &applied); err != nil {
		t.Fatalf("unmarshal SkillApplied payload: %v", err)
	}
	if applied.Count != 1 || applied.TotalChars != 27 || len(applied.SkillIDs) != 1 || applied.SkillIDs[0] != "ws-test" {
		t.Fatalf("SkillApplied payload = %#v, want count 1 ws-test 27 chars", applied)
	}

	var payload DispatchLLMCallPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal action payload: %v", err)
	}
	if len(payload.SelectedSkills) != 1 || payload.SelectedSkills[0].ID != "ws-test" {
		t.Fatalf("payload SelectedSkills = %#v, want one ws-test", payload.SelectedSkills)
	}
}

func TestReducerUserMessageEmitsSkillSkippedWhenNoMatch(t *testing.T) {
	reducer := NewReducer(ReducerConfig{
		SkillsEnabled: true,
		SkillIndex:    wsSkillIndex(),
		SkillSelect:   skill.SelectConfig{MaxSelected: 3, MaxChars: 12000},
	})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{SessionID: "session_1", RunStatus: domain.RunIdle, CreatedAt: now, UpdatedAt: now}
	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"tell me a story about a cat"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(result.NextState.SelectedSkills) != 0 {
		t.Fatalf("SelectedSkills = %#v, want none", result.NextState.SelectedSkills)
	}
	if len(result.Events) != 3 || result.Events[2].Type != domain.EventSkillSkipped {
		t.Fatalf("events = %#v, want RunStarted, RunStatusChanged, SkillSkipped", result.Events)
	}
}

func TestReducerUserMessageSkipsSelectionWhenDisabled(t *testing.T) {
	// Index present but SkillsEnabled false: selection is skipped, matching v0.2.
	reducer := NewReducer(ReducerConfig{
		SkillsEnabled: false,
		SkillIndex:    wsSkillIndex(),
	})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{SessionID: "session_1", RunStatus: domain.RunIdle, CreatedAt: now, UpdatedAt: now}
	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"how do I run a websocket runtime test?"}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(result.NextState.SelectedSkills) != 0 {
		t.Fatalf("SelectedSkills = %#v, want none when disabled", result.NextState.SelectedSkills)
	}
	if len(result.Events) != 2 {
		t.Fatalf("events len = %d, want 2 (no skill events when disabled)", len(result.Events))
	}
}

func TestReducerToolSuccessReusesSelectedSkillsWithoutReemitting(t *testing.T) {
	reducer := NewReducer(ReducerConfig{
		SkillsEnabled: true,
		SkillIndex:    wsSkillIndex(),
		SkillSelect:   skill.SelectConfig{MaxSelected: 3, MaxChars: 12000},
	})
	now := time.Unix(1700000000, 0).UTC()
	state := domain.SessionState{
		SessionID:      "session_1",
		CurrentRunID:   "run_1",
		RunStatus:      domain.RunWaitingTool,
		CreatedAt:      now,
		UpdatedAt:      now,
		SelectedSkills: []domain.SkillRef{{ID: "ws-test", Name: "ws", Version: "1"}},
	}
	event := domain.Event{
		EventID:   "evt_tool_success",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventToolCallSucceeded,
		Seq:       3,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"tool_call_id":"call_1","name":"echo","result":{"text":"hello"}}`),
	}

	result, err := reducer.Apply(context.Background(), state, event)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	// No skill events are re-emitted on a follow-up LLM call.
	if len(result.Events) != 0 {
		t.Fatalf("events len = %d, want 0 (no skill re-emit on tool success)", len(result.Events))
	}
	var payload DispatchLLMCallPayload
	if err := json.Unmarshal(result.Actions[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal action payload: %v", err)
	}
	if len(payload.SelectedSkills) != 1 || payload.SelectedSkills[0].ID != "ws-test" {
		t.Fatalf("payload SelectedSkills = %#v, want reused ws-test", payload.SelectedSkills)
	}
}
