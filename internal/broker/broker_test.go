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

func TestBrokerGlobalFeedIsolation(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, unsubSession := b.Subscribe(ctx, "session_1")
	defer unsubSession()
	global, unsubGlobal := b.SubscribeGlobal(ctx)

	b.Publish(protocol.RuntimeEvent{Type: "event", Event: "RunStarted", SessionID: "session_1"})
	b.PublishGlobal(protocol.RuntimeEvent{Type: "event", Event: "SkillIndexReloaded"})

	select {
	case got := <-global:
		if got.Event != "SkillIndexReloaded" {
			t.Fatalf("global event = %#v, want SkillIndexReloaded", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the global event")
	}
	select {
	case got := <-session:
		if got.Event != "RunStarted" {
			t.Fatalf("session event = %#v, want RunStarted", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the session event")
	}
	// No cross-leak in either direction: both channels are drained now.
	select {
	case got := <-global:
		t.Fatalf("global feed leaked session event %#v", got)
	default:
	}
	select {
	case got := <-session:
		t.Fatalf("session subscription leaked global event %#v", got)
	default:
	}

	// Unsubscribe closes the channel; later publishes are dropped, not panics.
	unsubGlobal()
	if _, ok := <-global; ok {
		t.Fatal("global channel not closed after unsubscribe")
	}
	b.PublishGlobal(protocol.RuntimeEvent{Type: "event", Event: "SkillIndexReloaded"})
}

func TestBrokerGlobalUnsubscribesOnContextCancel(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	global, _ := b.SubscribeGlobal(ctx)
	cancel()

	select {
	case _, ok := <-global:
		if ok {
			t.Fatal("expected closed channel after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context-cancel cleanup")
	}
}
