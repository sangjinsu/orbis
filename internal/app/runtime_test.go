package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
		AdminToken:   "admin-tok",
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

	// skill.reload is mutating as of v2 and requires the admin token.
	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "reload_0", Method: "skill.reload"}); err == nil {
		t.Fatal("skill.reload without token error = nil, want invalid-token error")
	}
	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{Type: "req", ID: "reload_1", Method: "skill.reload", Params: json.RawMessage(`{"token":"admin-tok"}`)}); err != nil {
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

// toolRunProvider returns a fake provider that proposes one echo tool call and
// then finishes, so a run completes having used a tool.
func toolRunProvider() *fakeProvider {
	return &fakeProvider{
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
	}
}

// newLearningService builds a runtime service with the v2 skill-learning loop
// wired (proposal store + audit log) on top of the tool-run harness.
func newLearningService(t *testing.T, autoPropose bool) (*RuntimeService, *skill.ProposalStore, *store.FileStore, string, <-chan protocol.RuntimeEvent, func()) {
	t.Helper()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(context.Background(), "session_1")

	proposals, err := skill.NewProposalStore(filepath.Join(fileStore.Root(), "skill_proposals"))
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	auditPath := filepath.Join(fileStore.Root(), "audit", "skill_audit.jsonl")
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:            fileStore,
		Broker:           eventBroker,
		LLMProvider:      toolRunProvider(),
		ToolRunner:       mockToolWorker(t, fileStore),
		ProposalStore:    proposals,
		AuditLog:         skill.NewAuditLog(auditPath),
		SkillAutoPropose: autoPropose,
		Now:              func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	return service, proposals, fileStore, auditPath, events, unsubscribe
}

// completeToolRun drives one session.message through the service until the run
// is persisted as COMPLETED and returns its run id.
func completeToolRun(t *testing.T, service *RuntimeService, fileStore *store.FileStore, events <-chan protocol.RuntimeEvent) string {
	t.Helper()
	ctx := context.Background()
	payload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type:   "req",
		ID:     "req_learn",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"use echo to say hello"}`),
	})
	if err != nil {
		t.Fatalf("session.message error = %v", err)
	}
	ack := decodeAck(t, payload)
	_ = collectRuntimeEventsUntil(t, events, string(domain.EventRunCompleted))
	waitFor(t, func() bool {
		run, err := fileStore.LoadRun(ctx, ack.RunID)
		return err == nil && run.Status == domain.RunCompleted
	})
	return ack.RunID
}

func TestRuntimeServiceCreatesSkillProposalFromRun(t *testing.T) {
	service, proposals, fileStore, auditPath, events, unsubscribe := newLearningService(t, false)
	defer unsubscribe()
	defer service.Close()
	runID := completeToolRun(t, service, fileStore, events)

	proposal, err := service.CreateSkillProposalFromRun(context.Background(), runID, "developer", true)
	if err != nil {
		t.Fatalf("CreateSkillProposalFromRun() error = %v", err)
	}
	if proposal.Status != skill.ProposalPending || proposal.SourceRunID != runID {
		t.Fatalf("proposal = %#v, want pending proposal for %s", proposal, runID)
	}
	if len(proposal.RelatedTools) != 1 || proposal.RelatedTools[0] != "echo" {
		t.Fatalf("RelatedTools = %v, want [echo]", proposal.RelatedTools)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillReviewRequired))
	assertOrderedSubsequence(t, eventNames(received),
		string(domain.EventSkillCandidateDetected),
		string(domain.EventSkillProposalCreated),
		string(domain.EventSkillReviewRequired),
	)

	pending, err := proposals.List(skill.ProposalPending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("List(pending) = %v, %v; want one pending proposal", pending, err)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil || !strings.Contains(string(audit), string(domain.EventSkillProposalCreated)) {
		t.Fatalf("audit log = %q, %v; want a SkillProposalCreated record", audit, err)
	}

	// The proposal id is deterministic per run, so a second manual request for
	// the same run is rejected as a duplicate instead of creating another one.
	if _, err := service.CreateSkillProposalFromRun(context.Background(), runID, "developer", true); err == nil {
		t.Fatal("second CreateSkillProposalFromRun() error = nil, want duplicate error")
	}
}

func TestRuntimeServiceAutoProposeCreatesPendingProposalOnly(t *testing.T) {
	service, proposals, fileStore, _, events, unsubscribe := newLearningService(t, true)
	defer unsubscribe()
	defer service.Close()
	completeToolRun(t, service, fileStore, events)

	// The auto hook runs in a tracked goroutine after RunCompleted.
	waitFor(t, func() bool {
		pending, err := proposals.List(skill.ProposalPending)
		return err == nil && len(pending) == 1
	})
	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillReviewRequired))
	if !containsEvent(received, string(domain.EventSkillProposalCreated)) {
		t.Fatalf("events = %#v, want SkillProposalCreated", eventNames(received))
	}
	pending, err := proposals.List(skill.ProposalPending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("List(pending) = %v, %v; want exactly one", pending, err)
	}
	// Auto-propose never promotes: the proposal stays pending.
	if pending[0].Status != skill.ProposalPending {
		t.Fatalf("auto proposal status = %q, want pending", pending[0].Status)
	}
}

func TestRuntimeServiceAutoProposeDefaultOffCreatesNothing(t *testing.T) {
	service, proposals, fileStore, _, events, unsubscribe := newLearningService(t, false)
	defer unsubscribe()
	defer service.Close()
	completeToolRun(t, service, fileStore, events)

	all, err := proposals.List("")
	if err != nil || len(all) != 0 {
		t.Fatalf("List() = %v, %v; want no proposals with auto-propose off", all, err)
	}
}

func TestCreateSkillProposalRequiresLearningEnabled(t *testing.T) {
	fileStore := store.NewFileStore(t.TempDir())
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		LLMProvider: &fakeProvider{response: worker.LLMResponse{Text: "ok"}},
		Now:         func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer service.Close()

	if _, err := service.CreateSkillProposalFromRun(context.Background(), "run_x", "developer", true); !errors.Is(err, errSkillLearningDisabled) {
		t.Fatalf("error = %v, want errSkillLearningDisabled", err)
	}
}

// newReviewService wires the full v2 review loop: the learning stores plus a
// real skill store (empty index) and promoter over a temp skills directory,
// guarded by an admin token.
func newReviewService(t *testing.T) (*RuntimeService, *skill.ProposalStore, *skill.Store, *store.FileStore, string, string, <-chan protocol.RuntimeEvent, func()) {
	t.Helper()
	fileStore := store.NewFileStore(t.TempDir())
	eventBroker := broker.New()
	events, unsubscribe := eventBroker.Subscribe(context.Background(), "session_1")

	skillsDir := filepath.Join(fileStore.Root(), "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("create skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "index.json"), []byte(`{"skills":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write empty index: %v", err)
	}
	skillStore, err := skill.NewStore(skillsDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	proposals, err := skill.NewProposalStore(filepath.Join(fileStore.Root(), "skill_proposals"))
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	auditPath := filepath.Join(fileStore.Root(), "audit", "skill_audit.jsonl")
	service := NewRuntimeService(RuntimeServiceConfig{
		Store:         fileStore,
		Broker:        eventBroker,
		LLMProvider:   toolRunProvider(),
		ToolRunner:    mockToolWorker(t, fileStore),
		SkillCatalog:  skillStore,
		ProposalStore: proposals,
		AuditLog:      skill.NewAuditLog(auditPath),
		Promoter:      skill.NewPromoter(skillsDir),
		AdminToken:    "admin-tok",
		Now:           func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	return service, proposals, skillStore, fileStore, skillsDir, auditPath, events, unsubscribe
}

func TestRuntimeServiceApproveFlowPromotesAndReloads(t *testing.T) {
	ctx := context.Background()
	service, _, skillStore, fileStore, skillsDir, auditPath, events, unsubscribe := newReviewService(t)
	defer unsubscribe()
	defer service.Close()
	runID := completeToolRun(t, service, fileStore, events)

	// Create through the WS method (covers the admin-token path).
	createPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_create", Method: "skill.proposal.create_from_run",
		Params: json.RawMessage(`{"run_id":"` + runID + `","token":"admin-tok"}`),
	})
	if err != nil {
		t.Fatalf("skill.proposal.create_from_run error = %v", err)
	}
	var created protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(createPayload, &created); err != nil {
		t.Fatalf("unmarshal created proposal: %v", err)
	}
	_ = collectRuntimeEventsUntil(t, events, string(domain.EventSkillReviewRequired))

	// Approve: the proposal is promoted, the index reloads, and the lifecycle is
	// observable in order.
	approvePayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_approve", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"` + created.ProposalID + `","token":"admin-tok"}`),
	})
	if err != nil {
		t.Fatalf("skill.proposal.approve error = %v", err)
	}
	var promoted protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(approvePayload, &promoted); err != nil {
		t.Fatalf("unmarshal promoted proposal: %v", err)
	}
	if promoted.Status != string(skill.ProposalPromoted) || promoted.PromotedSkillID == "" {
		t.Fatalf("approve payload = %#v, want promoted with skill id", promoted.SkillProposalSummary)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillAuditRecorded))
	assertOrderedSubsequence(t, eventNames(received),
		string(domain.EventSkillProposalApproved),
		string(domain.EventSkillPromoted),
		string(domain.EventSkillIndexReloadRequested),
		string(domain.EventSkillIndexReloaded),
		string(domain.EventSkillAuditRecorded),
	)

	// Promoted skill exists on disk and in the reloaded in-memory index.
	if _, err := os.Stat(filepath.Join(skillsDir, promoted.PromotedSkillID+".md")); err != nil {
		t.Fatalf("promoted body file missing: %v", err)
	}
	entry, ok := skillStore.Get(promoted.PromotedSkillID)
	if !ok {
		t.Fatal("promoted skill not in the reloaded index")
	}
	if entry.SourceProposalID != created.ProposalID || entry.SourceRunID != runID {
		t.Fatalf("promoted provenance = %#v, want proposal/run ids", entry.Metadata)
	}

	// The learned skill is selectable via its related tool being enabled.
	selected := skill.Select(skillStore.Snapshot(), skill.SelectionInput{ToolNames: []string{"echo"}}, skill.SelectConfig{MaxSelected: 3})
	if len(selected) != 1 || selected[0].Ref.ID != promoted.PromotedSkillID {
		t.Fatalf("Select() = %#v, want the promoted skill via tool availability", selected)
	}

	audit, err := os.ReadFile(auditPath)
	if err != nil || !strings.Contains(string(audit), string(domain.EventSkillProposalApproved)) || !strings.Contains(string(audit), string(domain.EventSkillPromoted)) {
		t.Fatalf("audit log missing approve/promote records: %v\n%s", err, audit)
	}

	// A promoted proposal cannot be approved again.
	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_again", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"` + created.ProposalID + `","token":"admin-tok"}`),
	}); err == nil {
		t.Fatal("second approve error = nil, want not-pending error")
	}
}

