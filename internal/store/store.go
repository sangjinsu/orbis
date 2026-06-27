package store

import (
	"context"

	"github.com/sangjinsu/orbis/internal/domain"
)

type Store interface {
	AppendEvent(ctx context.Context, event domain.Event) error
	LoadSession(ctx context.Context, sessionID string) (domain.SessionState, error)
	SaveSession(ctx context.Context, state domain.SessionState) error
	LoadRun(ctx context.Context, runID string) (domain.RunState, error)
	SaveRun(ctx context.Context, state domain.RunState) error
}
