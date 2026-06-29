package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/worker"
)

func TestDispatcherRoutesLLMActionToProviderAndEnqueuesResultEvents(t *testing.T) {
	sink := &recordingEventSink{}
	provider := &fakeLLMProvider{streamEvents: []worker.LLMStreamEvent{
		{Delta: "hel"},
		{Delta: "lo", ProviderResponseID: "resp_1"},
		{Done: true, ProviderResponseID: "resp_1"},
	}}
	dispatcher := NewDispatcher(DispatcherConfig{
		LLMProvider: provider,
		EventSink:   sink,
	})

	payload, err := json.Marshal(DispatchLLMCallPayload{Input: "Say hello"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_1",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchLLMCall,
		IdempotencyKey: "run_1:llm:act_1",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if provider.seenInput != "Say hello" {
		t.Fatalf("provider input = %q, want Say hello", provider.seenInput)
	}
	if len(sink.events) != 4 {
		t.Fatalf("events len = %d, want 4", len(sink.events))
	}
	if sink.events[0].Type != domain.EventLLMCallStarted {
		t.Fatalf("first event = %q, want %q", sink.events[0].Type, domain.EventLLMCallStarted)
	}
	if sink.events[1].Type != domain.EventAssistantDelta || sink.events[2].Type != domain.EventAssistantDelta {
		t.Fatalf("delta events = %q, %q; want AssistantDelta, AssistantDelta", sink.events[1].Type, sink.events[2].Type)
	}
	if sink.events[3].Type != domain.EventLLMResponseReceived {
		t.Fatalf("fourth event = %q, want %q", sink.events[3].Type, domain.EventLLMResponseReceived)
	}
	var firstDelta AssistantDeltaPayload
	if err := json.Unmarshal(sink.events[1].Payload, &firstDelta); err != nil {
		t.Fatalf("unmarshal first delta payload: %v", err)
	}
	if firstDelta.Delta != "hel" {
		t.Fatalf("first delta = %q, want hel", firstDelta.Delta)
	}
	var result LLMResponsePayload
	if err := json.Unmarshal(sink.events[3].Payload, &result); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if result.Text != "hello" || result.ProviderResponseID != "resp_1" {
		t.Fatalf("result = %#v, want hello/resp_1", result)
	}
}

func TestDispatcherInjectsSelectedSkillBodiesAsInstructions(t *testing.T) {
	sink := &recordingEventSink{}
	provider := &fakeLLMProvider{streamEvents: []worker.LLMStreamEvent{
		{Delta: "ok", ProviderResponseID: "resp_1"},
		{Done: true, ProviderResponseID: "resp_1"},
	}}
	bodies := fakeSkillBodies{"ws-test": "WebSocket runtime test body"}
	dispatcher := NewDispatcher(DispatcherConfig{
		LLMProvider: provider,
		SkillBodies: bodies,
		EventSink:   sink,
	})

	payload, err := json.Marshal(DispatchLLMCallPayload{
		Input:          "how do I test?",
		SelectedSkills: []domain.SkillRef{{ID: "ws-test", Name: "ws"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_1",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchLLMCall,
		IdempotencyKey: "run_1:llm:act_1",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !strings.Contains(provider.seenInstructions, "<orbis_skills>") {
		t.Fatalf("instructions = %q, want <orbis_skills> block", provider.seenInstructions)
	}
	if !strings.Contains(provider.seenInstructions, "WebSocket runtime test body") {
		t.Fatalf("instructions = %q, want skill body text", provider.seenInstructions)
	}
}

func TestDispatcherLeavesInstructionsEmptyWithoutSelectedSkills(t *testing.T) {
	sink := &recordingEventSink{}
	provider := &fakeLLMProvider{streamEvents: []worker.LLMStreamEvent{
		{Delta: "ok"},
		{Done: true},
	}}
	dispatcher := NewDispatcher(DispatcherConfig{
		LLMProvider: provider,
		SkillBodies: fakeSkillBodies{"ws-test": "body"},
		EventSink:   sink,
	})

	payload, err := json.Marshal(DispatchLLMCallPayload{Input: "hi"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_1",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchLLMCall,
		IdempotencyKey: "run_1:llm:act_1",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if provider.seenInstructions != "" {
		t.Fatalf("instructions = %q, want empty without selected skills", provider.seenInstructions)
	}
}

func TestDispatcherRoutesFinalAnswerToRuntimeEvents(t *testing.T) {
	sink := &recordingEventSink{}
	dispatcher := NewDispatcher(DispatcherConfig{EventSink: sink})
	payload, err := json.Marshal(FinalAnswerPayload{Text: "hello", ProviderResponseID: "resp_1"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_final",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionEmitFinalAnswer,
		IdempotencyKey: "run_1:final:act_final",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if len(sink.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != domain.EventFinalAnswerEmitted {
		t.Fatalf("first event = %q, want %q", sink.events[0].Type, domain.EventFinalAnswerEmitted)
	}
	if sink.events[1].Type != domain.EventRunCompleted {
		t.Fatalf("second event = %q, want %q", sink.events[1].Type, domain.EventRunCompleted)
	}
}

func TestDispatcherRoutesToolActionToToolRunner(t *testing.T) {
	sink := &recordingEventSink{}
	runner := &fakeToolRunner{outcome: worker.ToolOutcome{
		Status: worker.ToolOutcomeSucceeded,
		Result: json.RawMessage(`{"text":"hello"}`),
	}}
	dispatcher := NewDispatcher(DispatcherConfig{
		EventSink:  sink,
		ToolRunner: runner,
	})
	payload, err := json.Marshal(DispatchToolCallPayload{
		ToolCallID:  "call_1",
		Name:        "echo",
		Args:        json.RawMessage(`{"text":"hello"}`),
		Attempt:     1,
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_tool",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchToolCall,
		IdempotencyKey: "run_1:tool:call_1",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if runner.seenReq.ToolName != "echo" || string(runner.seenReq.Args) != `{"text":"hello"}` {
		t.Fatalf("runner saw name=%q args=%s, want echo args", runner.seenReq.ToolName, runner.seenReq.Args)
	}
	if runner.seenReq.IdempotencyKey != "run_1:tool:call_1" {
		t.Fatalf("runner idempotency key = %q, want run_1:tool:call_1", runner.seenReq.IdempotencyKey)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != domain.EventToolCallStarted || sink.events[1].Type != domain.EventToolCallSucceeded {
		t.Fatalf("events = %q, %q; want ToolCallStarted, ToolCallSucceeded", sink.events[0].Type, sink.events[1].Type)
	}
	var result ToolCallEventPayload
	if err := json.Unmarshal(sink.events[1].Payload, &result); err != nil {
		t.Fatalf("unmarshal tool result payload: %v", err)
	}
	if result.ToolCallID != "call_1" || result.Name != "echo" || string(result.Result) != `{"text":"hello"}` {
		t.Fatalf("result = %#v result=%s, want echo result", result, result.Result)
	}
}

func TestDispatcherToolRejectionEmitsRejectedOnly(t *testing.T) {
	sink := &recordingEventSink{}
	runner := &fakeToolRunner{outcome: worker.ToolOutcome{
		Status:     worker.ToolOutcomeRejected,
		ReasonCode: "toolset_not_allowed",
		Error:      "denied",
	}}
	dispatcher := NewDispatcher(DispatcherConfig{EventSink: sink, ToolRunner: runner})
	payload, _ := json.Marshal(DispatchToolCallPayload{ToolCallID: "call_1", Name: "mock.dangerous", Attempt: 1, MaxAttempts: 2})
	action := domain.Action{
		ActionID: "act_tool", SessionID: "session_1", RunID: "run_1",
		Type: domain.ActionDispatchToolCall, IdempotencyKey: "run_1:tool:call_1", Payload: payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(sink.events))
	}
	if sink.events[1].Type != domain.EventToolCallRejected {
		t.Fatalf("second event = %q, want ToolCallRejected (no RunFailed from dispatcher)", sink.events[1].Type)
	}
}

func TestDispatcherToolFailureEmitsFailedWithRetryable(t *testing.T) {
	sink := &recordingEventSink{}
	runner := &fakeToolRunner{outcome: worker.ToolOutcome{
		Status:     worker.ToolOutcomeFailed,
		ReasonCode: "error",
		Error:      "boom",
		Retryable:  true,
	}}
	dispatcher := NewDispatcher(DispatcherConfig{EventSink: sink, ToolRunner: runner})
	payload, _ := json.Marshal(DispatchToolCallPayload{ToolCallID: "call_1", Name: "mock.fail_once", Attempt: 1, MaxAttempts: 2})
	action := domain.Action{
		ActionID: "act_tool", SessionID: "session_1", RunID: "run_1",
		Type: domain.ActionDispatchToolCall, IdempotencyKey: "run_1:tool:call_1", Payload: payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if sink.events[len(sink.events)-1].Type != domain.EventToolCallFailed {
		t.Fatalf("last event = %q, want ToolCallFailed", sink.events[len(sink.events)-1].Type)
	}
	var failed ToolCallEventPayload
	if err := json.Unmarshal(sink.events[1].Payload, &failed); err != nil {
		t.Fatalf("unmarshal failed payload: %v", err)
	}
	if !failed.Retryable || failed.Attempt != 1 || failed.MaxAttempts != 2 {
		t.Fatalf("failed payload = %#v, want retryable attempt 1/2", failed)
	}
}

func TestDispatcherScheduleTimerFiresTimerEvent(t *testing.T) {
	sink := &recordingEventSink{}
	dispatcher := NewDispatcher(DispatcherConfig{EventSink: sink})
	payload, _ := json.Marshal(ScheduleTimerPayload{
		Kind:     "tool_retry",
		Delay:    0,
		ToolCall: &DispatchToolCallPayload{ToolCallID: "call_1", Name: "echo", Attempt: 2, MaxAttempts: 2, IdempotencyKey: "run_1:tool:call_1"},
	})
	action := domain.Action{
		ActionID: "act_retry", SessionID: "session_1", RunID: "run_1",
		Type: domain.ActionScheduleTimer, IdempotencyKey: "run_1:ScheduleTimer:call_1:2", Payload: payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(sink.events) != 1 || sink.events[0].Type != domain.EventTimerFired {
		t.Fatalf("events = %#v, want one TimerFired", sink.events)
	}
	var fired TimerFiredPayload
	if err := json.Unmarshal(sink.events[0].Payload, &fired); err != nil {
		t.Fatalf("unmarshal timer payload: %v", err)
	}
	if fired.Kind != "tool_retry" || fired.ToolCall == nil || fired.ToolCall.Attempt != 2 {
		t.Fatalf("fired payload = %#v, want tool_retry attempt 2", fired)
	}
}

func TestDispatcherProviderFailureEmitsLLMAndRunFailure(t *testing.T) {
	sink := &recordingEventSink{}
	provider := &fakeLLMProvider{err: errors.New("provider timeout")}
	dispatcher := NewDispatcher(DispatcherConfig{
		LLMProvider: provider,
		EventSink:   sink,
	})
	payload, err := json.Marshal(DispatchLLMCallPayload{Input: "Say hello"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_1",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchLLMCall,
		IdempotencyKey: "run_1:llm:act_1",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if len(sink.events) != 3 {
		t.Fatalf("events len = %d, want 3", len(sink.events))
	}
	if sink.events[0].Type != domain.EventLLMCallStarted {
		t.Fatalf("first event = %q, want %q", sink.events[0].Type, domain.EventLLMCallStarted)
	}
	if sink.events[1].Type != domain.EventLLMCallFailed {
		t.Fatalf("second event = %q, want %q", sink.events[1].Type, domain.EventLLMCallFailed)
	}
	if sink.events[2].Type != domain.EventRunFailed {
		t.Fatalf("third event = %q, want %q", sink.events[2].Type, domain.EventRunFailed)
	}
}

type recordingEventSink struct {
	events []domain.Event
}

func (s *recordingEventSink) Enqueue(ctx context.Context, event domain.Event) error {
	_ = ctx
	s.events = append(s.events, event)
	return nil
}

type fakeLLMProvider struct {
	response         worker.LLMResponse
	streamEvents     []worker.LLMStreamEvent
	err              error
	seenInput        string
	seenInstructions string
}

// fakeSkillBodies is an in-memory skill.Bodies for dispatcher tests.
type fakeSkillBodies map[string]string

func (f fakeSkillBodies) Body(id string) (string, bool) {
	body, ok := f[id]
	return body, ok
}

type fakeToolRunner struct {
	outcome worker.ToolOutcome
	seenReq worker.ToolRequest
}

func (r *fakeToolRunner) Run(ctx context.Context, req worker.ToolRequest) worker.ToolOutcome {
	_ = ctx
	r.seenReq = req
	return r.outcome
}

func (p *fakeLLMProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	p.seenInput = req.Input
	p.seenInstructions = req.Instructions
	if p.err != nil {
		return worker.LLMResponse{}, p.err
	}
	return p.response, nil
}

func (p *fakeLLMProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	p.seenInput = req.Input
	p.seenInstructions = req.Instructions
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan worker.LLMStreamEvent, len(p.streamEvents)+2)
	if len(p.streamEvents) == 0 {
		ch <- worker.LLMStreamEvent{Delta: p.response.Text, ProviderResponseID: p.response.ProviderResponseID}
		ch <- worker.LLMStreamEvent{Done: true, ProviderResponseID: p.response.ProviderResponseID}
	} else {
		for _, event := range p.streamEvents {
			ch <- event
		}
	}
	close(ch)
	return ch, nil
}
