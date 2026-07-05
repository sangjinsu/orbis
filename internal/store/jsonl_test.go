package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

func TestFileStoreAppendsEventsAndSavesSnapshots(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewFileStore(dir)
	now := time.Unix(1700000000, 0).UTC()

	event := domain.Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      domain.EventUserMessageReceived,
		Seq:       1,
		CreatedAt: now,
		Payload:   json.RawMessage(`{"text":"hello"}`),
	}
	if err := store.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	eventFile, err := os.Open(filepath.Join(dir, "events", "session_1.jsonl"))
	if err != nil {
		t.Fatalf("open event file: %v", err)
	}
	defer eventFile.Close()

	scanner := bufio.NewScanner(eventFile)
	if !scanner.Scan() {
		t.Fatal("event file has no first line")
	}
	var gotEvent domain.Event
	if err := json.Unmarshal(scanner.Bytes(), &gotEvent); err != nil {
		t.Fatalf("unmarshal event line: %v", err)
	}
	if gotEvent.EventID != "evt_1" {
		t.Fatalf("EventID = %q, want evt_1", gotEvent.EventID)
	}

	session := domain.SessionState{SessionID: "session_1", CurrentRunID: "run_1", RunStatus: domain.RunWaitingLLM}
	if err := store.SaveSession(ctx, session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	loadedSession, err := store.LoadSession(ctx, "session_1")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loadedSession.CurrentRunID != "run_1" {
		t.Fatalf("CurrentRunID = %q, want run_1", loadedSession.CurrentRunID)
	}

	run := domain.RunState{RunID: "run_1", SessionID: "session_1", Status: domain.RunWaitingLLM}
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	loadedRun, err := store.LoadRun(ctx, "run_1")
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if loadedRun.SessionID != "session_1" {
		t.Fatalf("Run SessionID = %q, want session_1", loadedRun.SessionID)
	}
}

func TestFileStoreListsEventsAfterSeqWithLimit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewFileStore(dir)
	now := time.Unix(1700000000, 0).UTC()

	for i, typ := range []domain.EventType{
		domain.EventSessionCreated,
		domain.EventUserMessageReceived,
		domain.EventLLMCallStarted,
	} {
		event := domain.Event{
			EventID:   "evt_" + string(typ),
			SessionID: "session_1",
			RunID:     "run_1",
			Type:      typ,
			Seq:       int64(i + 1),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			Payload:   json.RawMessage(`{}`),
		}
		if err := store.AppendEvent(ctx, event); err != nil {
			t.Fatalf("AppendEvent(%d) error = %v", i, err)
		}
	}

	events, err := store.ListEvents(ctx, "session_1", ListEventsOptions{AfterSeq: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Seq != 2 || events[0].Type != domain.EventUserMessageReceived {
		t.Fatalf("event = %#v, want seq 2 UserMessageReceived", events[0])
	}
}

// A run snapshot is rewritten by the session event queue while service methods
// concurrently reload it; readers must never observe a torn write.
func TestFileStoreConcurrentSaveAndLoadRun(t *testing.T) {
	ctx := context.Background()
	fileStore := NewFileStore(t.TempDir())
	state := domain.RunState{RunID: "run_1", SessionID: "session_1", Status: domain.RunCompleted}
	if err := fileStore.SaveRun(ctx, state); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			if err := fileStore.SaveRun(ctx, state); err != nil {
				t.Errorf("concurrent SaveRun() error = %v", err)
				return
			}
		}
	}()
	for i := 0; i < 500; i++ {
		if _, err := fileStore.LoadRun(ctx, "run_1"); err != nil {
			t.Fatalf("LoadRun() during concurrent writes error = %v", err)
		}
	}
	<-done
}
