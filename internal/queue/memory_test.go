package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

func TestMemoryQueuePreservesFIFOOrder(t *testing.T) {
	q := NewMemoryQueue(2)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	first := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"first"}`),
	}
	second := first
	second.EventID = "evt_2"
	second.Seq = 2
	second.Payload = json.RawMessage(`{"text":"second"}`)

	if err := q.Enqueue(ctx, first); err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	if err := q.Enqueue(ctx, second); err != nil {
		t.Fatalf("Enqueue(second) error = %v", err)
	}

	gotFirst, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue(first) error = %v", err)
	}
	gotSecond, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue(second) error = %v", err)
	}
	if gotFirst.EventID != "evt_1" || gotSecond.EventID != "evt_2" {
		t.Fatalf("order = %q, %q; want evt_1, evt_2", gotFirst.EventID, gotSecond.EventID)
	}
}
