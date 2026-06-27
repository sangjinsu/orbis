package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
)

func newToolWorker(t *testing.T) (*ToolWorker, *store.FileStore) {
	t.Helper()
	reg := tool.NewRegistry()
	if err := tool.RegisterMockTools(reg, func() time.Time { return time.Unix(1700000000, 0).UTC() }); err != nil {
		t.Fatalf("RegisterMockTools error = %v", err)
	}
	fs := store.NewFileStore(t.TempDir())
	w := NewToolWorker(ToolWorkerConfig{
		Registry:       reg,
		Policy:         tool.NewPolicy(reg, tool.DefaultPolicyConfig()),
		Store:          fs,
		DefaultTimeout: time.Second,
	})
	return w, fs
}

func TestToolWorkerSuccessPersists(t *testing.T) {
	w, fs := newToolWorker(t)
	out := w.Run(context.Background(), ToolRequest{
		ToolName: "math.add", Args: json.RawMessage(`{"a":1,"b":2}`),
		IdempotencyKey: "k1", Attempt: 1, MaxAttempts: 2,
	})
	if out.Status != ToolOutcomeSucceeded {
		t.Fatalf("status = %v, err = %s", out.Status, out.Error)
	}
	if string(out.Result) != `{"result":3}` {
		t.Fatalf("result = %s, want result 3", out.Result)
	}
	rec, err := fs.LoadToolCall(context.Background(), "k1")
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if rec.Status != string(tool.CallStatusSucceeded) {
		t.Fatalf("record status = %s, want succeeded", rec.Status)
	}
}

func TestToolWorkerDeduplicates(t *testing.T) {
	w, fs := newToolWorker(t)
	// A previous success must not re-execute; the cached result is returned.
	if err := fs.SaveToolCall(context.Background(), store.ToolCallRecord{
		IdempotencyKey: "dk", ToolName: "math.add",
		Status: string(tool.CallStatusSucceeded), Result: json.RawMessage(`{"result":999}`),
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	out := w.Run(context.Background(), ToolRequest{
		ToolName: "math.add", Args: json.RawMessage(`{"a":1,"b":2}`),
		IdempotencyKey: "dk", Attempt: 1, MaxAttempts: 2,
	})
	if out.Status != ToolOutcomeDeduplicated {
		t.Fatalf("status = %v, want deduplicated", out.Status)
	}
	if string(out.Result) != `{"result":999}` {
		t.Fatalf("result = %s, want cached 999", out.Result)
	}
}

func TestToolWorkerTimeout(t *testing.T) {
	w, _ := newToolWorker(t)
	out := w.Run(context.Background(), ToolRequest{
		ToolName: "mock.sleep", Args: json.RawMessage(`{"duration":"5s"}`),
		IdempotencyKey: "sk", Attempt: 1, MaxAttempts: 2, Timeout: 20 * time.Millisecond,
	})
	if out.Status != ToolOutcomeFailed || !out.TimedOut {
		t.Fatalf("outcome = %+v, want timed-out failure", out)
	}
	if out.ReasonCode != "timeout" {
		t.Fatalf("reason = %s, want timeout", out.ReasonCode)
	}
}

func TestToolWorkerPolicyRejects(t *testing.T) {
	w, _ := newToolWorker(t)

	out := w.Run(context.Background(), ToolRequest{ToolName: "mock.dangerous", IdempotencyKey: "x", Attempt: 1, MaxAttempts: 2})
	if out.Status != ToolOutcomeRejected || out.ReasonCode != string(tool.ReasonToolsetNotAllowed) {
		t.Fatalf("dangerous outcome = %+v, want rejected toolset_not_allowed", out)
	}

	out = w.Run(context.Background(), ToolRequest{ToolName: "mock.fail_once", Args: json.RawMessage(`{}`), Attempt: 1, MaxAttempts: 2})
	if out.Status != ToolOutcomeRejected || out.ReasonCode != string(tool.ReasonMissingIdempotency) {
		t.Fatalf("missing-idempotency outcome = %+v, want rejected missing_idempotency_key", out)
	}
}

func TestToolWorkerFailOnceThenSucceed(t *testing.T) {
	w, _ := newToolWorker(t)
	first := w.Run(context.Background(), ToolRequest{
		ToolName: "mock.fail_once", Args: json.RawMessage(`{}`),
		IdempotencyKey: "fk", Attempt: 1, MaxAttempts: 2,
	})
	if first.Status != ToolOutcomeFailed {
		t.Fatalf("attempt 1 status = %v, want failed", first.Status)
	}
	if !first.Retryable {
		t.Fatal("attempt 1 should be retryable")
	}
	second := w.Run(context.Background(), ToolRequest{
		ToolName: "mock.fail_once", Args: json.RawMessage(`{}`),
		IdempotencyKey: "fk", Attempt: 2, MaxAttempts: 2,
	})
	if second.Status != ToolOutcomeSucceeded {
		t.Fatalf("attempt 2 status = %v, err = %s", second.Status, second.Error)
	}
}
