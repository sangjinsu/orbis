package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

func TestWebSocketSessionMessageReturnsImmediateAck(t *testing.T) {
	responsePayload, err := json.Marshal(protocol.AckPayload{
		SessionID: "session_1",
		RunID:     "run_1",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	runtime := &recordingRuntime{
		payload: responsePayload,
	}
	server := httptest.NewServer(NewHTTPHandler(runtime))
	defer server.Close()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.CloseNow()

	req := protocol.ClientRequest{
		Type:   "req",
		ID:     "req_1",
		Method: "session.message",
		Params: json.RawMessage(`{"session_id":"session_1","text":"hello"}`),
	}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var res protocol.ServerResponse
	if err := wsjson.Read(ctx, conn, &res); err != nil {
		t.Fatalf("read response: %v", err)
	}

	if runtime.seen.Method != "session.message" {
		t.Fatalf("runtime method = %q, want session.message", runtime.seen.Method)
	}
	if res.Type != "res" || res.ID != "req_1" || !res.OK {
		t.Fatalf("response = %#v, want ok res for req_1", res)
	}
	var payload protocol.AckPayload
	if err := json.Unmarshal(res.Payload, &payload); err != nil {
		t.Fatalf("unmarshal response payload: %v", err)
	}
	if payload.SessionID != "session_1" || payload.RunID != "run_1" {
		t.Fatalf("payload = %#v, want session_1/run_1", payload)
	}
}

func TestWebSocketSubscribeReceivesRuntimeEvents(t *testing.T) {
	runtime := &recordingRuntime{}
	broker := newRecordingBroker()
	server := httptest.NewServer(NewHTTPHandler(runtime, WithBroker(broker)))
	defer server.Close()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.CloseNow()

	req := protocol.ClientRequest{
		Type:   "req",
		ID:     "req_sub",
		Method: "session.subscribe",
		Params: json.RawMessage(`{"session_id":"session_1"}`),
	}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var res protocol.ServerResponse
	if err := wsjson.Read(ctx, conn, &res); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if !res.OK {
		t.Fatalf("subscribe response = %#v, want ok", res)
	}

	broker.publish(protocol.RuntimeEvent{
		Type:      "event",
		Event:     "RunStarted",
		Seq:       1,
		SessionID: "session_1",
		RunID:     "run_1",
		Payload:   json.RawMessage(`{}`),
	})

	var event protocol.RuntimeEvent
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("read runtime event: %v", err)
	}
	if event.Event != "RunStarted" || event.SessionID != "session_1" {
		t.Fatalf("event = %#v, want RunStarted/session_1", event)
	}
}

func TestWebSocketSubscribeGlobalScope(t *testing.T) {
	runtime := &recordingRuntime{}
	broker := newRecordingBroker()
	server := httptest.NewServer(NewHTTPHandler(runtime, WithBroker(broker)))
	defer server.Close()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.CloseNow()

	// A global-scope subscription needs no session_id.
	if err := wsjson.Write(ctx, conn, protocol.ClientRequest{
		Type: "req", ID: "req_gsub", Method: "session.subscribe",
		Params: json.RawMessage(`{"scope":"global"}`),
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var res protocol.ServerResponse
	if err := wsjson.Read(ctx, conn, &res); err != nil || !res.OK {
		t.Fatalf("global subscribe response = %#v, %v; want ok", res, err)
	}

	// A global event has no session and no sequence: it is a live feed only.
	broker.publishGlobal(protocol.RuntimeEvent{
		Type:    "event",
		Event:   "SkillIndexReloaded",
		Payload: json.RawMessage(`{"actor":"admin","count":3}`),
	})
	var event protocol.RuntimeEvent
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("read global event: %v", err)
	}
	if event.Event != "SkillIndexReloaded" || event.SessionID != "" || event.Seq != 0 {
		t.Fatalf("event = %#v, want sessionless SkillIndexReloaded", event)
	}
}

func TestWebSocketSubscribeRejectsUnknownScopeAndMissingSession(t *testing.T) {
	runtime := &recordingRuntime{}
	server := httptest.NewServer(NewHTTPHandler(runtime, WithBroker(newRecordingBroker())))
	defer server.Close()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.CloseNow()

	for _, tc := range []struct {
		id     string
		params string
		want   string
	}{
		{"req_noscope", `{}`, "session_id is required"},
		{"req_badscope", `{"scope":"everything"}`, "unknown scope"},
	} {
		if err := wsjson.Write(ctx, conn, protocol.ClientRequest{
			Type: "req", ID: tc.id, Method: "session.subscribe",
			Params: json.RawMessage(tc.params),
		}); err != nil {
			t.Fatalf("write subscribe %s: %v", tc.id, err)
		}
		var res protocol.ServerResponse
		if err := wsjson.Read(ctx, conn, &res); err != nil {
			t.Fatalf("read response %s: %v", tc.id, err)
		}
		if res.OK || !strings.Contains(res.Error, tc.want) {
			t.Fatalf("%s response = %#v, want error containing %q", tc.id, res, tc.want)
		}
	}
}

type recordingRuntime struct {
	seen    protocol.ClientRequest
	payload json.RawMessage
}

func (r *recordingRuntime) HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	_ = ctx
	r.seen = req
	return r.payload, nil
}

type recordingBroker struct {
	events chan protocol.RuntimeEvent
	global chan protocol.RuntimeEvent
}

func newRecordingBroker() *recordingBroker {
	return &recordingBroker{
		events: make(chan protocol.RuntimeEvent, 1),
		global: make(chan protocol.RuntimeEvent, 1),
	}
}

func (b *recordingBroker) Subscribe(ctx context.Context, sessionID string) (<-chan protocol.RuntimeEvent, func()) {
	_ = ctx
	_ = sessionID
	return b.events, func() {}
}

func (b *recordingBroker) SubscribeGlobal(ctx context.Context) (<-chan protocol.RuntimeEvent, func()) {
	_ = ctx
	return b.global, func() {}
}

func (b *recordingBroker) publish(event protocol.RuntimeEvent) {
	b.events <- event
}

func (b *recordingBroker) publishGlobal(event protocol.RuntimeEvent) {
	b.global <- event
}
