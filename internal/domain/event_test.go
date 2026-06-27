package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventJSONEnvelopeUsesStableFieldNames(t *testing.T) {
	event := Event{
		EventID:   "evt_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Type:      EventUserMessageReceived,
		Seq:       7,
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		Payload:   json.RawMessage(`{"text":"hello"}`),
	}

	got, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	for _, key := range []string{"event_id", "session_id", "run_id", "type", "seq", "created_at", "payload"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(got))
		}
	}
	if decoded["type"] != string(EventUserMessageReceived) {
		t.Fatalf("type = %v, want %q", decoded["type"], EventUserMessageReceived)
	}
}

func TestActionRequiresIdempotencyKey(t *testing.T) {
	action := Action{
		ActionID:       "act_1",
		SessionID:      "session_1",
		RunID:          "run_1",
		Type:           ActionDispatchLLMCall,
		IdempotencyKey: "run_1:llm:1",
		Payload:        json.RawMessage(`{"text":"hello"}`),
	}

	if err := action.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	action.IdempotencyKey = ""
	if err := action.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing idempotency key error")
	}
}
