package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	eventPath := filepath.Join(fileStore.Root(), "events", "session_1.jsonl")
	if _, err := os.Stat(eventPath); err != nil {
		t.Fatalf("event log was not written: %v", err)
	}
}

type fakeProvider struct {
	response worker.LLMResponse
}

func (p *fakeProvider) Complete(ctx context.Context, req worker.LLMRequest) (worker.LLMResponse, error) {
	_ = ctx
	_ = req
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
