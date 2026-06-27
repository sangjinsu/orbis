package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/worker"
)

type RuntimeServiceConfig struct {
	Store       store.Store
	Broker      EventBroker
	LLMProvider worker.LLMProvider
	Now         func() time.Time
}

type EventBroker interface {
	Publish(event protocol.RuntimeEvent)
}

type RuntimeService struct {
	store  store.Store
	broker EventBroker
	now    func() time.Time

	mu          sync.Mutex
	lanes       map[string]*orbisruntime.SessionLane
	dispatcher  *orbisruntime.Dispatcher
	llmProvider worker.LLMProvider
}

type sessionMessageParams struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

func NewRuntimeService(cfg RuntimeServiceConfig) *RuntimeService {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	service := &RuntimeService{
		store:       cfg.Store,
		broker:      cfg.Broker,
		now:         now,
		lanes:       map[string]*orbisruntime.SessionLane{},
		llmProvider: cfg.LLMProvider,
	}
	service.dispatcher = orbisruntime.NewDispatcher(orbisruntime.DispatcherConfig{
		LLMProvider: cfg.LLMProvider,
		EventSink:   service,
		Now:         now,
	})
	return service
}

func (s *RuntimeService) HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (protocol.AckPayload, error) {
	switch req.Method {
	case "session.message":
		return s.handleSessionMessage(ctx, req)
	default:
		return protocol.AckPayload{}, fmt.Errorf("unsupported method %q", req.Method)
	}
}

func (s *RuntimeService) Enqueue(ctx context.Context, event domain.Event) error {
	go func() {
		_ = s.handleEvent(ctx, event)
	}()
	return nil
}

func (s *RuntimeService) handleSessionMessage(ctx context.Context, req protocol.ClientRequest) (protocol.AckPayload, error) {
	var params sessionMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return protocol.AckPayload{}, fmt.Errorf("decode session.message params: %w", err)
	}
	if params.SessionID == "" {
		params.SessionID = "session_" + req.ID
	}
	if params.Text == "" {
		return protocol.AckPayload{}, fmt.Errorf("session.message text is required")
	}
	runID := "run_" + req.ID
	now := s.now()

	if err := s.store.SaveSession(ctx, domain.SessionState{
		SessionID:    params.SessionID,
		CurrentRunID: runID,
		RunStatus:    domain.RunIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return protocol.AckPayload{}, fmt.Errorf("save initial session: %w", err)
	}
	if err := s.store.SaveRun(ctx, domain.RunState{
		RunID:     runID,
		SessionID: params.SessionID,
		Status:    domain.RunQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return protocol.AckPayload{}, fmt.Errorf("save initial run: %w", err)
	}

	payload, err := json.Marshal(orbisruntime.UserMessagePayload{Text: params.Text})
	if err != nil {
		return protocol.AckPayload{}, fmt.Errorf("marshal user message event: %w", err)
	}
	event := domain.Event{
		EventID:   "evt_" + req.ID,
		SessionID: params.SessionID,
		RunID:     runID,
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   payload,
	}
	go func() {
		_ = s.handleEvent(context.Background(), event)
	}()

	return protocol.AckPayload{SessionID: params.SessionID, RunID: runID}, nil
}

func (s *RuntimeService) handleEvent(ctx context.Context, event domain.Event) error {
	s.publish(event)
	lane := s.laneFor(event.SessionID)
	return lane.Handle(ctx, event)
}

func (s *RuntimeService) publish(event domain.Event) {
	if s.broker == nil {
		return
	}
	s.broker.Publish(protocol.RuntimeEvent{
		Type:      "event",
		Event:     string(event.Type),
		Seq:       event.Seq,
		SessionID: event.SessionID,
		RunID:     event.RunID,
		Payload:   event.Payload,
	})
}

func (s *RuntimeService) laneFor(sessionID string) *orbisruntime.SessionLane {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lane, ok := s.lanes[sessionID]; ok {
		return lane
	}
	lane := orbisruntime.NewSessionLane(orbisruntime.SessionLaneConfig{
		SessionID:  sessionID,
		Reducer:    orbisruntime.Reducer{},
		Store:      s.store,
		Dispatcher: s.dispatcher,
	})
	s.lanes[sessionID] = lane
	return lane
}
