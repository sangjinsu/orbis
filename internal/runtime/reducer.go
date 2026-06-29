package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

// ReducerConfig injects static tool-calling policy so the reducer can decide
// retries deterministically without performing side effects.
type ReducerConfig struct {
	ToolTimeout time.Duration
	Retry       tool.RetryPolicy

	// Skills (v1). When SkillsEnabled is true and SkillIndex is set, the reducer
	// selects skills once per run from an immutable in-memory snapshot — a pure,
	// deterministic computation with no I/O — and emits skill lifecycle events.
	// The zero value (disabled, nil index) preserves the v0.2 behavior of
	// skipping skill selection entirely.
	SkillsEnabled bool
	SkillIndex    skill.Index
	SkillSelect   skill.SelectConfig
}

type Reducer struct {
	cfg ReducerConfig
}

// NewReducer builds a configured reducer. The zero value Reducer{} is also
// valid and falls back to safe defaults.
func NewReducer(cfg ReducerConfig) Reducer {
	return Reducer{cfg: cfg}
}

func (r Reducer) retryPolicy() tool.RetryPolicy {
	if r.cfg.Retry.MaxAttempts == 0 {
		return tool.DefaultRetryPolicy()
	}
	return r.cfg.Retry
}

func (r Reducer) toolTimeout() time.Duration {
	if r.cfg.ToolTimeout <= 0 {
		return 5 * time.Second
	}
	return r.cfg.ToolTimeout
}

type ReduceResult struct {
	NextState domain.SessionState
	Actions   []domain.Action
	Events    []domain.Event
}

type UserMessagePayload struct {
	Text string `json:"text"`
}

type DispatchLLMCallPayload struct {
	Input    string              `json:"input"`
	Messages []worker.LLMMessage `json:"messages,omitempty"`
	// SelectedSkills are the skills the reducer chose for this run. The dispatcher
	// resolves their bodies from the in-memory store and renders them into the
	// LLM request Instructions; the reducer never carries body text itself.
	SelectedSkills []domain.SkillRef `json:"selected_skills,omitempty"`
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

// ToolCallPayload is an LLM-proposed tool call.
type ToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
}

