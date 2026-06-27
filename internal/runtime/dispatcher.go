package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/worker"
)

type EventSink interface {
	Enqueue(ctx context.Context, event domain.Event) error
}

type DispatcherConfig struct {
	LLMProvider worker.LLMProvider
	EventSink   EventSink
	Now         func() time.Time
}

type Dispatcher struct {
	llmProvider worker.LLMProvider
	eventSink   EventSink
	now         func() time.Time
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Dispatcher{
		llmProvider: cfg.LLMProvider,
		eventSink:   cfg.EventSink,
		now:         now,
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, action domain.Action) error {
	if err := action.Validate(); err != nil {
		return err
	}
	switch action.Type {
	case domain.ActionDispatchLLMCall:
		return d.dispatchLLMCall(ctx, action)
	case domain.ActionEmitFinalAnswer:
		return d.dispatchFinalAnswer(ctx, action)
	default:
		return nil
	}
}

func (d *Dispatcher) dispatchLLMCall(ctx context.Context, action domain.Action) error {
	if d.llmProvider == nil {
		return fmt.Errorf("llm provider is required")
	}
	if d.eventSink == nil {
		return fmt.Errorf("event sink is required")
	}
	var payload DispatchLLMCallPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fmt.Errorf("decode llm action payload: %w", err)
	}

	if err := d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":started",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventLLMCallStarted,
		CreatedAt: d.now(),
		Payload:   json.RawMessage(`{}`),
	}); err != nil {
		return err
	}

	resp, err := d.llmProvider.Complete(ctx, worker.LLMRequest{Input: payload.Input})
	if err != nil {
		failurePayload, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
		if marshalErr != nil {
			return marshalErr
		}
		if err := d.eventSink.Enqueue(ctx, domain.Event{
			EventID:   action.ActionID + ":failed",
			SessionID: action.SessionID,
			RunID:     action.RunID,
			Type:      domain.EventLLMCallFailed,
			CreatedAt: d.now(),
			Payload:   failurePayload,
		}); err != nil {
			return err
		}
		return d.eventSink.Enqueue(ctx, domain.Event{
			EventID:   action.RunID + ":failed",
			SessionID: action.SessionID,
			RunID:     action.RunID,
			Type:      domain.EventRunFailed,
			CreatedAt: d.now(),
			Payload:   failurePayload,
		})
	}

	resultPayload, err := json.Marshal(LLMResponsePayload{
		Text:               resp.Text,
		ProviderResponseID: resp.ProviderResponseID,
	})
	if err != nil {
		return fmt.Errorf("marshal llm response event: %w", err)
	}
	return d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":received",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventLLMResponseReceived,
		CreatedAt: d.now(),
		Payload:   resultPayload,
	})
}

func (d *Dispatcher) dispatchFinalAnswer(ctx context.Context, action domain.Action) error {
	if d.eventSink == nil {
		return fmt.Errorf("event sink is required")
	}
	var payload FinalAnswerPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fmt.Errorf("decode final answer payload: %w", err)
	}
	finalPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal final answer event: %w", err)
	}
	if err := d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":emitted",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventFinalAnswerEmitted,
		CreatedAt: d.now(),
		Payload:   finalPayload,
	}); err != nil {
		return err
	}
	return d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.RunID + ":completed",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventRunCompleted,
		CreatedAt: d.now(),
		Payload:   json.RawMessage(`{}`),
	})
}
