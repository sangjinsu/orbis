package broker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/protocol"
)

func TestBrokerPublishesToSessionSubscribers(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsubscribe := b.Subscribe(ctx, "session_1")
	defer unsubscribe()

	want := protocol.RuntimeEvent{
		Type:      "event",
		Event:     "RunStarted",
		Seq:       1,
		SessionID: "session_1",
		RunID:     "run_1",
		Payload:   json.RawMessage(`{}`),
	}
	b.Publish(want)

	select {
	case got := <-events:
		if got.Event != want.Event || got.SessionID != want.SessionID {
			t.Fatalf("event = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broker event")
	}
}
