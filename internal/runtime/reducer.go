package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sangjinsu/orbis/internal/domain"
)

type Reducer struct{}

type ReduceResult struct {
	NextState domain.SessionState
	Actions   []domain.Action
	Events    []domain.Event
}

type UserMessagePayload struct {
	Text string `json:"text"`
}

type DispatchLLMCallPayload struct {
	Input string `json:"input"`
}

type LLMResponsePayload struct {
	Text               string `json:"text"`
	ProviderResponseID string `json:"provider_response_id"`
}

type FailurePayload struct {
	Error string `json:"error"`
}

type FinalAnswerPayload struct {
	Text               string `json:"text"`
	ProviderResponseID string `json:"provider_response_id"`
}

func (Reducer) Apply(ctx context.Context, state domain.SessionState, event domain.Event) (ReduceResult, error) {
	_ = ctx

	next := state
	next.LastEventSeq = event.Seq
	next.UpdatedAt = event.CreatedAt
	if next.SessionID == "" {
		next.SessionID = event.SessionID
		next.CreatedAt = event.CreatedAt
	}

	if next.RunStatus == domain.RunCancelled && event.Type != domain.EventRunCancelled {
		return ReduceResult{NextState: next}, nil
	}

	switch event.Type {
	case domain.EventUserMessageReceived:
		return reduceUserMessage(next, event)
	case domain.EventLLMResponseReceived:
		return reduceLLMResponse(next, event)
	case domain.EventLLMCallFailed:
		return reduceFailure(next, event)
	case domain.EventFinalAnswerEmitted:
		return ReduceResult{NextState: next}, nil
	case domain.EventRunCompleted:
		next.RunStatus = domain.RunCompleted
		return ReduceResult{NextState: next}, nil
	case domain.EventRunFailed:
		next.RunStatus = domain.RunFailed
		return ReduceResult{NextState: next}, nil
	case domain.EventRunCancelled:
		next.RunStatus = domain.RunCancelled
		return ReduceResult{NextState: next}, nil
	default:
		return ReduceResult{NextState: next}, nil
	}
}

func reduceFailure(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload FailurePayload
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return ReduceResult{}, fmt.Errorf("decode failure payload: %w", err)
		}
	}
	state.RunStatus = domain.RunFailed
	return ReduceResult{NextState: state}, nil
}

func reduceUserMessage(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload UserMessagePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode user message payload: %w", err)
	}
	if payload.Text == "" {
		return ReduceResult{}, fmt.Errorf("user message text is required")
	}

	state.CurrentRunID = event.RunID
	state.RunStatus = domain.RunWaitingLLM
	state.MessageHistory = append(state.MessageHistory, domain.Message{
		Role:      "user",
		Content:   payload.Text,
		CreatedAt: event.CreatedAt,
	})

	actionPayload, err := json.Marshal(DispatchLLMCallPayload{Input: payload.Text})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal llm action payload: %w", err)
	}
	action := domain.Action{
		ActionID:       event.RunID + ":llm:" + event.EventID,
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionDispatchLLMCall,
		IdempotencyKey: event.RunID + ":DispatchLLMCall:" + event.EventID,
		Payload:        actionPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}

	return ReduceResult{
		NextState: state,
		Actions:   []domain.Action{action},
	}, nil
}

func reduceLLMResponse(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload LLMResponsePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode llm response payload: %w", err)
	}
	if payload.Text == "" {
		return ReduceResult{}, fmt.Errorf("llm response text is required")
	}

	state.RunStatus = domain.RunCompleted
	state.MessageHistory = append(state.MessageHistory, domain.Message{
		Role:      "assistant",
		Content:   payload.Text,
		CreatedAt: event.CreatedAt,
	})

	actionPayload, err := json.Marshal(FinalAnswerPayload{
		Text:               payload.Text,
		ProviderResponseID: payload.ProviderResponseID,
	})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal final answer payload: %w", err)
	}
	action := domain.Action{
		ActionID:       event.RunID + ":final:" + event.EventID,
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionEmitFinalAnswer,
		IdempotencyKey: event.RunID + ":EmitFinalAnswer:" + event.EventID,
		Payload:        actionPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}

	return ReduceResult{
		NextState: state,
		Actions:   []domain.Action{action},
	}, nil
}
