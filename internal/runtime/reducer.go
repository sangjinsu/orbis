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

type RunStatusChangedPayload struct {
	Status domain.RunStatus `json:"status"`
}

type LLMResponsePayload struct {
	Text               string           `json:"text,omitempty"`
	ProviderResponseID string           `json:"provider_response_id,omitempty"`
	ToolCall           *ToolCallPayload `json:"tool_call,omitempty"`
}

type AssistantDeltaPayload struct {
	Delta              string `json:"delta"`
	ProviderResponseID string `json:"provider_response_id,omitempty"`
}

type ToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
}

type DispatchToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
}

type ToolCallResultPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
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
	case domain.EventToolCallSucceeded:
		return reduceToolCallSucceeded(next, event)
	case domain.EventToolCallFailed:
		return reduceFailure(next, event)
	case domain.EventTimerFired:
		return reduceTimerFired(next, event)
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

func reduceTimerFired(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	if isTerminalRunStatus(state.RunStatus) {
		return ReduceResult{NextState: state}, nil
	}
	state.RunStatus = domain.RunFailed
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{"error":"run timeout"}`)
	}
	return ReduceResult{
		NextState: state,
		Events: []domain.Event{{
			EventID:   event.RunID + ":failed:timer",
			SessionID: event.SessionID,
			RunID:     event.RunID,
			Type:      domain.EventRunFailed,
			CreatedAt: event.CreatedAt,
			Payload:   payload,
		}},
	}, nil
}

func isTerminalRunStatus(status domain.RunStatus) bool {
	return status == domain.RunCompleted || status == domain.RunFailed || status == domain.RunCancelled
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
	statusPayload, err := json.Marshal(RunStatusChangedPayload{Status: state.RunStatus})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal run status changed payload: %w", err)
	}

	return ReduceResult{
		NextState: state,
		Events: []domain.Event{
			{
				EventID:   event.RunID + ":started",
				SessionID: event.SessionID,
				RunID:     event.RunID,
				Type:      domain.EventRunStarted,
				CreatedAt: event.CreatedAt,
				Payload:   json.RawMessage(`{}`),
			},
			{
				EventID:   event.RunID + ":status:waiting_llm",
				SessionID: event.SessionID,
				RunID:     event.RunID,
				Type:      domain.EventRunStatusChanged,
				CreatedAt: event.CreatedAt,
				Payload:   statusPayload,
			},
		},
		Actions: []domain.Action{action},
	}, nil
}

func reduceLLMResponse(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload LLMResponsePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode llm response payload: %w", err)
	}
	if payload.ToolCall != nil {
		return reduceLLMToolCall(state, event, *payload.ToolCall)
	}
	if payload.Text == "" {
		return ReduceResult{}, fmt.Errorf("llm response text or tool_call is required")
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

func reduceLLMToolCall(state domain.SessionState, event domain.Event, toolCall ToolCallPayload) (ReduceResult, error) {
	if toolCall.ToolCallID == "" {
		return ReduceResult{}, fmt.Errorf("tool_call_id is required")
	}
	if toolCall.Name == "" {
		return ReduceResult{}, fmt.Errorf("tool name is required")
	}
	if len(toolCall.Args) == 0 {
		toolCall.Args = json.RawMessage(`{}`)
	}

	state.RunStatus = domain.RunWaitingTool
	actionPayload, err := json.Marshal(DispatchToolCallPayload{
		ToolCallID: toolCall.ToolCallID,
		Name:       toolCall.Name,
		Args:       toolCall.Args,
	})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal tool action payload: %w", err)
	}
	action := domain.Action{
		ActionID:       event.RunID + ":tool:" + event.EventID,
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionDispatchToolCall,
		IdempotencyKey: event.RunID + ":DispatchToolCall:" + event.EventID,
		Payload:        actionPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}
	return ReduceResult{NextState: state, Actions: []domain.Action{action}}, nil
}

func reduceToolCallSucceeded(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload ToolCallResultPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode tool result payload: %w", err)
	}
	if payload.ToolCallID == "" {
		return ReduceResult{}, fmt.Errorf("tool_call_id is required")
	}
	state.RunStatus = domain.RunWaitingLLM
	resultText := string(payload.Result)
	state.MessageHistory = append(state.MessageHistory, domain.Message{
		Role:      "tool",
		Content:   resultText,
		CreatedAt: event.CreatedAt,
	})

	actionPayload, err := json.Marshal(DispatchLLMCallPayload{Input: resultText})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal follow-up llm action payload: %w", err)
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
	return ReduceResult{NextState: state, Actions: []domain.Action{action}}, nil
}
