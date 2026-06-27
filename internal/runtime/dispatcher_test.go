package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/worker"
)

func TestDispatcherRoutesLLMActionToProviderAndEnqueuesResultEvents(t *testing.T) {
	sink := &recordingEventSink{}
	provider := &fakeLLMProvider{response: worker.LLMResponse{Text: "hello", ProviderResponseID: "resp_1"}}
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
	if len(sink.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != domain.EventLLMCallStarted {
		t.Fatalf("first event = %q, want %q", sink.events[0].Type, domain.EventLLMCallStarted)
	}
	if sink.events[1].Type != domain.EventLLMResponseReceived {
		t.Fatalf("second event = %q, want %q", sink.events[1].Type, domain.EventLLMResponseReceived)
	}
	var result LLMResponsePayload
	if err := json.Unmarshal(sink.events[1].Payload, &result); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if result.Text != "hello" || result.ProviderResponseID != "resp_1" {
		t.Fatalf("result = %#v, want hello/resp_1", result)
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
	response  worker.LLMResponse
	seenInput string
}

func (p *fakeLLMProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	p.seenInput = req.Input
	return p.response, nil
}

func (p *fakeLLMProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	_ = ctx
	_ = req
	panic("not used")
}
