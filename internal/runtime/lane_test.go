package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
)

func TestSessionLaneAppliesEventsInOrderAndDispatchesActions(t *testing.T) {
	ctx := context.Background()
	store := &recordingStore{}
	lane := NewSessionLane(SessionLaneConfig{
		SessionID: "session_1",
		Reducer:   Reducer{},
		Store:     store,
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

	firstResult, err := lane.Handle(ctx, first)
	if err != nil {
		t.Fatalf("Handle(first) error = %v", err)
	}
	secondResult, err := lane.Handle(ctx, second)
	if err != nil {
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
	actions := append(firstResult.Actions, secondResult.Actions...)
	if len(actions) != 2 {
		t.Fatalf("actions len = %d, want 2", len(actions))
	}
	if actions[0].Type != domain.ActionDispatchLLMCall || actions[1].Type != domain.ActionEmitFinalAnswer {
		t.Fatalf("action types = %q, %q", actions[0].Type, actions[1].Type)
	}
}

func TestSessionLaneSnapshotsSelectedSkillsOntoRun(t *testing.T) {
	ctx := context.Background()
	store := &recordingStore{}
	lane := NewSessionLane(SessionLaneConfig{
		SessionID: "session_1",
		Reducer: NewReducer(ReducerConfig{
			SkillsEnabled: true,
			SkillIndex:    wsSkillIndex(),
			SkillSelect:   skill.SelectConfig{MaxSelected: 3, MaxChars: 12000},
		}),
		Store: store,
	})
	now := time.Unix(1700000000, 0).UTC()
	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"how do I run a websocket runtime test?"}`),
	}

	if _, err := lane.Handle(ctx, event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.run.SelectedSkills) != 1 || store.run.SelectedSkills[0].ID != "ws-test" {
		t.Fatalf("run.SelectedSkills = %#v, want one ws-test snapshot", store.run.SelectedSkills)
	}
	if len(store.session.SelectedSkills) != 1 || store.session.SelectedSkills[0].ID != "ws-test" {
		t.Fatalf("session.SelectedSkills = %#v, want one ws-test", store.session.SelectedSkills)
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

func (s *recordingStore) ListEvents(ctx context.Context, sessionID string, opts store.ListEventsOptions) ([]domain.Event, error) {
	_ = ctx
	_ = sessionID
	_ = opts
	return s.events, nil
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