func TestRuntimeServiceRejectFlow(t *testing.T) {
	ctx := context.Background()
	service, proposals, _, fileStore, _, auditPath, events, unsubscribe := newReviewService(t)
	defer unsubscribe()
	defer service.Close()
	runID := completeToolRun(t, service, fileStore, events)

	proposal, err := service.CreateSkillProposalFromRun(ctx, runID, skill.ActorDeveloper, true)
	if err != nil {
		t.Fatalf("CreateSkillProposalFromRun() error = %v", err)
	}
	_ = collectRuntimeEventsUntil(t, events, string(domain.EventSkillReviewRequired))

	rejectPayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_reject", Method: "skill.proposal.reject",
		Params: json.RawMessage(`{"proposal_id":"` + proposal.ProposalID + `","reason":"too narrow","token":"admin-tok"}`),
	})
	if err != nil {
		t.Fatalf("skill.proposal.reject error = %v", err)
	}
	var rejected protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(rejectPayload, &rejected); err != nil {
		t.Fatalf("unmarshal rejected proposal: %v", err)
	}
	if rejected.Status != string(skill.ProposalRejected) {
		t.Fatalf("status = %q, want rejected", rejected.Status)
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillAuditRecorded))
	assertOrderedSubsequence(t, eventNames(received),
		string(domain.EventSkillProposalRejected),
		string(domain.EventSkillAuditRecorded),
	)
	stored, err := proposals.Get(proposal.ProposalID)
	if err != nil || stored.Status != skill.ProposalRejected {
		t.Fatalf("stored proposal = %#v, %v; want rejected", stored, err)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil || !strings.Contains(string(audit), string(domain.EventSkillProposalRejected)) {
		t.Fatalf("audit log missing reject record: %v", err)
	}

	// A rejected proposal cannot be approved.
	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_late", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"` + proposal.ProposalID + `","token":"admin-tok"}`),
	}); err == nil {
		t.Fatal("approve after reject error = nil, want not-pending error")
	}
}

