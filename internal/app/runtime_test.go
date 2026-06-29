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
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

func mockToolWorker(t *testing.T, fileStore *store.FileStore) *worker.ToolWorker {
	t.Helper()
	registry := tool.NewRegistry()
	if err := tool.RegisterMockTools(registry, func() time.Time {
		return time.Unix(1700000000, 0).UTC()
	}); err != nil {
		t.Fatalf("RegisterMockTools error = %v", err)
	}
	return worker.NewToolWorker(worker.ToolWorkerConfig{
		Registry: registry,
		Policy:   tool.NewPolicy(registry, tool.DefaultPolicyConfig()),
		Store:    fileStore,
		Now:      func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
}

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
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}
	ack := decodeAck(t, payload)
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
	defer service.Close()

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
		string(domain.EventRunStarted),
		string(domain.EventRunStatusChanged),
		string(domain.EventLLMCallStarted),
		string(domain.EventAssistantDelta),
		string(domain.EventLLMResponseReceived),
		string(domain.EventFinalAnswerEmitted),
		string(domain.EventRunCompleted),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events = %#v, want %#v", seen, want)
	}
	assertEventSeqs(t, received, []int64{1, 2, 3, 4, 5, 6, 7, 8})
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
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}
	ack := decodeAck(t, payload)

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunFailed))
	seen := eventNames(received)
	want := []string{
		string(domain.EventUserMessageReceived),
		string(domain.EventRunStarted),
		string(domain.EventRunStatusChanged),
		string(domain.EventLLMCallStarted),
		string(domain.EventLLMCallFailed),
		string(domain.EventRunFailed),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events = %#v, want %#v", seen, want)
	}
	assertEventSeqs(t, received, []int64{1, 2, 3, 4, 5, 6})

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

func TestRuntimeServiceSupportsSessionCreateRunStatusAndEventsList(t *testing.T) {
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
	defer service.Close()

	createPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "create_1",
		Method: "session.create",
		Params: json.RawMessage(`{"session_id":"session_1"}`),
	})
	if err != nil {
		t.Fatalf("session.create error = %v", err)
	}
	var created protocol.SessionPayload
	if err := json.Unmarshal(createPayload, &created); err != nil {
		t.Fatalf("unmarshal session.create payload: %v", err)
	}
	if created.SessionID != "session_1" {
		t.Fatalf("created session_id = %q, want session_1", created.SessionID)
	}

	waitFor(t, func() bool {
		events, err := fileStore.ListEvents(ctx, "session_1", store.ListEventsOptions{})
		return err == nil && len(events) == 1 && events[0].Type == domain.EventSessionCreated
	})

	msgPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("session.message error = %v", err)
	}
	var ack protocol.AckPayload
	if err := json.Unmarshal(msgPayload, &ack); err != nil {
		t.Fatalf("unmarshal message payload: %v", err)
	}

	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunCompleted
	})

	statusPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "status_1",
		Method: "run.status",
		Params: json.RawMessage(`{"run_id":"` + ack.RunID + `"}`),
	})
	if err != nil {
		t.Fatalf("run.status error = %v", err)
	}
	var status protocol.RunStatusPayload
	if err := json.Unmarshal(statusPayload, &status); err != nil {
		t.Fatalf("unmarshal run.status payload: %v", err)
	}
	if status.RunID != ack.RunID || status.SessionID != "session_1" || status.Status != domain.RunCompleted {
		t.Fatalf("status = %#v, want completed run for session_1", status)
	}

	listPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "events_1",
		Method: "events.list",
		Params: json.RawMessage(`{"session_id":"session_1","after_seq":1,"limit":2}`),
	})
	if err != nil {
		t.Fatalf("events.list error = %v", err)
	}
	var listed protocol.EventsListPayload
	if err := json.Unmarshal(listPayload, &listed); err != nil {
		t.Fatalf("unmarshal events.list payload: %v", err)
	}
	if len(listed.Events) != 2 {
		t.Fatalf("listed events len = %d, want 2", len(listed.Events))
	}
	if listed.Events[0].Seq != 2 || listed.Events[0].Event != string(domain.EventUserMessageReceived) {
		t.Fatalf("first listed event = %#v, want seq 2 UserMessageReceived", listed.Events[0])
	}
}

