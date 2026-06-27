package queue

import (
	"context"

	"github.com/sangjinsu/orbis/internal/domain"
)

type MemoryQueue struct {
	events chan domain.Event
}

func NewMemoryQueue(capacity int) *MemoryQueue {
	if capacity <= 0 {
		capacity = 1
	}
	return &MemoryQueue{events: make(chan domain.Event, capacity)}
}

func (q *MemoryQueue) Enqueue(ctx context.Context, event domain.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case q.events <- event:
		return nil
	}
}

func (q *MemoryQueue) Dequeue(ctx context.Context) (domain.Event, error) {
	select {
	case <-ctx.Done():
		return domain.Event{}, ctx.Err()
	case event := <-q.events:
		return event, nil
	}
}
