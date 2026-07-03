package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

type RuntimeServiceConfig struct {
	Store        store.Store
	Broker       EventBroker
	LLMProvider  worker.LLMProvider
	ToolRunner   orbisruntime.ToolRunner
	ToolSchemas  []tool.ToolSchema
	SkillBodies  skill.Bodies
	SkillCatalog SkillCatalog
	// Skill learning (v2). A nil ProposalStore disables the learning loop.
	// SkillAutoPropose only creates pending proposals from completed runs; it
	// never promotes anything.
	ProposalStore    *skill.ProposalStore
	AuditLog         *skill.AuditLog
	SkillAutoPropose bool
	ReducerConfig    orbisruntime.ReducerConfig
	RunTimeout       time.Duration
	Now              func() time.Time
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
	eventQueues map[string]chan domain.Event
	dispatcher  *orbisruntime.Dispatcher
	llmProvider worker.LLMProvider
	reducerCfg  orbisruntime.ReducerConfig
	skills      SkillCatalog
	proposals   *skill.ProposalStore
	auditLog    *skill.AuditLog
	autoPropose bool

	runMu      sync.Mutex
	activeRuns map[string]*runExecution
	runTimeout time.Duration

	// Lifecycle: quit signals background goroutines to stop, wg tracks the
	// session-queue and dispatch goroutines, and closing (under lifecycleMu)
	// gates new goroutine spawns so Close can wait without racing wg.Add.
	lifecycleMu sync.Mutex
	closing     bool
	quit        chan struct{}
	wg          sync.WaitGroup
}

// errRuntimeClosed is returned by Enqueue once Close has begun, so late events
// from draining goroutines are dropped instead of panicking on a closed runtime.
var errRuntimeClosed = errors.New("runtime service is closed")

const sessionEventQueueSize = 128

type runExecution struct {
	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
}

type sessionMessageParams struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

type sessionCreateParams struct {
	SessionID string `json:"session_id"`
}

type runStatusParams struct {
	RunID string `json:"run_id"`
}

type eventsListParams struct {
	SessionID string `json:"session_id"`
	AfterSeq  int64  `json:"after_seq"`
	Limit     int    `json:"limit"`
}

const (
	defaultEventsListLimit = 100
	maxEventsListLimit     = 500
)

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
		eventQueues: map[string]chan domain.Event{},
		llmProvider: cfg.LLMProvider,
		reducerCfg:  cfg.ReducerConfig,
		skills:      cfg.SkillCatalog,
		proposals:   cfg.ProposalStore,
		auditLog:    cfg.AuditLog,
		autoPropose: cfg.SkillAutoPropose,
		activeRuns:  map[string]*runExecution{},
		runTimeout:  cfg.RunTimeout,
		quit:        make(chan struct{}),
	}
	service.dispatcher = orbisruntime.NewDispatcher(orbisruntime.DispatcherConfig{
		LLMProvider: cfg.LLMProvider,
		ToolRunner:  cfg.ToolRunner,
		ToolSchemas: cfg.ToolSchemas,
		SkillBodies: cfg.SkillBodies,
		EventSink:   service,
		Now:         now,
	})
	return service
}

