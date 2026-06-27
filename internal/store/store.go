package store

import (
	"context"
	"errors"

	"github.com/sangjinsu/orbis/internal/domain"
)

var ErrNotFound = errors.New("not found")

type ListEventsOptions struct {
	AfterSeq int64
	Limit    int
}

type Store interface {
	AppendEvent(ctx context.Context, event domain.Event) error
	ListEvents(ctx context.Context, sessionID string, opts ListEventsOptions) ([]domain.Event, error)
	LoadSession(ctx context.Context, sessionID string) (domain.SessionState, error)
	SaveSession(ctx context.Context, state domain.SessionState) error
	LoadRun(ctx context.Context, runID string) (domain.RunState, error)
	SaveRun(ctx context.Context, state domain.RunState) error
}
