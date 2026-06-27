package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/store"
)

type ReducerInterface interface {
	Apply(ctx context.Context, state domain.SessionState, event domain.Event) (ReduceResult, error)
}

type ActionDispatcher interface {
	Dispatch(ctx context.Context, action domain.Action) error
}

type SessionLaneConfig struct {
	SessionID  string
	Reducer    ReducerInterface
	Store      store.Store
	Dispatcher ActionDispatcher
}

type SessionLane struct {
	sessionID  string
	reducer    ReducerInterface
	store      store.Store
	dispatcher ActionDispatcher
	mu         sync.Mutex
}

func NewSessionLane(cfg SessionLaneConfig) *SessionLane {
	return &SessionLane{
		sessionID:  cfg.SessionID,
		reducer:    cfg.Reducer,
		store:      cfg.Store,
		dispatcher: cfg.Dispatcher,
	}
}

func (l *SessionLane) Handle(ctx context.Context, event domain.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.store == nil {
		return fmt.Errorf("store is required")
	}
	if l.reducer == nil {
		return fmt.Errorf("reducer is required")
	}
	if event.SessionID != l.sessionID {
		return fmt.Errorf("event session %q does not match lane session %q", event.SessionID, l.sessionID)
	}

	state, err := l.store.LoadSession(ctx, event.SessionID)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if err := l.store.AppendEvent(ctx, event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	result, err := l.reducer.Apply(ctx, state, event)
	if err != nil {
		return fmt.Errorf("reduce event: %w", err)
	}
	if err := l.store.SaveSession(ctx, result.NextState); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	for _, action := range result.Actions {
		if l.dispatcher == nil {
			return fmt.Errorf("dispatcher is required")
		}
		if err := l.dispatcher.Dispatch(ctx, action); err != nil {
			return fmt.Errorf("dispatch action %s: %w", action.ActionID, err)
		}
	}
	return nil
}
