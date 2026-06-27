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