// DispatchToolCallPayload is the action payload that drives the Tool Worker. It
// carries the attempt bookkeeping and timeout the reducer decided from config.
type DispatchToolCallPayload struct {
	ToolCallID     string          `json:"tool_call_id"`
	Name           string          `json:"name"`
	Args           json.RawMessage `json:"args"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Attempt        int             `json:"attempt,omitempty"`
	MaxAttempts    int             `json:"max_attempts,omitempty"`
	Timeout        time.Duration   `json:"timeout,omitempty"`
}

// ToolCallEventPayload is the shape of every tool lifecycle event.
type ToolCallEventPayload struct {
	ToolCallID     string          `json:"tool_call_id"`
	Name           string          `json:"tool_name,omitempty"`
	Args           json.RawMessage `json:"args,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Attempt        int             `json:"attempt,omitempty"`
	MaxAttempts    int             `json:"max_attempts,omitempty"`
	DurationMS     int64           `json:"duration_ms,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	ReasonCode     string          `json:"reason_code,omitempty"`
	Retryable      bool            `json:"retryable,omitempty"`
}

// ScheduleTimerPayload drives the timer worker. Kind distinguishes a tool retry
// backoff from a run timeout.
type ScheduleTimerPayload struct {
	Kind     string                   `json:"kind"`
	Delay    time.Duration            `json:"delay"`
	ToolCall *DispatchToolCallPayload `json:"tool_call,omitempty"`
}

// TimerFiredPayload is emitted when a scheduled timer elapses.
type TimerFiredPayload struct {
	Kind     string                   `json:"kind,omitempty"`
	Reason   string                   `json:"reason,omitempty"`
	ToolCall *DispatchToolCallPayload `json:"tool_call,omitempty"`
}

type FailurePayload struct {
	Error string `json:"error"`
}

type FinalAnswerPayload struct {
	Text               string `json:"text"`
	ProviderResponseID string `json:"provider_response_id"`
}

func (r Reducer) Apply(ctx context.Context, state domain.SessionState, event domain.Event) (ReduceResult, error) {
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
		return r.reduceUserMessage(next, event)
	case domain.EventLLMResponseReceived:
		return r.reduceLLMResponse(next, event)
	case domain.EventLLMCallFailed:
		return reduceFailure(next, event)
	case domain.EventToolCallSucceeded:
		return r.reduceToolCallSucceeded(next, event)
	case domain.EventToolCallFailed, domain.EventToolCallTimedOut:
		return r.reduceToolFailure(next, event)
	case domain.EventToolCallRejected:
		return reduceToolRejected(next, event)
	case domain.EventTimerFired:
		return r.reduceTimerFired(next, event)
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

func (r Reducer) reduceTimerFired(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	if domain.IsTerminalRunStatus(state.RunStatus) {
		return ReduceResult{NextState: state}, nil
	}
	var payload TimerFiredPayload
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	if payload.Kind == "tool_retry" && payload.ToolCall != nil {
		return r.reduceToolRetryTimer(state, event, *payload.ToolCall)
	}

	state.RunStatus = domain.RunFailed
	failPayload := event.Payload
	if len(failPayload) == 0 {
		failPayload = json.RawMessage(`{"error":"run timeout"}`)
	}
	return ReduceResult{
		NextState: state,
		Events: []domain.Event{{
			EventID:   event.RunID + ":failed:timer",
			SessionID: event.SessionID,
			RunID:     event.RunID,
			Type:      domain.EventRunFailed,
			CreatedAt: event.CreatedAt,
			Payload:   failPayload,
		}},
	}, nil
}

func (r Reducer) reduceToolRetryTimer(state domain.SessionState, event domain.Event, call DispatchToolCallPayload) (ReduceResult, error) {
	state.RunStatus = domain.RunWaitingTool
	actionPayload, err := json.Marshal(call)
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal retry tool action payload: %w", err)
	}
	action := domain.Action{
		ActionID:       fmt.Sprintf("%s:tool:%s:%d", event.RunID, call.ToolCallID, call.Attempt),
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionDispatchToolCall,
		IdempotencyKey: call.IdempotencyKey,
		Payload:        actionPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}
	retriedPayload, err := json.Marshal(ToolCallEventPayload{
		ToolCallID:     call.ToolCallID,
		Name:           call.Name,
		IdempotencyKey: call.IdempotencyKey,
		Attempt:        call.Attempt,
		MaxAttempts:    call.MaxAttempts,
	})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal tool retried payload: %w", err)
	}
	retried := domain.Event{
		EventID:   fmt.Sprintf("%s:tool_retried:%s:%d", event.RunID, call.ToolCallID, call.Attempt),
		SessionID: event.SessionID,
		RunID:     event.RunID,
		Type:      domain.EventToolCallRetried,
		CreatedAt: event.CreatedAt,
		Payload:   retriedPayload,
	}
	return ReduceResult{NextState: state, Events: []domain.Event{retried}, Actions: []domain.Action{action}}, nil
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

// reduceToolFailure decides whether to retry a failed/timed-out tool call or
// fail the run, using the static retry policy. It never executes a tool.
func (r Reducer) reduceToolFailure(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	if domain.IsTerminalRunStatus(state.RunStatus) {
		return ReduceResult{NextState: state}, nil
	}
	var payload ToolCallEventPayload
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return ReduceResult{}, fmt.Errorf("decode tool failure payload: %w", err)
		}
	}
	if payload.Retryable && payload.Attempt > 0 && payload.Attempt < payload.MaxAttempts {
		return r.scheduleToolRetry(state, event, payload)
	}

	state.RunStatus = domain.RunFailed
	failPayload := event.Payload
	if len(failPayload) == 0 {
		failPayload = json.RawMessage(`{"error":"tool failed"}`)
	}
	return ReduceResult{
		NextState: state,
		Events: []domain.Event{{
			EventID:   event.RunID + ":failed:tool",
			SessionID: event.SessionID,
			RunID:     event.RunID,
			Type:      domain.EventRunFailed,
			CreatedAt: event.CreatedAt,
			Payload:   failPayload,
		}},
	}, nil
}

func (r Reducer) scheduleToolRetry(state domain.SessionState, event domain.Event, failed ToolCallEventPayload) (ReduceResult, error) {
	nextAttempt := failed.Attempt + 1
	delay := r.retryPolicy().NextDelay(nextAttempt)
	nextCall := DispatchToolCallPayload{
		ToolCallID:     failed.ToolCallID,
		Name:           failed.Name,
		Args:           failed.Args,
		IdempotencyKey: failed.IdempotencyKey,
		Attempt:        nextAttempt,
		MaxAttempts:    failed.MaxAttempts,
		Timeout:        r.toolTimeout(),
	}
	timerPayload, err := json.Marshal(ScheduleTimerPayload{Kind: "tool_retry", Delay: delay, ToolCall: &nextCall})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal schedule timer payload: %w", err)
	}
	state.RunStatus = domain.RunWaitingTimer
	action := domain.Action{
		ActionID:       fmt.Sprintf("%s:tool_retry:%s:%d", event.RunID, failed.ToolCallID, nextAttempt),
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionScheduleTimer,
		IdempotencyKey: fmt.Sprintf("%s:ScheduleTimer:%s:%d", event.RunID, failed.ToolCallID, nextAttempt),
		Payload:        timerPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}
	scheduledPayload, err := json.Marshal(ToolCallEventPayload{
		ToolCallID:     failed.ToolCallID,
		Name:           failed.Name,
		IdempotencyKey: failed.IdempotencyKey,
		Attempt:        nextAttempt,
		MaxAttempts:    failed.MaxAttempts,
	})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal tool retry scheduled payload: %w", err)
	}
	scheduled := domain.Event{
		EventID:   fmt.Sprintf("%s:tool_retry_scheduled:%s:%d", event.RunID, failed.ToolCallID, nextAttempt),
		SessionID: event.SessionID,
		RunID:     event.RunID,
		Type:      domain.EventToolCallRetryScheduled,
		CreatedAt: event.CreatedAt,
		Payload:   scheduledPayload,
	}
	return ReduceResult{NextState: state, Events: []domain.Event{scheduled}, Actions: []domain.Action{action}}, nil
}

func reduceToolRejected(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	if domain.IsTerminalRunStatus(state.RunStatus) {
		return ReduceResult{NextState: state}, nil
	}
	state.RunStatus = domain.RunFailed
	failPayload := event.Payload
	if len(failPayload) == 0 {
		failPayload = json.RawMessage(`{"error":"tool rejected"}`)
	}
	return ReduceResult{
		NextState: state,
		Events: []domain.Event{{
			EventID:   event.RunID + ":failed:rejected",
			SessionID: event.SessionID,
			RunID:     event.RunID,
			Type:      domain.EventRunFailed,
			CreatedAt: event.CreatedAt,
			Payload:   failPayload,
		}},
	}, nil
}

func (r Reducer) reduceUserMessage(state domain.SessionState, event domain.Event) (ReduceResult, error) {
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

	// Select skills once per run from the in-memory snapshot (pure, no I/O) and
	// record them on the state so follow-up LLM calls reuse the same set.
	refs, skillEvents, err := r.selectSkills(event, payload.Text)
	if err != nil {
		return ReduceResult{}, err
	}
	state.SelectedSkills = refs

	actionPayload, err := json.Marshal(DispatchLLMCallPayload{
		Input:          payload.Text,
		Messages:       BuildLLMMessages(state),
		SelectedSkills: refs,
	})
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

	events := []domain.Event{
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
	}
	// Skill events follow RunStatusChanged so the stream reads
	// ...→RunStatusChanged→SkillSelected→SkillLoaded→SkillApplied→LLMCallStarted.
	events = append(events, skillEvents...)

	return ReduceResult{
		NextState: state,
		Events:    events,
		Actions:   []domain.Action{action},
	}, nil
}

// selectSkills runs pure in-memory skill selection for a newly started run. It
// returns the chosen skill refs (for the run snapshot and the dispatch payload)
// plus the lifecycle events to emit: one SkillSelected and one SkillLoaded per
// skill followed by a single SkillApplied summary, or a single SkillSkipped when
// nothing matched. With skills disabled it is a no-op. It performs no I/O: the
// snapshot is an immutable copy taken from the in-memory index.
func (r Reducer) selectSkills(event domain.Event, text string) ([]domain.SkillRef, []domain.Event, error) {
	if !r.cfg.SkillsEnabled || r.cfg.SkillIndex == nil {
		return nil, nil, nil
	}

	selected := skill.Select(r.cfg.SkillIndex.Snapshot(), skill.SelectionInput{Text: text}, r.cfg.SkillSelect)
	if len(selected) == 0 {
		skipped, err := skillEvent(event, domain.EventSkillSkipped, ":skill_skipped", skill.SkillEventPayload{})
		if err != nil {
			return nil, nil, err
		}
		return nil, []domain.Event{skipped}, nil
	}

	refs := make([]domain.SkillRef, 0, len(selected))
	ids := make([]string, 0, len(selected))
	events := make([]domain.Event, 0, len(selected)*2+1)
	totalChars := 0

	// Selection phase: record why each skill was chosen (score + reason).
	for _, sel := range selected {
		evt, err := skillEvent(event, domain.EventSkillSelected, ":skill_selected:"+sel.Ref.ID, skill.SkillEventPayload{
			SkillID:      sel.Ref.ID,
			SkillName:    sel.Ref.Name,
			SkillVersion: sel.Ref.Version,
			Score:        sel.Score,
			Reason:       sel.Reason,
		})
		if err != nil {
			return nil, nil, err
		}
		events = append(events, evt)
	}
	// Load phase: record what content (hash + chars) was made available.
	for _, sel := range selected {
		evt, err := skillEvent(event, domain.EventSkillLoaded, ":skill_loaded:"+sel.Ref.ID, skill.SkillEventPayload{
			SkillID:      sel.Ref.ID,
			SkillName:    sel.Ref.Name,
			SkillVersion: sel.Ref.Version,
			ContentHash:  sel.Ref.ContentHash,
			Chars:        sel.Ref.Chars,
		})
		if err != nil {
			return nil, nil, err
		}
		events = append(events, evt)
		refs = append(refs, sel.Ref)
		ids = append(ids, sel.Ref.ID)
		totalChars += sel.Ref.Chars
	}
	applied, err := skillEvent(event, domain.EventSkillApplied, ":skill_applied", skill.SkillAppliedPayload{
		SkillIDs:   ids,
		Count:      len(ids),
		TotalChars: totalChars,
	})
	if err != nil {
		return nil, nil, err
	}
	events = append(events, applied)

	return refs, events, nil
}

// skillEvent builds a derived skill lifecycle event with a deterministic ID. The
// payload is any of the skill event payload shapes; only metadata is carried.
func skillEvent(event domain.Event, typ domain.EventType, suffix string, payload any) (domain.Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, fmt.Errorf("marshal %s payload: %w", typ, err)
	}
	return domain.Event{
		EventID:   event.RunID + suffix,
		SessionID: event.SessionID,
		RunID:     event.RunID,
		Type:      typ,
		CreatedAt: event.CreatedAt,
		Payload:   encoded,
	}, nil
}

func (r Reducer) reduceLLMResponse(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload LLMResponsePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode llm response payload: %w", err)
	}
	if payload.ToolCall != nil {
		return r.reduceLLMToolCall(state, event, *payload.ToolCall)
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

func (r Reducer) reduceLLMToolCall(state domain.SessionState, event domain.Event, toolCall ToolCallPayload) (ReduceResult, error) {
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
	state.MessageHistory = append(state.MessageHistory, domain.Message{
		Role:       "assistant",
		CreatedAt:  event.CreatedAt,
		ToolCallID: toolCall.ToolCallID,
		ToolName:   toolCall.Name,
		ToolArgs:   toolCall.Args,
	})

	idempotencyKey := event.RunID + ":tool:" + toolCall.ToolCallID
	actionPayload, err := json.Marshal(DispatchToolCallPayload{
		ToolCallID:     toolCall.ToolCallID,
		Name:           toolCall.Name,
		Args:           toolCall.Args,
		IdempotencyKey: idempotencyKey,
		Attempt:        1,
		MaxAttempts:    r.retryPolicy().MaxAttempts,
		Timeout:        r.toolTimeout(),
	})
	if err != nil {
		return ReduceResult{}, fmt.Errorf("marshal tool action payload: %w", err)
	}
	action := domain.Action{
		ActionID:       fmt.Sprintf("%s:tool:%s:1", event.RunID, toolCall.ToolCallID),
		SessionID:      event.SessionID,
		RunID:          event.RunID,
		Type:           domain.ActionDispatchToolCall,
		IdempotencyKey: idempotencyKey,
		Payload:        actionPayload,
	}
	if err := action.Validate(); err != nil {
		return ReduceResult{}, err
	}
	return ReduceResult{NextState: state, Actions: []domain.Action{action}}, nil
}

func (r Reducer) reduceToolCallSucceeded(state domain.SessionState, event domain.Event) (ReduceResult, error) {
	var payload ToolCallEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ReduceResult{}, fmt.Errorf("decode tool result payload: %w", err)
	}
	if payload.ToolCallID == "" {
		return ReduceResult{}, fmt.Errorf("tool_call_id is required")
	}
	if domain.IsTerminalRunStatus(state.RunStatus) {
		return ReduceResult{NextState: state}, nil
	}
	state.RunStatus = domain.RunWaitingLLM
	resultText := string(payload.Result)
	state.MessageHistory = append(state.MessageHistory, domain.Message{
		Role:       "tool",
		Content:    resultText,
		CreatedAt:  event.CreatedAt,
		ToolCallID: payload.ToolCallID,
		ToolName:   payload.Name,
	})

	// Reuse the skills selected at run start; do not re-select or re-emit so the
	// prompt stays stable across tool iterations within a run.
	actionPayload, err := json.Marshal(DispatchLLMCallPayload{
		Input:          resultText,
		Messages:       BuildLLMMessages(state),
		SelectedSkills: state.SelectedSkills,
	})
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
