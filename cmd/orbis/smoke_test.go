package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

func TestRunWSSmokeSucceedsOnRunCompleted(t *testing.T) {
	server := newSmokeServer(t, []protocol.RuntimeEvent{
		{Type: "event", Event: "UserMessageReceived", SessionID: "session_smoke", RunID: "run_smoke", Payload: json.RawMessage(`{}`)},
		{Type: "event", Event: "LLMCallStarted", SessionID: "session_smoke", RunID: "run_smoke", Payload: json.RawMessage(`{}`)},
		{Type: "event", Event: "RunCompleted", SessionID: "session_smoke", RunID: "run_smoke", Payload: json.RawMessage(`{}`)},
	})
	defer server.Close()

	var out strings.Builder
	err := runWSSmoke(context.Background(), wsSmokeConfig{
		URL:       "ws" + strings.TrimPrefix(server.URL, "http") + "/ws",
		SessionID: "session_smoke",
		Text:      "hello",
		Timeout:   time.Second,
	}, &out)
	if err != nil {
		t.Fatalf("runWSSmoke() error = %v", err)
	}
	if strings.Contains(out.String(), "hello") {
		t.Fatalf("smoke output leaked prompt text: %s", out.String())
	}
	if !strings.Contains(out.String(), "RunCompleted") {
		t.Fatalf("smoke output = %s, want RunCompleted", out.String())
	}
}

func TestRunWSSmokeFailsOnRunFailed(t *testing.T) {
	server := newSmokeServer(t, []protocol.RuntimeEvent{
		{Type: "event", Event: "LLMCallFailed", SessionID: "session_smoke", RunID: "run_smoke", Payload: json.RawMessage(`{"error":"provider timeout"}`)},
		{Type: "event", Event: "RunFailed", SessionID: "session_smoke", RunID: "run_smoke", Payload: json.RawMessage(`{"error":"provider timeout"}`)},
	})
	defer server.Close()

	var out strings.Builder
	err := runWSSmoke(context.Background(), wsSmokeConfig{
		URL:       "ws" + strings.TrimPrefix(server.URL, "http") + "/ws",
		SessionID: "session_smoke",
		Text:      "hello",
		Timeout:   time.Second,
	}, &out)
	if err == nil {
		t.Fatal("runWSSmoke() error = nil, want failure")
	}
	if !strings.Contains(out.String(), "RunFailed") {
		t.Fatalf("smoke output = %s, want RunFailed", out.String())
	}
}

func TestWSURLFromAddr(t *testing.T) {
	for _, tc := range []struct {
		name string
		addr string
		want string
	}{
		{name: "host port", addr: "localhost:8080", want: "ws://localhost:8080/ws"},
		{name: "port only", addr: ":8080", want: "ws://127.0.0.1:8080/ws"},
		{name: "http", addr: "http://localhost:8080", want: "ws://localhost:8080/ws"},
		{name: "existing websocket path", addr: "ws://localhost:8080/ws", want: "ws://localhost:8080/ws"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wsURLFromAddr(tc.addr); got != tc.want {
				t.Fatalf("wsURLFromAddr(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}

func newSmokeServer(t *testing.T, events []protocol.RuntimeEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
		defer conn.CloseNow()

		ctx := context.Background()
		for i := 0; i < 2; i++ {
			var req protocol.ClientRequest
			if err := wsjson.Read(ctx, conn, &req); err != nil {
				t.Fatalf("read request: %v", err)
			}
			payload, _ := json.Marshal(protocol.AckPayload{SessionID: "session_smoke", RunID: "run_smoke"})
			if err := wsjson.Write(ctx, conn, protocol.ServerResponse{Type: "res", ID: req.ID, OK: true, Payload: payload}); err != nil {
				t.Fatalf("write response: %v", err)
			}
		}
		for _, event := range events {
			if err := wsjson.Write(ctx, conn, event); err != nil {
				t.Fatalf("write event: %v", err)
			}
		}
	}))
}
