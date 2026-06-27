package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

func TestSessionLaneAppliesEventsInOrderAndDispatchesActions(t *testing.T) {
	ctx := context.Background()
	store := &recordingStore{}
	dispatcher := &recordingDispatcher{}
	lane := NewSessionLane(SessionLaneConfig{
		SessionID:  "session_1",
		Reducer:    Reducer{},
		Store:      store,
		Dispatcher: dispatcher,
	})
	now := time.Unix(1700000000, 0).UTC()

	first := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"hello"}`),
	}
	second := domain.Event{
		EventID:   "evt_2",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventLLMResponseReceived,
		Seq:       2,
		CreatedAt: now.Add(time.Second),
		Payload:   json.RawMessage(`{"text":"hi","provider_response_id":"resp_1"}`),
	}

	if err := lane.Handle(ctx, first); err != nil {
		t.Fatalf("Handle(first) error = %v", err)
	}
	if err := lane.Handle(ctx, second); err != nil {
		t.Fatalf("Handle(second) error = %v", err)
	}

	if len(store.events) != 2 {
		t.Fatalf("stored events len = %d, want 2", len(store.events))
	}
	if store.events[0].EventID != "evt_1" || store.events[1].EventID != "evt_2" {
		t.Fatalf("stored event order = %q, %q; want evt_1, evt_2", store.events[0].EventID, store.events[1].EventID)
	}
	if store.session.RunStatus != domain.RunCompleted {
		t.Fatalf("stored RunStatus = %q, want %q", store.session.RunStatus, domain.RunCompleted)
	}
	if len(dispatcher.actions) != 2 {
		t.Fatalf("dispatched actions len = %d, want 2", len(dispatcher.actions))
	}
	if dispatcher.actions[0].Type != domain.ActionDispatchLLMCall || dispatcher.actions[1].Type != domain.ActionEmitFinalAnswer {
		t.Fatalf("dispatched action types = %q, %q", dispatcher.actions[0].Type, dispatcher.actions[1].Type)
	}
}

type recordingStore struct {
	events  []domain.Event
	session domain.SessionState
	run     domain.RunState
}

func (s *recordingStore) AppendEvent(ctx context.Context, event domain.Event) error {
	_ = ctx
	s.events = append(s.events, event)
	return nil
}

func (s *recordingStore) LoadSession(ctx context.Context, sessionID string) (domain.SessionState, error) {
	_ = ctx
	if s.session.SessionID == "" {
		return domain.SessionState{SessionID: sessionID, RunStatus: domain.RunIdle}, nil
	}
	return s.session, nil
}

func (s *recordingStore) SaveSession(ctx context.Context, state domain.SessionState) error {
	_ = ctx
	s.session = state
	return nil
}

func (s *recordingStore) LoadRun(ctx context.Context, runID string) (domain.RunState, error) {
	_ = ctx
	if s.run.RunID == "" {
		return domain.RunState{RunID: runID}, nil
	}
	return s.run, nil
}

func (s *recordingStore) SaveRun(ctx context.Context, state domain.RunState) error {
	_ = ctx
	s.run = state
	return nil
}

type recordingDispatcher struct {
	actions []domain.Action
}

func (d *recordingDispatcher) Dispatch(ctx context.Context, action domain.Action) error {
	_ = ctx
	d.actions = append(d.actions, action)
	return nil
}