// goTrack starts fn in a tracked goroutine unless the service is closing. It
// returns false without starting fn once Close has begun. Checking closing and
// calling wg.Add under lifecycleMu makes the spawn atomic with respect to Close,
// so Close can wait on wg without racing a concurrent Add from zero.
func (s *RuntimeService) goTrack(fn func()) bool {
	s.lifecycleMu.Lock()
	if s.closing {
		s.lifecycleMu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.lifecycleMu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// Close stops accepting new work, cancels in-flight runs, and waits for all
// background session-queue and dispatch goroutines to finish. Background store
// writes happen only inside those goroutines, so after Close no write can land —
// tests defer Close so writes complete before t.TempDir cleanup runs. Close is
// safe to call more than once; the service must not be reused afterward.
func (s *RuntimeService) Close() {
	s.lifecycleMu.Lock()
	if s.closing {
		s.lifecycleMu.Unlock()
		return
	}
	s.closing = true
	close(s.quit)
	s.lifecycleMu.Unlock()

	// Cancel in-flight runs (and stop their timers) so dispatch goroutines blocked
	// on a provider stream unblock promptly instead of waiting out the run timeout.
	s.runMu.Lock()
	for _, exec := range s.activeRuns {
		if exec.timer != nil {
			exec.timer.Stop()
		}
		exec.cancel()
	}
	s.runMu.Unlock()

	s.wg.Wait()
}

func (s *RuntimeService) HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	switch req.Method {
	case "session.create":
		return s.handleSessionCreate(ctx, req)
	case "session.message":
		return s.handleSessionMessage(ctx, req)
	case "run.status":
		return s.handleRunStatus(ctx, req)
	case "run.cancel":
		return s.handleRunCancel(ctx, req)
	case "events.list":
		return s.handleEventsList(ctx, req)
	case "skill.list":
		return s.handleSkillList(ctx, req)
	case "skill.get":
		return s.handleSkillGet(ctx, req)
	case "skill.reload":
		return s.handleSkillReload(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported method %q", req.Method)
	}
}

func (s *RuntimeService) Enqueue(ctx context.Context, event domain.Event) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if event.SessionID == "" {
		return fmt.Errorf("event session_id is required")
	}
	select {
	case <-s.quit:
		return errRuntimeClosed
	default:
	}
	queue := s.eventQueueFor(event.SessionID)
	select {
	case queue <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.quit:
		return errRuntimeClosed
	}
}

func (s *RuntimeService) handleSessionCreate(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params sessionCreateParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode session.create params: %w", err)
		}
	}
	if params.SessionID == "" {
		params.SessionID = "session_" + req.ID
	}
	now := s.now()
	if err := s.store.SaveSession(ctx, domain.SessionState{
		SessionID: params.SessionID,
		RunStatus: domain.RunIdle,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	event := domain.Event{
		EventID:   "evt_" + req.ID,
		SessionID: params.SessionID,
		Type:      domain.EventSessionCreated,
		CreatedAt: now,
		Payload:   json.RawMessage(`{}`),
	}
	if err := s.Enqueue(ctx, event); err != nil {
		return nil, fmt.Errorf("enqueue session created event: %w", err)
	}
	return marshalPayload(protocol.SessionPayload{SessionID: params.SessionID})
}

func (s *RuntimeService) handleSessionMessage(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params sessionMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("decode session.message params: %w", err)
	}
	if params.SessionID == "" {
		params.SessionID = "session_" + req.ID
	}
	if params.Text == "" {
		return nil, fmt.Errorf("session.message text is required")
	}
	runID := "run_" + req.ID
	now := s.now()

	session, err := s.store.LoadSession(ctx, params.SessionID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("load session: %w", err)
		}
		session = domain.SessionState{
			SessionID: params.SessionID,
			RunStatus: domain.RunIdle,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.SessionID = params.SessionID
	session.CurrentRunID = runID
	session.RunStatus = domain.RunIdle
	session.UpdatedAt = now
	if err := s.store.SaveSession(ctx, session); err != nil {
		return nil, fmt.Errorf("save initial session: %w", err)
	}
	if err := s.store.SaveRun(ctx, domain.RunState{
		RunID:     runID,
		SessionID: params.SessionID,
		Status:    domain.RunQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("save initial run: %w", err)
	}
	s.registerRun(runID, params.SessionID)

	payload, err := json.Marshal(orbisruntime.UserMessagePayload{Text: params.Text})
	if err != nil {
		return nil, fmt.Errorf("marshal user message event: %w", err)
	}
	event := domain.Event{
		EventID:   "evt_" + req.ID,
		SessionID: params.SessionID,
		RunID:     runID,
		Type:      domain.EventUserMessageReceived,
		CreatedAt: now,
		Payload:   payload,
	}
	if err := s.Enqueue(ctx, event); err != nil {
		return nil, fmt.Errorf("enqueue user message event: %w", err)
	}

	return marshalPayload(protocol.AckPayload{SessionID: params.SessionID, RunID: runID})
}

func (s *RuntimeService) handleRunStatus(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params runStatusParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("decode run.status params: %w", err)
	}
	if params.RunID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	run, err := s.store.LoadRun(ctx, params.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}
	return marshalPayload(protocol.RunStatusPayload{
		RunID:     run.RunID,
		SessionID: run.SessionID,
		Status:    run.Status,
	})
}

func (s *RuntimeService) handleRunCancel(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params runStatusParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("decode run.cancel params: %w", err)
	}
	if params.RunID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	run, err := s.store.LoadRun(ctx, params.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}
	if !domain.IsTerminalRunStatus(run.Status) {
		s.cancelRun(params.RunID)
		event := domain.Event{
			EventID:   params.RunID + ":cancelled",
			SessionID: run.SessionID,
			RunID:     params.RunID,
			Type:      domain.EventRunCancelled,
			CreatedAt: s.now(),
			Payload:   json.RawMessage(`{}`),
		}
		if err := s.Enqueue(ctx, event); err != nil {
			return nil, fmt.Errorf("enqueue run cancelled event: %w", err)
		}
		run.Status = domain.RunCancelled
	}
	return marshalPayload(protocol.RunStatusPayload{
		RunID:     run.RunID,
		SessionID: run.SessionID,
		Status:    run.Status,
	})
}