func TestRuntimeServiceApproveConflictMarksFailed(t *testing.T) {
	ctx := context.Background()
	service, proposals, skillStore, fileStore, skillsDir, auditPath, events, unsubscribe := newReviewService(t)
	defer unsubscribe()
	defer service.Close()
	runID := completeToolRun(t, service, fileStore, events)

	// Seed the active index with the id the proposal will target.
	seed := `{"skills":[{"id":"existing-skill","name":"existing","path":"existing-skill.md","status":"active","priority":100}]}`
	if err := os.WriteFile(filepath.Join(skillsDir, "index.json"), []byte(seed+"\n"), 0o644); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "existing-skill.md"), []byte("existing body"), 0o644); err != nil {
		t.Fatalf("seed body: %v", err)
	}
	if err := skillStore.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	now := time.Unix(1700000000, 0).UTC()
	conflicting := skill.SkillProposal{
		ProposalID:  "prop_conflict",
		SourceRunID: runID,
		Title:       "Conflicting proposal",
		SkillID:     "existing-skill",
		Body:        "# Conflicting proposal\n\nBody.",
		Status:      skill.ProposalPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := proposals.Create(conflicting); err != nil {
		t.Fatalf("Create(conflicting) error = %v", err)
	}

	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_conflict", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"prop_conflict","token":"admin-tok"}`),
	}); err == nil {
		t.Fatal("approve conflicting proposal error = nil, want conflict error")
	}

	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillAuditRecorded))
	assertOrderedSubsequence(t, eventNames(received),
		string(domain.EventSkillProposalApproved),
		string(domain.EventSkillPromotionFailed),
		string(domain.EventSkillAuditRecorded),
	)
	stored, err := proposals.Get("prop_conflict")
	if err != nil || stored.Status != skill.ProposalFailed {
		t.Fatalf("stored proposal = %#v, %v; want failed", stored, err)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil || !strings.Contains(string(audit), string(domain.EventSkillPromotionFailed)) {
		t.Fatalf("audit log missing promotion-failed record: %v", err)
	}
}