func TestRuntimeServiceRunsMockToolCallThenFinalAnswer(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:  fileStore,
		Broker: eventBroker,
		LLMProvider: &fakeProvider{
			streamBatches: [][]worker.LLMStreamEvent{
				{
					{
						ToolCall: &worker.ToolCall{
							ToolCallID: "call_1",
							Name:       "echo",
							Args:       json.RawMessage(`{"text":"hello"}`),
						},
						Done: true,
					},
				},
				{
					{Delta: "tool complete", ProviderResponseID: "resp_2"},
					{Done: true, ProviderResponseID: "resp_2"},
				},
			},
		},
		ToolRunner: mockToolWorker(t, fileStore),
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_tool",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"use echo"}`),
	})
	if err != nil {
		t.Fatalf("session.message error = %v", err)
	}
	ack := decodeAck(t, payload)

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))
	seen := eventNames(received)
	want := []string{
		string(domain.EventUserMessageReceived),
		string(domain.EventRunStarted),
		string(domain.EventRunStatusChanged),
		string(domain.EventLLMCallStarted),
		string(domain.EventLLMResponseReceived),
		string(domain.EventToolCallStarted),
		string(domain.EventToolCallSucceeded),
		string(domain.EventLLMCallStarted),
		string(domain.EventAssistantDelta),
		string(domain.EventLLMResponseReceived),
		string(domain.EventFinalAnswerEmitted),
		string(domain.EventRunCompleted),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events = %#v, want %#v", seen, want)
	}
	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunCompleted
	})
}

func TestRuntimeServiceRetriesFailedToolThenCompletes(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:  fileStore,
		Broker: eventBroker,
		LLMProvider: &fakeProvider{
			streamBatches: [][]worker.LLMStreamEvent{
				{
					{
						ToolCall: &worker.ToolCall{
							ToolCallID: "call_1",
							Name:       "mock.fail_once",
							Args:       json.RawMessage(`{}`),
						},
						Done: true,
					},
				},
				{
					{Delta: "done", ProviderResponseID: "resp_2"},
					{Done: true, ProviderResponseID: "resp_2"},
				},
			},
		},
		ToolRunner: mockToolWorker(t, fileStore),
		ReducerConfig: orbisruntime.ReducerConfig{
			ToolTimeout: time.Second,
			Retry: tool.RetryPolicy{
				MaxAttempts:       2,
				InitialDelay:      time.Millisecond,
				MaxDelay:          time.Millisecond,
				BackoffMultiplier: 1,
			},
		},
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer service.Close()

	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_retry",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"use fail once"}`),
	}); err != nil {
		t.Fatalf("session.message error = %v", err)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))
	assertOrderedSubsequence(t, eventNames(received),
		string(domain.EventToolCallStarted),
		string(domain.EventToolCallFailed),
		string(domain.EventToolCallRetryScheduled),
		string(domain.EventTimerFired),
		string(domain.EventToolCallRetried),
		string(domain.EventToolCallSucceeded),
		string(domain.EventRunCompleted),
	)
}

func assertOrderedSubsequence(t *testing.T, names []string, want ...string) {
	t.Helper()
	idx := 0
	for _, name := range names {
		if idx < len(want) && name == want[idx] {
			idx++
		}
	}
	if idx != len(want) {
		t.Fatalf("events = %#v, want ordered subsequence %#v (matched %d/%d)", names, want, idx, len(want))
	}
}

func TestRuntimeServicePublishesLLMStartedBeforeProviderCompletes(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	provider := newBlockingStreamProvider()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		Broker:      eventBroker,
		LLMProvider: provider,
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("HandleClientRequest() error = %v", err)
	}
	ack := decodeAck(t, payload)

	received := collectRuntimeEventsUntil(t, events, string(domain.EventLLMCallStarted))
	seen := eventNames(received)
	want := []string{
		string(domain.EventUserMessageReceived),
		string(domain.EventRunStarted),
		string(domain.EventRunStatusChanged),
		string(domain.EventLLMCallStarted),
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("events before provider release = %#v, want %#v", seen, want)
	}

	provider.finish(worker.LLMStreamEvent{Delta: "안녕하세요", ProviderResponseID: "resp_1"})
	received = append(received, collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))...)
	if !containsEvent(received, string(domain.EventAssistantDelta)) {
		t.Fatalf("events = %#v, want AssistantDelta", eventNames(received))
	}

	// The broker publishes events before the session lane persists them, so the
	// observed RunCompleted does not yet guarantee the run record is on disk.
	// Wait for the persisted terminal status (matching the sibling tests) so the
	// run's background writes finish before t.TempDir cleanup runs.
	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunCompleted
	})
}