func (s *RuntimeService) handleEventsList(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params eventsListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("decode events.list params: %w", err)
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultEventsListLimit
	}
	if limit > maxEventsListLimit {
		limit = maxEventsListLimit
	}
	events, err := s.store.ListEvents(ctx, params.SessionID, store.ListEventsOptions{
		AfterSeq: params.AfterSeq,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	payload := protocol.EventsListPayload{Events: make([]protocol.RuntimeEvent, 0, len(events))}
	for _, event := range events {
		payload.Events = append(payload.Events, runtimeEventFromDomain(event))
	}
	return marshalPayload(payload)
}

func (s *RuntimeService) handleEvent(ctx context.Context, event domain.Event) error {
	event, err := s.prepareEvent(ctx, event)
	if err != nil {
		return err
	}
	s.publish(event)
	lane := s.laneFor(event.SessionID)
	result, err := lane.Handle(ctx, event)
	if err != nil {
		return err
	}
	for _, derived := range result.Events {
		if err := s.Enqueue(ctx, derived); err != nil {
			return err
		}
	}
	for _, action := range result.Actions {
		action := action
		s.goTrack(func() {
			ctx := s.runContext(action.RunID)
			if err := ctx.Err(); err != nil {
				return
			}
			_ = s.dispatcher.Dispatch(ctx, action)
		})
	}
	if event.Type == domain.EventTimerFired {
		s.clearRun(event.RunID)
	}
	// Auto-propose (off by default) creates a pending skill proposal from a
	// completed run. It runs in a tracked goroutine after the lane persisted the
	// terminal state, only ever creates a proposal (never promotes), and drops
	// non-candidate runs silently — the detector is the filter.
	if event.Type == domain.EventRunCompleted && s.autoPropose && s.proposals != nil {
		runID := event.RunID
		s.goTrack(func() {
			_, _ = s.CreateSkillProposalFromRun(context.Background(), runID, skill.ActorSystem, false)
		})
	}
	if isTerminalEvent(event.Type) {
		s.clearRun(event.RunID)
	}
	return nil
}

func (s *RuntimeService) prepareEvent(ctx context.Context, event domain.Event) (domain.Event, error) {
	if event.Seq != 0 {
		return event, nil
	}
	state, err := s.store.LoadSession(ctx, event.SessionID)
	if err != nil {
		return domain.Event{}, fmt.Errorf("load session for event seq: %w", err)
	}
	event.Seq = state.LastEventSeq + 1
	return event, nil
}

func (s *RuntimeService) publish(event domain.Event) {
	if s.broker == nil {
		return
	}
	s.broker.Publish(runtimeEventFromDomain(event))
}

func runtimeEventFromDomain(event domain.Event) protocol.RuntimeEvent {
	return protocol.RuntimeEvent{
		Type:      "event",
		Event:     string(event.Type),
		Seq:       event.Seq,
		SessionID: event.SessionID,
		RunID:     event.RunID,
		Payload:   event.Payload,
	}
}

func marshalPayload(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *RuntimeService) registerRun(runID, sessionID string) {
	ctx, cancel := context.WithCancel(context.Background())
	exec := &runExecution{ctx: ctx, cancel: cancel}
	if s.runTimeout > 0 {
		exec.timer = time.AfterFunc(s.runTimeout, func() {
			payload, err := json.Marshal(map[string]string{"error": "run timeout", "reason": "run_timeout", "kind": "run_timeout"})
			if err != nil {
				payload = json.RawMessage(`{"error":"run timeout"}`)
			}
			_ = s.Enqueue(context.Background(), domain.Event{
				EventID:   runID + ":timer",
				SessionID: sessionID,
				RunID:     runID,
				Type:      domain.EventTimerFired,
				CreatedAt: s.now(),
				Payload:   payload,
			})
		})
	}

	s.runMu.Lock()
	if previous := s.activeRuns[runID]; previous != nil {
		if previous.timer != nil {
			previous.timer.Stop()
		}
		previous.cancel()
	}
	s.activeRuns[runID] = exec
	s.runMu.Unlock()
}

func (s *RuntimeService) runContext(runID string) context.Context {
	s.runMu.Lock()
	exec := s.activeRuns[runID]
	s.runMu.Unlock()
	if exec == nil {
		return context.Background()
	}
	return exec.ctx
}

func (s *RuntimeService) cancelRun(runID string) {
	s.runMu.Lock()
	exec := s.activeRuns[runID]
	s.runMu.Unlock()
	if exec == nil {
		return
	}
	if exec.timer != nil {
		exec.timer.Stop()
	}
	exec.cancel()
}

func (s *RuntimeService) clearRun(runID string) {
	if runID == "" {
		return
	}
	s.runMu.Lock()
	exec := s.activeRuns[runID]
	delete(s.activeRuns, runID)
	s.runMu.Unlock()
	if exec == nil {
		return
	}
	if exec.timer != nil {
		exec.timer.Stop()
	}
	exec.cancel()
}

func isTerminalEvent(eventType domain.EventType) bool {
	return eventType == domain.EventRunCompleted || eventType == domain.EventRunFailed || eventType == domain.EventRunCancelled
}

func (s *RuntimeService) laneFor(sessionID string) *orbisruntime.SessionLane {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lane, ok := s.lanes[sessionID]; ok {
		return lane
	}
	lane := orbisruntime.NewSessionLane(orbisruntime.SessionLaneConfig{
		SessionID: sessionID,
		Reducer:   orbisruntime.NewReducer(s.reducerCfg),
		Store:     s.store,
	})
	s.lanes[sessionID] = lane
	return lane
}

func (s *RuntimeService) eventQueueFor(sessionID string) chan domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if queue, ok := s.eventQueues[sessionID]; ok {
		return queue
	}
	queue := make(chan domain.Event, sessionEventQueueSize)
	s.eventQueues[sessionID] = queue
	s.goTrack(func() { s.runSessionEventQueue(queue) })
	return queue
}

func (s *RuntimeService) runSessionEventQueue(events <-chan domain.Event) {
	for {
		select {
		case event := <-events:
			_ = s.handleEvent(context.Background(), event)
		case <-s.quit:
			return
		}
	}
}
