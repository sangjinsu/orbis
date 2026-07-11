package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

// newChatServer accepts one chat client: it acks the subscribe, then for each
// session.message acks with a run id and streams the next scripted turn.
func newChatServer(t *testing.T, turns [][]protocol.RuntimeEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()

		var sub protocol.ClientRequest
		if err := wsjson.Read(ctx, conn, &sub); err != nil || sub.Method != "session.subscribe" {
			t.Errorf("first request = %+v, %v; want session.subscribe", sub, err)
			return
		}
		if err := wsjson.Write(ctx, conn, protocol.ServerResponse{Type: "res", ID: sub.ID, OK: true}); err != nil {
			return
		}

		for _, turn := range turns {
			var msg protocol.ClientRequest
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				return
			}
			if msg.Method != "session.message" {
				t.Errorf("request method = %q, want session.message", msg.Method)
				return
			}
			ackPayload, _ := json.Marshal(protocol.AckPayload{SessionID: "chat_test", RunID: "run_test"})
			if err := wsjson.Write(ctx, conn, protocol.ServerResponse{Type: "res", ID: msg.ID, OK: true, Payload: ackPayload}); err != nil {
				return
			}
			for _, event := range turn {
				if err := wsjson.Write(ctx, conn, event); err != nil {
					return
				}
			}
		}
		// Keep reading like the real server does, so the client's graceful
		// close handshake completes instead of waiting out its timeout.
		for {
			var discard json.RawMessage
			if err := wsjson.Read(ctx, conn, &discard); err != nil {
				return
			}
		}
	}))
}

func chatEvent(name, payload string) protocol.RuntimeEvent {
	return protocol.RuntimeEvent{
		Type: "event", Event: name, SessionID: "chat_test", RunID: "run_test",
		Payload: json.RawMessage(payload),
	}
}

func TestRunChatStreamsDeltasAndToolNotes(t *testing.T) {
	server := newChatServer(t, [][]protocol.RuntimeEvent{{
		chatEvent("SkillApplied", `{"skill_ids":["tool-calling-policy"],"count":1}`),
		chatEvent("ToolCallStarted", `{"tool_name":"math.add","args":{"a":1,"b":2}}`),
		chatEvent("ToolCallSucceeded", `{"tool_name":"math.add","duration_ms":3}`),
		chatEvent("AssistantDelta", `{"delta":"The answer "}`),
		chatEvent("AssistantDelta", `{"delta":"is 3."}`),
		chatEvent("FinalAnswerEmitted", `{"text":"The answer is 3."}`),
		chatEvent("RunCompleted", `{}`),
	}})
	defer server.Close()

	var out strings.Builder
	err := runChat(context.Background(), chatConfig{
		URL: wsURLFromAddr(server.URL), SessionID: "chat_test", Verbosity: chatMedium,
	}, strings.NewReader("add 1 and 2\n/quit\n"), &out)
	if err != nil {
		t.Fatalf("runChat() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"session chat_test",
		"[skill] tool-calling-policy",
		"[tool] math.add {\"a\":1,\"b\":2}",
		"[tool] math.add ok (3ms)",
		"orbis> The answer is 3.",
		"bye",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	// Deltas were streamed, so the final answer must not be printed twice.
	if strings.Count(got, "The answer is 3.") != 1 {
		t.Fatalf("final answer duplicated:\n%s", got)
	}
}

func TestRunChatQuietShowsTextOnly(t *testing.T) {
	server := newChatServer(t, [][]protocol.RuntimeEvent{{
		chatEvent("ToolCallStarted", `{"tool_name":"math.add"}`),
		chatEvent("ToolCallSucceeded", `{"tool_name":"math.add"}`),
		chatEvent("FinalAnswerEmitted", `{"text":"done"}`),
		chatEvent("RunCompleted", `{}`),
	}})
	defer server.Close()

	var out strings.Builder
	if err := runChat(context.Background(), chatConfig{
		URL: wsURLFromAddr(server.URL), SessionID: "chat_test", Verbosity: chatQuiet,
	}, strings.NewReader("hi\n"), &out); err != nil {
		t.Fatalf("runChat() error = %v", err)
	}
	got := out.String()
	if strings.Contains(got, "[tool]") {
		t.Fatalf("quiet output contains tool notes:\n%s", got)
	}
	// Without deltas the final answer text is the fallback.
	if !strings.Contains(got, "orbis> done") {
		t.Fatalf("output missing the final answer:\n%s", got)
	}
}

func TestRunChatVerbosePrintsEventNames(t *testing.T) {
	server := newChatServer(t, [][]protocol.RuntimeEvent{{
		chatEvent("LLMCallStarted", `{}`),
		chatEvent("FinalAnswerEmitted", `{"text":"ok"}`),
		chatEvent("RunCompleted", `{}`),
	}})
	defer server.Close()

	var out strings.Builder
	if err := runChat(context.Background(), chatConfig{
		URL: wsURLFromAddr(server.URL), SessionID: "chat_test", Verbosity: chatVerbose,
	}, strings.NewReader("hi\n"), &out); err != nil {
		t.Fatalf("runChat() error = %v", err)
	}
	if !strings.Contains(out.String(), "· LLMCallStarted") || !strings.Contains(out.String(), "· RunCompleted") {
		t.Fatalf("verbose output missing event lines:\n%s", out.String())
	}
}

func TestRunChatSurfacesRunFailureAndContinues(t *testing.T) {
	server := newChatServer(t, [][]protocol.RuntimeEvent{
		{chatEvent("RunFailed", `{"error":"provider unavailable"}`)},
		{chatEvent("FinalAnswerEmitted", `{"text":"recovered"}`), chatEvent("RunCompleted", `{}`)},
	})
	defer server.Close()

	var out strings.Builder
	if err := runChat(context.Background(), chatConfig{
		URL: wsURLFromAddr(server.URL), SessionID: "chat_test", Verbosity: chatMedium,
	}, strings.NewReader("first\nsecond\n/quit\n"), &out); err != nil {
		t.Fatalf("runChat() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[error] run failed: provider unavailable") {
		t.Fatalf("output missing the run failure:\n%s", got)
	}
	// The REPL survives a failed run and completes the next turn.
	if !strings.Contains(got, "orbis> recovered") {
		t.Fatalf("output missing the second turn:\n%s", got)
	}
}

func TestChatVerboseAndQuietAreExclusive(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"chat", "--verbose", "--quiet"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	if err := root.Execute(); !errors.Is(err, errUsage) {
		t.Fatalf("chat --verbose --quiet error = %v, want errUsage", err)
	}
}