func TestRuntimeServiceCancelsRun(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	provider := newContextAwareBlockingProvider()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		Broker:      eventBroker,
		LLMProvider: provider,
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_cancel",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("session.message error = %v", err)
	}
	ack := decodeAck(t, payload)
	_ = collectRuntimeEventsUntil(t, events, string(domain.EventLLMCallStarted))

	cancelPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "cancel_1",
		Method: "run.cancel",
		Params: json.RawMessage(`{"run_id":"` + ack.RunID + `"}`),
	})
	if err != nil {
		t.Fatalf("run.cancel error = %v", err)
	}
	var cancelled protocol.RunStatusPayload
	if err := json.Unmarshal(cancelPayload, &cancelled); err != nil {
		t.Fatalf("unmarshal run.cancel payload: %v", err)
	}
	if cancelled.Status != domain.RunCancelled {
		t.Fatalf("cancel response status = %s, want %s", cancelled.Status, domain.RunCancelled)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunCancelled))
	if !containsEvent(received, string(domain.EventRunCancelled)) {
		t.Fatalf("events = %#v, want RunCancelled", eventNames(received))
	}
	select {
	case <-provider.cancelled:
	case <-time.After(time.Second):
		t.Fatal("provider context was not cancelled")
	}
	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunCancelled
	})
}

func TestRuntimeServiceTimesOutRunWithTimerFired(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		Broker:      eventBroker,
		LLMProvider: newContextAwareBlockingProvider(),
		RunTimeout:  20 * time.Millisecond,
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	})
	defer service.Close()

	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_timeout",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"안녕"}`),
	})
	if err != nil {
		t.Fatalf("session.message error = %v", err)
	}
	ack := decodeAck(t, payload)

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunFailed))
	if !containsEvent(received, string(domain.EventTimerFired)) {
		t.Fatalf("events = %#v, want TimerFired before RunFailed", eventNames(received))
	}
	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunFailed
	})
}

func TestRuntimeServiceSkillListGetReload(t *testing.T) {
	ctx := context.Background()
	catalog := &fakeSkillCatalog{
		metas: []skill.Metadata{{ID: "ws-test", Name: "ws", Title: "WebSocket Runtime Test", Version: "1"}},
		entries: map[string]skill.Entry{
			"ws-test": {
				Metadata:    skill.Metadata{ID: "ws-test", Name: "ws", Version: "1"},
				Body:        "WS BODY",
				ContentHash: "hash-ws",
				Chars:       7,
			},
		},
	}
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:        store.NewFileStore(t.TempDir()),
		SkillCatalog: catalog,
		Now:          func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer service.Close()

	listPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "list_1", Method: "skill.list"})
	if err != nil {
		t.Fatalf("skill.list error = %v", err)
	}
	var list protocol.SkillListPayload
	if err := json.Unmarshal(listPayload, &list); err != nil {
		t.Fatalf("unmarshal skill.list: %v", err)
	}
	if len(list.Skills) != 1 || list.Skills[0].ID != "ws-test" {
		t.Fatalf("skills = %#v, want one ws-test", list.Skills)
	}

	getPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "get_1", Method: "skill.get", Params: json.RawMessage(`{"skill_id":"ws-test"}`)})
	if err != nil {
		t.Fatalf("skill.get error = %v", err)
	}
	var detail protocol.SkillDetailPayload
	if err := json.Unmarshal(getPayload, &detail); err != nil {
		t.Fatalf("unmarshal skill.get: %v", err)
	}
	if detail.ID != "ws-test" || detail.Body != "WS BODY" {
		t.Fatalf("detail = %#v, want ws-test with body", detail)
	}

	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "get_2", Method: "skill.get", Params: json.RawMessage(`{"skill_id":"missing"}`)}); err == nil {
		t.Fatal("skill.get unknown error = nil, want not-found error")
	}

	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "reload_1", Method: "skill.reload"}); err != nil {
		t.Fatalf("skill.reload error = %v", err)
	}
	if catalog.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", catalog.reloadCount)
	}
}

