package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/worker"
)

type EventSink interface {
	Enqueue(ctx context.Context, event domain.Event) error
}

type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
}

type DispatcherConfig struct {
	LLMProvider  worker.LLMProvider
	ToolExecutor ToolExecutor
	EventSink    EventSink
	Now          func() time.Time
}

type Dispatcher struct {
	llmProvider  worker.LLMProvider
	toolExecutor ToolExecutor
	eventSink    EventSink
	now          func() time.Time
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Dispatcher{
		llmProvider:  cfg.LLMProvider,
		toolExecutor: cfg.ToolExecutor,
		eventSink:    cfg.EventSink,
		now:          now,
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, action domain.Action) error {
	if err := action.Validate(); err != nil {
		return err
	}
	switch action.Type {
	case domain.ActionDispatchLLMCall:
		return d.dispatchLLMCall(ctx, action)
	case domain.ActionDispatchToolCall:
		return d.dispatchToolCall(ctx, action)
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

	stream, err := d.llmProvider.Stream(ctx, worker.LLMRequest{Input: payload.Input})
	if err != nil {
		return d.dispatchLLMFailure(ctx, action, err)
	}

	var text strings.Builder
	var providerResponseID string
	var toolCall *ToolCallPayload
	deltaSeq := 0
	for streamEvent := range stream {
		if streamEvent.Err != nil {
			return d.dispatchLLMFailure(ctx, action, streamEvent.Err)
		}
		if streamEvent.ProviderResponseID != "" {
			providerResponseID = streamEvent.ProviderResponseID
		}
		if streamEvent.Delta != "" {
			text.WriteString(streamEvent.Delta)
			deltaSeq++
			deltaPayload, err := json.Marshal(AssistantDeltaPayload{
				Delta:              streamEvent.Delta,
				ProviderResponseID: streamEvent.ProviderResponseID,
			})
			if err != nil {
				return fmt.Errorf("marshal assistant delta event: %w", err)
			}
			if err := d.eventSink.Enqueue(ctx, domain.Event{
				EventID:   fmt.Sprintf("%s:delta:%d", action.ActionID, deltaSeq),
				SessionID: action.SessionID,
				RunID:     action.RunID,
				Type:      domain.EventAssistantDelta,
				CreatedAt: d.now(),
				Payload:   deltaPayload,
			}); err != nil {
				return err
			}
		}
		if streamEvent.ToolCall != nil {
			toolCall = &ToolCallPayload{
				ToolCallID: streamEvent.ToolCall.ToolCallID,
				Name:       streamEvent.ToolCall.Name,
				Args:       streamEvent.ToolCall.Args,
			}
		}
		if streamEvent.Done {
			break
		}
	}
	if text.Len() == 0 && toolCall == nil {
		return d.dispatchLLMFailure(ctx, action, fmt.Errorf("llm stream contained no output text"))
	}

	resultPayload, err := json.Marshal(LLMResponsePayload{
		Text:               text.String(),
		ProviderResponseID: providerResponseID,
		ToolCall:           toolCall,
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

func (d *Dispatcher) dispatchToolCall(ctx context.Context, action domain.Action) error {
	if d.toolExecutor == nil {
		return fmt.Errorf("tool executor is required")
	}
	if d.eventSink == nil {
		return fmt.Errorf("event sink is required")
	}
	var payload DispatchToolCallPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fmt.Errorf("decode tool action payload: %w", err)
	}
	startedPayload, err := json.Marshal(ToolCallPayload{
		ToolCallID: payload.ToolCallID,
		Name:       payload.Name,
		Args:       payload.Args,
	})
	if err != nil {
		return fmt.Errorf("marshal tool started payload: %w", err)
	}
	if err := d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":started",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventToolCallStarted,
		CreatedAt: d.now(),
		Payload:   startedPayload,
	}); err != nil {
		return err
	}

	result, err := d.toolExecutor.Execute(ctx, payload.Name, payload.Args)
	if err != nil {
		return d.dispatchToolFailure(ctx, action, payload, err)
	}
	resultPayload, err := json.Marshal(ToolCallResultPayload{
		ToolCallID: payload.ToolCallID,
		Name:       payload.Name,
		Result:     result,
	})
	if err != nil {
		return fmt.Errorf("marshal tool result payload: %w", err)
	}
	return d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":succeeded",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventToolCallSucceeded,
		CreatedAt: d.now(),
		Payload:   resultPayload,
	})
}

func (d *Dispatcher) dispatchToolFailure(ctx context.Context, action domain.Action, payload DispatchToolCallPayload, cause error) error {
	failurePayload, marshalErr := json.Marshal(ToolCallResultPayload{
		ToolCallID: payload.ToolCallID,
		Name:       payload.Name,
		Error:      cause.Error(),
	})
	if marshalErr != nil {
		return marshalErr
	}
	if err := d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":failed",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventToolCallFailed,
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

func (d *Dispatcher) dispatchLLMFailure(ctx context.Context, action domain.Action, cause error) error {
	failurePayload, marshalErr := json.Marshal(map[string]string{"error": cause.Error()})
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
