package runtime

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDispatcherRoutesToolActionToToolExecutor(t *testing.T) {
	sink := &recordingEventSink{}
	executor := &fakeToolExecutor{result: json.RawMessage(`{"text":"hello"}`)}
	dispatcher := NewDispatcher(DispatcherConfig{
		EventSink:    sink,
		ToolExecutor: executor,
	})
	payload, err := json.Marshal(DispatchToolCallPayload{
		ToolCallID: "call_1",
		Name:       "echo",
		Args:       json.RawMessage(`{"text":"hello"}`),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	action := domain.Action{
		ActionID:       "act_tool",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           domain.ActionDispatchToolCall,
		IdempotencyKey: "run_1:tool:act_tool",
		Payload:        payload,
	}

	if err := dispatcher.Dispatch(context.Background(), action); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if executor.seenName != "echo" || string(executor.seenArgs) != `{"text":"hello"}` {
		t.Fatalf("executor saw name=%q args=%s, want echo args", executor.seenName, executor.seenArgs)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != domain.EventToolCallStarted || sink.events[1].Type != domain.EventToolCallSucceeded {
		t.Fatalf("events = %q, %q; want ToolCallStarted, ToolCallSucceeded", sink.events[0].Type, sink.events[1].Type)
	}
	var result ToolCallResultPayload
	if err := json.Unmarshal(sink.events[1].Payload, &result); err != nil {
		t.Fatalf("unmarshal tool result payload: %v", err)
	}
	if result.ToolCallID != "call_1" || result.Name != "echo" || string(result.Result) != `{"text":"hello"}` {
		t.Fatalf("result = %#v result=%s, want echo result", result, result.Result)
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
	response     worker.LLMResponse
	streamEvents []worker.LLMStreamEvent
	err          error
	seenInput    string
}

type fakeToolExecutor struct {
	result   json.RawMessage
	err      error
	seenName string
	seenArgs json.RawMessage
}

func (e *fakeToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	_ = ctx
	e.seenName = name
	e.seenArgs = args
	if e.err != nil {
		return nil, e.err
	}
	return e.result, nil
}

func (p *fakeLLMProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	p.seenInput = req.Input
	if p.err != nil {
		return worker.LLMResponse{}, p.err
	}
	return p.response, nil
}

func (p *fakeLLMProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	p.seenInput = req.Input
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