func TestRuntimeServiceApproveBumpsExistingLearnedSkill(t *testing.T) {
	ctx := context.Background()
	service, proposals, skillStore, fileStore, skillsDir, _, events, unsubscribe := newReviewService(t)
	defer unsubscribe()
	defer service.Close()
	runID := completeToolRun(t, service, fileStore, events)

	// First promotion lands the learned skill at v1.
	first, err := service.CreateSkillProposalFromRun(ctx, runID, skill.ActorAdmin, true)
	if err != nil {
		t.Fatalf("CreateSkillProposalFromRun() error = %v", err)
	}
	if _, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_v1", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"` + first.ProposalID + `","token":"admin-tok"}`),
	}); err != nil {
		t.Fatalf("first approve error = %v", err)
	}
	_ = collectRuntimeEventsUntil(t, events, string(domain.EventSkillAuditRecorded))

	// A second proposal targets the same skill id (created manually: the
	// deterministic prop_<runID> id is already taken by the first proposal).
	now := time.Unix(1700000000, 0).UTC()
	second := skill.SkillProposal{
		ProposalID:  "prop_v2",
		SourceRunID: runID,
		Title:       "Learned workflow, revised",
		SkillID:     first.SkillID,
		Purpose:     "Revised purpose",
		Body:        "# Learned workflow, revised\n\nUpdated procedure.",
		Status:      skill.ProposalPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := proposals.Create(second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	approvePayload, err := service.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "sp_v2", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"prop_v2","token":"admin-tok"}`),
	})
	if err != nil {
		t.Fatalf("second approve error = %v", err)
	}
	var promoted protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(approvePayload, &promoted); err != nil {
		t.Fatalf("unmarshal promoted proposal: %v", err)
	}
	if promoted.Status != string(skill.ProposalPromoted) || promoted.Version != "2" {
		t.Fatalf("approve payload = %#v, want promoted at version 2", promoted.SkillProposalSummary)
	}

	// The SkillPromoted event carries the new version.
	received := collectRuntimeEventsUntil(t, events, string(domain.EventSkillAuditRecorded))
	sawPromoted := false
	for _, ev := range received {
		if ev.Event != string(domain.EventSkillPromoted) {
			continue
		}
		sawPromoted = true
		var payload struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil || payload.Version != "2" {
			t.Fatalf("SkillPromoted payload = %s, %v; want version 2", ev.Payload, err)
		}
	}
	if !sawPromoted {
		t.Fatal("SkillPromoted event not observed for the version bump")
	}

	// One index entry at v2, the reloaded store serves the new body, and the
	// previous body is archived.
	entry, ok := skillStore.Get(first.SkillID)
	if !ok || entry.Version != "2" || entry.Body != second.Body {
		t.Fatalf("reloaded entry = %#v, want v2 with the revised body", entry.Metadata)
	}
	if entry.SourceProposalID != "prop_v2" {
		t.Fatalf("provenance = %q, want prop_v2", entry.SourceProposalID)
	}
	archived, err := os.ReadFile(filepath.Join(skillsDir, "archive", first.SkillID+"@1.md"))
	if err != nil || string(archived) != first.Body {
		t.Fatalf("archived body = %q, %v; want the v1 body", archived, err)
	}
}

func TestSkillLearningWSAdminGating(t *testing.T) {
	ctx := context.Background()

	// No admin token configured: mutating methods are disabled entirely.
	fileStore := store.NewFileStore(t.TempDir())
	noToken := NewRuntimeService(RuntimeServiceConfig{
		Store:         fileStore,
		LLMProvider:   &fakeProvider{response: worker.LLMResponse{Text: "ok"}},
		ProposalStore: mustProposalStore(t, fileStore),
		Now:           func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer noToken.Close()
	if _, err := noToken.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "g1", Method: "skill.proposal.approve",
		Params: json.RawMessage(`{"proposal_id":"p","token":"anything"}`),
	}); !errors.Is(err, errAdminDisabled) {
		t.Fatalf("error = %v, want errAdminDisabled", err)
	}

	// Token configured: a wrong token is rejected, reads stay open.
	withToken := NewRuntimeService(RuntimeServiceConfig{
		Store:         store.NewFileStore(t.TempDir()),
		LLMProvider:   &fakeProvider{response: worker.LLMResponse{Text: "ok"}},
		ProposalStore: mustProposalStore(t, fileStore),
		AdminToken:    "admin-tok",
		Now:           func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	defer withToken.Close()
	if _, err := withToken.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "g2", Method: "skill.proposal.create_from_run",
		Params: json.RawMessage(`{"run_id":"run_x","token":"wrong"}`),
	}); !errors.Is(err, errInvalidAdminToken) {
		t.Fatalf("error = %v, want errInvalidAdminToken", err)
	}
	if _, err := withToken.HandleClientRequest(ctx, protocol.ClientRequest{
		Type: "req", ID: "g3", Method: "skill.proposal.list",
	}); err != nil {
		t.Fatalf("skill.proposal.list without token error = %v, want open read", err)
	}
}

func mustProposalStore(t *testing.T, fileStore *store.FileStore) *skill.ProposalStore {
	t.Helper()
	proposals, err := skill.NewProposalStore(filepath.Join(fileStore.Root(), "skill_proposals"))
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	return proposals
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