func TestRuntimeServicePublishesSkillEventsWhenEnabled(t *testing.T) {
	ctx := context.Background()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(ctx, "session_1")
	defer unsubscribe()
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:  fileStore,
		Broker: eventBroker,
		LLMProvider: &fakeProvider{
			response: worker.LLMResponse{Text: "ok", ProviderResponseID: "resp_1"},
		},
		ReducerConfig: orbisruntime.ReducerConfig{
			SkillsEnabled: true,
			SkillIndex: fakeSkillIndex{entries: []skill.Entry{{
				Metadata: skill.Metadata{ID: "ws-test", Name: "ws", Triggers: []string{"websocket"}, Status: "active", Priority: 100},
				Body:     "WS BODY", ContentHash: "hash-ws", Chars: 7,
			}}},
			SkillSelect: skill.SelectConfig{MaxSelected: 3, MaxChars: 12000},
		},
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer service.Close()

	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"how do I run a websocket test?"}`),
	}); err != nil {
		t.Fatalf("session.message error = %v", err)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))
	if !containsEvent(received, string(domain.EventSkillApplied)) {
		t.Fatalf("events = %#v, want SkillApplied", eventNames(received))
	}
}

type fakeSkillCatalog struct {
	metas       []skill.Metadata
	entries     map[string]skill.Entry
	reloadCount int
}

func (c *fakeSkillCatalog) List() []skill.Metadata { return c.metas }

func (c *fakeSkillCatalog) Get(id string) (skill.Entry, bool) {
	entry, ok := c.entries[id]
	return entry, ok
}

func (c *fakeSkillCatalog) Reload() error {
	c.reloadCount++
	return nil
}

type fakeSkillIndex struct {
	entries []skill.Entry
}

func (f fakeSkillIndex) Snapshot() []skill.Entry { return f.entries }

type fakeProvider struct {
	response      worker.LLMResponse
	streamBatches [][]worker.LLMStreamEvent
	err           error
	streamCalls   int
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
	_ = req
	if p.err != nil {
		return nil, p.err
	}
	var events []worker.LLMStreamEvent
	if p.streamCalls < len(p.streamBatches) {
		events = p.streamBatches[p.streamCalls]
	}
	p.streamCalls++
	if len(events) == 0 {
		events = []worker.LLMStreamEvent{
			{Delta: p.response.Text, ProviderResponseID: p.response.ProviderResponseID},
			{Done: true, ProviderResponseID: p.response.ProviderResponseID},
		}
	}
	ch := make(chan worker.LLMStreamEvent, len(events))
	for _, event := range events {
		select {
		case ch <- event:
		case <-ctx.Done():
			close(ch)
			return ch, nil
		}
	}
	close(ch)
	return ch, nil
}

type blockingStreamProvider struct {
	ch chan worker.LLMStreamEvent
}

func newBlockingStreamProvider() *blockingStreamProvider {
	return &blockingStreamProvider{ch: make(chan worker.LLMStreamEvent)}
}

func (p *blockingStreamProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	_ = req
	panic("not used")
}

func (p *blockingStreamProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	_ = ctx
	_ = req
	return p.ch, nil
}

func (p *blockingStreamProvider) finish(event worker.LLMStreamEvent) {
	p.ch <- event
	p.ch <- worker.LLMStreamEvent{Done: true, ProviderResponseID: event.ProviderResponseID}
	close(p.ch)
}

type contextAwareBlockingProvider struct {
	cancelled chan struct{}
}

func newContextAwareBlockingProvider() *contextAwareBlockingProvider {
	return &contextAwareBlockingProvider{cancelled: make(chan struct{})}
}

func (p *contextAwareBlockingProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	_ = req
	panic("not used")
}

func (p *contextAwareBlockingProvider) Stream(ctx context.Context, req worker.LLMRequest) (<-chan worker.LLMStreamEvent, error) {
	_ = req
	ch := make(chan worker.LLMStreamEvent)
	go func() {
		<-ctx.Done()
		close(p.cancelled)
		close(ch)
	}()
	return ch, nil
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

func decodeAck(t *testing.T, payload json.RawMessage) protocol.AckPayload {
	t.Helper()
	var ack protocol.AckPayload
	if err := json.Unmarshal(payload, &ack); err != nil {
		t.Fatalf("unmarshal ack payload: %v", err)
	}
	return ack
}

func containsEvent(events []protocol.RuntimeEvent, want string) bool {
	for _, event := range events {
		if event.Event == want {
			return true
		}
	}
	return false
}
