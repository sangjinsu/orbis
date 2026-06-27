package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

type EventSink interface {
	Enqueue(ctx context.Context, event domain.Event) error
}

// ToolRunner executes a single tool call and returns a structured outcome. The
// Tool Worker is the only implementation; the dispatcher turns outcomes into
// events but never executes tools itself.
type ToolRunner interface {
	Run(ctx context.Context, req worker.ToolRequest) worker.ToolOutcome
}

type DispatcherConfig struct {
	LLMProvider worker.LLMProvider
	ToolRunner  ToolRunner
	ToolSchemas []tool.ToolSchema
	EventSink   EventSink
	Now         func() time.Time
}

type Dispatcher struct {
	llmProvider worker.LLMProvider
	toolRunner  ToolRunner
	toolSchemas []tool.ToolSchema
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
		toolRunner:  cfg.ToolRunner,
		toolSchemas: cfg.ToolSchemas,
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
	case domain.ActionDispatchToolCall:
		return d.dispatchToolCall(ctx, action)
	case domain.ActionScheduleTimer:
		return d.dispatchScheduleTimer(ctx, action)
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

	stream, err := d.llmProvider.Stream(ctx, worker.LLMRequest{
		Input:    payload.Input,
		Messages: payload.Messages,
		Tools:    d.toolSchemas,
	})
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
	if d.toolRunner == nil {
		return fmt.Errorf("tool runner is required")
	}
	if d.eventSink == nil {
		return fmt.Errorf("event sink is required")
	}
	var payload DispatchToolCallPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fmt.Errorf("decode tool action payload: %w", err)
	}
	attempt := payload.Attempt
	if attempt < 1 {
		attempt = 1
	}

	started := d.baseToolEvent(action, payload, attempt)
	started.Args = payload.Args
	if err := d.emitToolEvent(ctx, action, domain.EventToolCallStarted, started, ":started"); err != nil {
		return err
	}

	outcome := d.toolRunner.Run(ctx, worker.ToolRequest{
		SessionID:      action.SessionID,
		RunID:          action.RunID,
		ToolCallID:     payload.ToolCallID,
		ToolName:       payload.Name,
		Args:           payload.Args,
		IdempotencyKey: action.IdempotencyKey,
		Attempt:        attempt,
		MaxAttempts:    payload.MaxAttempts,
		Timeout:        payload.Timeout,
	})

	switch outcome.Status {
	case worker.ToolOutcomeSucceeded:
		return d.emitToolSucceeded(ctx, action, payload, attempt, outcome)
	case worker.ToolOutcomeDeduplicated:
		dedup := d.baseToolEvent(action, payload, attempt)
		dedup.Result = outcome.Result
		if err := d.emitToolEvent(ctx, action, domain.EventToolCallDeduplicated, dedup, ":deduplicated"); err != nil {
			return err
		}
		return d.emitToolSucceeded(ctx, action, payload, attempt, outcome)
	case worker.ToolOutcomeRejected:
		rejected := d.baseToolEvent(action, payload, attempt)
		rejected.ReasonCode = outcome.ReasonCode
		rejected.Error = outcome.Error
		return d.emitToolEvent(ctx, action, domain.EventToolCallRejected, rejected, ":rejected")
	default:
		eventType := domain.EventToolCallFailed
		if outcome.TimedOut {
			eventType = domain.EventToolCallTimedOut
		}
		failed := d.baseToolEvent(action, payload, attempt)
		failed.Args = payload.Args
		failed.DurationMS = outcome.DurationMS
		failed.Error = outcome.Error
		failed.ReasonCode = outcome.ReasonCode
		failed.Retryable = outcome.Retryable
		return d.emitToolEvent(ctx, action, eventType, failed, ":failed")
	}
}

// baseToolEvent builds the fields shared by every tool lifecycle event; callers
// set the outcome-specific fields (result, error, reason_code, ...) before emit.
func (d *Dispatcher) baseToolEvent(action domain.Action, payload DispatchToolCallPayload, attempt int) ToolCallEventPayload {
	return ToolCallEventPayload{
		ToolCallID:     payload.ToolCallID,
		Name:           payload.Name,
		IdempotencyKey: action.IdempotencyKey,
		Attempt:        attempt,
		MaxAttempts:    payload.MaxAttempts,
	}
}

func (d *Dispatcher) emitToolSucceeded(ctx context.Context, action domain.Action, payload DispatchToolCallPayload, attempt int, outcome worker.ToolOutcome) error {
	succeeded := d.baseToolEvent(action, payload, attempt)
	succeeded.DurationMS = outcome.DurationMS
	succeeded.Result = outcome.Result
	return d.emitToolEvent(ctx, action, domain.EventToolCallSucceeded, succeeded, ":succeeded")
}

func (d *Dispatcher) emitToolEvent(ctx context.Context, action domain.Action, eventType domain.EventType, payload ToolCallEventPayload, suffix string) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}
	return d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + suffix,
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      eventType,
		CreatedAt: d.now(),
		Payload:   encoded,
	})
}

func (d *Dispatcher) dispatchScheduleTimer(ctx context.Context, action domain.Action) error {
	if d.eventSink == nil {
		return fmt.Errorf("event sink is required")
	}
	var payload ScheduleTimerPayload
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fmt.Errorf("decode schedule timer payload: %w", err)
	}
	if payload.Delay > 0 {
		timer := time.NewTimer(payload.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	firedPayload, err := json.Marshal(TimerFiredPayload{Kind: payload.Kind, ToolCall: payload.ToolCall})
	if err != nil {
		return fmt.Errorf("marshal timer fired payload: %w", err)
	}
	return d.eventSink.Enqueue(ctx, domain.Event{
		EventID:   action.ActionID + ":fired",
		SessionID: action.SessionID,
		RunID:     action.RunID,
		Type:      domain.EventTimerFired,
		CreatedAt: d.now(),
		Payload:   firedPayload,
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
