package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

// syncBuffer lets the test read output while runWatch writes from a goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newWatchServer accepts one WS client, asserts it subscribes with
// scope "global", acks, streams the canned events, and holds the connection
// open until the client disconnects.
func newWatchServer(t *testing.T, ackOK bool, events []protocol.RuntimeEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()

		var req protocol.ClientRequest
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			t.Errorf("read subscribe request: %v", err)
			return
		}
		if req.Method != "session.subscribe" {
			t.Errorf("method = %q, want session.subscribe", req.Method)
		}
		var params struct {
			Scope     string `json:"scope"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Scope != "global" || params.SessionID != "" {
			t.Errorf("subscribe params = %s, want scope global without session_id", req.Params)
		}

		res := protocol.ServerResponse{Type: "res", ID: req.ID, OK: ackOK}
		if !ackOK {
			res.Error = "broker is not configured"
		}
		if err := wsjson.Write(ctx, conn, res); err != nil {
			return
		}
		if !ackOK {
			return
		}
		for _, event := range events {
			if err := wsjson.Write(ctx, conn, event); err != nil {
				return
			}
		}
		<-ctx.Done()
	}))
}

func waitForOutput(t *testing.T, buf *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output %q never contained %q", buf.String(), want)
}

func TestRunWatchStreamsGlobalEvents(t *testing.T) {
	events := []protocol.RuntimeEvent{
		{Type: "event", Event: "SkillProposalCreated", Seq: 7, SessionID: "sess_1", RunID: "run_1", Payload: json.RawMessage(`{"proposal_id":"prop_run_1"}`)},
		{Type: "event", Event: "SkillIndexReloaded", Payload: json.RawMessage(`{"actor":"alice","count":9}`)},
	}
	server := newWatchServer(t, true, events)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- runWatch(ctx, watchConfig{URL: wsURLFromAddr(server.URL)}, out) }()

	waitForOutput(t, out, "SkillIndexReloaded")
	got := out.String()
	if !strings.Contains(got, "SkillProposalCreated") || !strings.Contains(got, "seq=7") ||
		!strings.Contains(got, "run=run_1") || !strings.Contains(got, `"actor":"alice"`) {
		t.Fatalf("output = %q, want both events with seq/run/payload", got)
	}

	// Interrupting the context ends the stream as a normal exit.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch() after cancel error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWatch() did not return after context cancel")
	}
}

func TestRunWatchJSONOutputsRawEvents(t *testing.T) {
	events := []protocol.RuntimeEvent{
		{Type: "event", Event: "SkillPromoted", Seq: 3, SessionID: "sess_1", Payload: json.RawMessage(`{"version":"2"}`)},
	}
	server := newWatchServer(t, true, events)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- runWatch(ctx, watchConfig{URL: wsURLFromAddr(server.URL), JSON: true}, out) }()

	waitForOutput(t, out, "SkillPromoted")
	// NDJSON: the event line is the raw wire encoding and decodes back.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var event protocol.RuntimeEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil || event.Event != "SkillPromoted" || event.Seq != 3 {
		t.Fatalf("last line = %q, %v; want the raw SkillPromoted event", lines[len(lines)-1], err)
	}
	cancel()
	<-done
}

func TestRunWatchFailsWhenSubscribeRejected(t *testing.T) {
	server := newWatchServer(t, false, nil)
	defer server.Close()

	out := &syncBuffer{}
	err := runWatch(context.Background(), watchConfig{URL: wsURLFromAddr(server.URL), Timeout: 5 * time.Second}, out)
	if err == nil || !strings.Contains(err.Error(), "subscribe rejected") {
		t.Fatalf("runWatch() error = %v, want subscribe-rejected error", err)
	}
}
