package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/broker"
	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/worker"
)

func TestRuntimeServiceHandlesSessionMessageAsBackgroundEvent(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	service := NewRuntimeService(RuntimeServiceConfig{
		Store: fileStore,
		LLMProvider: &fakeProvider{
			response: worker.LLMResponse{Text: "안녕하세요", ProviderResponseID: "resp_1"},
		},
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})

	ack, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}
	if ack.SessionID != "session_1" || ack.RunID == "" {
		t.Fatalf("ack = %#v, want session_1 and non-empty run id", ack)
	}

	waitFor(t, func() bool {
		state, err := fileStore.LoadSession(ctx, "session_1")
		return err == nil && state.RunStatus == domain.RunCompleted
	})

	state, err := fileStore.LoadSession(ctx, "session_1")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(state.MessageHistory) != 2 {
		t.Fatalf("MessageHistory len = %d, want 2", len(state.MessageHistory))
	}
	run, err := fileStore.LoadRun(ctx, ack.RunID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if run.Status != domain.RunCompleted {
		t.Fatalf("run Status = %s, want %s", run.Status, domain.RunCompleted)
	}
	eventPath := filepath.Join(fileStore.Root(), "events", "session_1.jsonl")
	if _, err := os.Stat(eventPath); err != nil {
		t.Fatalf("event log was not written: %v", err)
	}
}

func TestRuntimeServicePublishesProgressEvents(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:  fileStore,
		Broker: eventBroker,
		LLMProvider: &fakeProvider{
			response: worker.LLMResponse{Text: "안녕하세요", ProviderResponseID: "resp_1"},
		},
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})

	_, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))
	seen := eventNames(received)
	want := []string{
		string(domain.EventUserMessageReceived),
		string(domain.EventLLMCallStarted),
		string(domain.EventLLMResponseReceived),
		string(domain.EventFinalAnswerEmitted),
		string(domain.EventRunCompleted),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events = %#v, want %#v", seen, want)
	}
	assertEventSeqs(t, received, []int64{1, 2, 3, 4, 5})
}

func TestRuntimeServicePublishesTerminalFailureAfterProviderError(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		Broker:      eventBroker,
		LLMProvider: &fakeProvider{err: errors.New("provider unavailable")},
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})

	ack, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunFailed))
	seen := eventNames(received)
	want := []string{
		string(domain.EventUserMessageReceived),
		string(domain.EventLLMCallStarted),
		string(domain.EventLLMCallFailed),
		string(domain.EventRunFailed),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events = %#v, want %#v", seen, want)
	}
	assertEventSeqs(t, received, []int64{1, 2, 3, 4})

	state, err := fileStore.LoadSession(ctx, "session_1")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if state.RunStatus != domain.RunFailed {
		t.Fatalf("RunStatus = %s, want %s", state.RunStatus, domain.RunFailed)
	}
	run, err := fileStore.LoadRun(ctx, ack.RunID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if run.Status != domain.RunFailed {
		t.Fatalf("run Status = %s, want %s", run.Status, domain.RunFailed)
	}
}

type fakeProvider struct {
	response worker.LLMResponse
	err      error
}

func (p *fakeProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	_ = req
	if p.err != nil {
		return worker.LLMResponse{}, p.err
	}
	return p.response, nil
}

func (p *fakeProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	_ = ctx
	_ = req
	panic("not used")
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before deadline")
}

func collectRuntimeEventsUntil(t *testing.T, events <-chan protocol.RuntimeEvent, terminal string) []protocol.RuntimeEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	seen := []protocol.RuntimeEvent{}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("event channel closed before %s; seen=%#v", terminal, seen)
			}
			seen = append(seen, event)
			if event.Event == terminal {
				return seen
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; seen=%#v", terminal, seen)
		}
	}
}

func eventNames(events []protocol.RuntimeEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.Event)
	}
	return names
}

func assertEventSeqs(t *testing.T, events []protocol.RuntimeEvent, want []int64) {
	t.Helper()
	got := make([]int64, 0, len(events))
	for _, event := range events {
		got = append(got, event.Seq)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event seqs = %#v, want %#v", got, want)
	}
}
