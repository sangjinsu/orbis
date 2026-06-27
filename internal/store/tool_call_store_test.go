package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSanitizeKeyIsStableAndCollisionResistant(t *testing.T) {
	a := sanitizeKey("run_1:tool:call/abc")
	b := sanitizeKey("run_1:tool:call/abc")
	if a != b {
		t.Fatalf("sanitizeKey not stable: %q vs %q", a, b)
	}
	c := sanitizeKey("run_1:tool:call_abc")
	if a == c {
		t.Fatal("distinct keys collided after sanitization")
	}
	if a == "" {
		t.Fatal("sanitized key is empty")
	}
}

func TestToolCallStoreRoundTrip(t *testing.T) {
	s := NewFileStore(t.TempDir())
	ctx := context.Background()
	rec := ToolCallRecord{
		IdempotencyKey: "run1:tool:call/abc",
		SessionID:      "s",
		RunID:          "r",
		ToolCallID:     "c",
		ToolName:       "echo",
		Status:         "succeeded",
		Attempts:       1,
		Result:         json.RawMessage(`{"text":"hi"}`),
		CreatedAt:      time.Unix(1, 0).UTC(),
		UpdatedAt:      time.Unix(2, 0).UTC(),
	}
	if err := s.SaveToolCall(ctx, rec); err != nil {
		t.Fatalf("SaveToolCall error = %v", err)
	}
	got, err := s.LoadToolCall(ctx, rec.IdempotencyKey)
	if err != nil {
		t.Fatalf("LoadToolCall error = %v", err)
	}
	if got.Status != "succeeded" || got.ToolName != "echo" {
		t.Fatalf("record = %+v", got)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(got.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Text != "hi" {
		t.Fatalf("result.Text = %q, want hi", result.Text)
	}
}

func TestToolCallStoreNotFound(t *testing.T) {
	s := NewFileStore(t.TempDir())
	_, err := s.LoadToolCall(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
