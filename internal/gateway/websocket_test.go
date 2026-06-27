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
	runtime := &recordingRuntime{
		ack: protocol.AckPayload{
			SessionID: "session_1",
			RunID:     "run_1",
		},
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

type recordingRuntime struct {
	seen protocol.ClientRequest
	ack  protocol.AckPayload
}

func (r *recordingRuntime) HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (protocol.AckPayload, error) {
	_ = ctx
	r.seen = req
	return r.ack, nil
}
