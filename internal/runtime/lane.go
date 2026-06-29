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
	SessionID string
	Reducer   ReducerInterface
	Store     store.Store
}

type SessionLane struct {
	sessionID string
	reducer   ReducerInterface
	store     store.Store
	mu        sync.Mutex
}

func NewSessionLane(cfg SessionLaneConfig) *SessionLane {
	return &SessionLane{
		sessionID: cfg.SessionID,
		reducer:   cfg.Reducer,
		store:     cfg.Store,
	}
}

func (l *SessionLane) Handle(ctx context.Context, event domain.Event) (ReduceResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.store == nil {
		return ReduceResult{}, fmt.Errorf("store is required")
	}
	if l.reducer == nil {
		return ReduceResult{}, fmt.Errorf("reducer is required")
	}
	if event.SessionID != l.sessionID {
		return ReduceResult{}, fmt.Errorf("event session %q does not match lane session %q", event.SessionID, l.sessionID)
	}

	state, err := l.store.LoadSession(ctx, event.SessionID)
	if err != nil {
		return ReduceResult{}, fmt.Errorf("load session: %w", err)
	}
	if err := l.store.AppendEvent(ctx, event); err != nil {
		return ReduceResult{}, fmt.Errorf("append event: %w", err)
	}

	result, err := l.reducer.Apply(ctx, state, event)
	if err != nil {
		return ReduceResult{}, fmt.Errorf("reduce event: %w", err)
	}
	if err := l.store.SaveSession(ctx, result.NextState); err != nil {
		return ReduceResult{}, fmt.Errorf("save session: %w", err)
	}
	if err := l.saveRunState(ctx, result.NextState, event); err != nil {
		return ReduceResult{}, err
	}

	return result, nil
}

func (l *SessionLane) saveRunState(ctx context.Context, state domain.SessionState, event domain.Event) error {
	if event.RunID == "" {
		return nil
	}
	run, err := l.store.LoadRun(ctx, event.RunID)
	if err != nil {
		return fmt.Errorf("load run: %w", err)
	}
	if run.RunID == "" {
		run.RunID = event.RunID
	}
	if run.SessionID == "" {
		run.SessionID = event.SessionID
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = state.CreatedAt
		if run.CreatedAt.IsZero() {
			run.CreatedAt = event.CreatedAt
		}
	}
	run.Status = state.RunStatus
	run.UpdatedAt = event.CreatedAt
	// Snapshot the run's selected skills once: record them the first time the run
	// has any, then leave them so the run history reflects what was applied even
	// after a later index reload.
	if len(run.SelectedSkills) == 0 && len(state.SelectedSkills) > 0 {
		run.SelectedSkills = state.SelectedSkills
	}
	if err := l.store.SaveRun(ctx, run); err != nil {
		return fmt.Errorf("save run: %w", err)
	}
	return nil
}
