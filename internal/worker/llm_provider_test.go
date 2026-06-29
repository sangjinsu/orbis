package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sangjinsu/orbis/internal/tool"
)

func TestOpenAIProviderCompleteUsesResponsesAPI(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_1",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [
						{"type": "output_text", "text": "hello from real provider", "annotations": []}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-test",
		Client:  server.Client(),
	})

	resp, err := provider.Complete(context.Background(), LLMRequest{
		Input: "Say hello",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses")
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotBody["model"] != "gpt-test" {
		t.Fatalf("model = %v, want %q", gotBody["model"], "gpt-test")
	}
	if gotBody["input"] != "Say hello" {
		t.Fatalf("input = %v, want %q", gotBody["input"], "Say hello")
	}
	if resp.Text != "hello from real provider" {
		t.Fatalf("Text = %q, want %q", resp.Text, "hello from real provider")
	}
	if resp.ProviderResponseID != "resp_1" {
		t.Fatalf("ProviderResponseID = %q, want %q", resp.ProviderResponseID, "resp_1")
	}
}

func TestOpenAIProviderSendsToolsAndParsesFunctionCall(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_2",
			"status": "completed",
			"output": [
				{"type": "function_call", "call_id": "call_abc", "name": "math_add", "arguments": "{\"a\":1,\"b\":2}"}
			]
		}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, Model: "gpt-test", Client: server.Client(),
	})

	resp, err := provider.Complete(context.Background(), LLMRequest{
		Input: "add 1 and 2",
		Tools: []tool.ToolSchema{{
			Name:        "math.add",
			Description: "Add two numbers",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one tool definition", gotBody["tools"])
	}
	first, _ := tools[0].(map[string]any)
	// The dotted registry name is sanitized to the Responses API pattern on the wire.
	if first["type"] != "function" || first["name"] != "math_add" {
		t.Fatalf("tool def = %v, want flattened function math_add", first)
	}
	if _, hasParams := first["parameters"]; !hasParams {
		t.Fatalf("tool def missing parameters: %v", first)
	}

	if resp.ToolCall == nil {
		t.Fatal("resp.ToolCall = nil, want parsed function call")
	}
	// The sanitized wire name is mapped back to the registered tool name.
	if resp.ToolCall.ToolCallID != "call_abc" || resp.ToolCall.Name != "math.add" {
		t.Fatalf("tool call = %#v, want call_abc/math.add", resp.ToolCall)
	}
	if string(resp.ToolCall.Args) != `{"a":1,"b":2}` {
		t.Fatalf("tool call args = %s, want {\"a\":1,\"b\":2}", resp.ToolCall.Args)
	}
}

func TestOpenAIProviderBuildsInputFromMessages(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_3","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"3"}]}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, Model: "gpt-test", Client: server.Client(),
	})

	_, err := provider.Complete(context.Background(), LLMRequest{
		Messages: []LLMMessage{
			{Role: "user", Content: "add 1 and 2"},
			{Role: "assistant", ToolCallID: "call_abc", ToolName: "math.add", ToolArgs: json.RawMessage(`{"a":1,"b":2}`)},
			{Role: "tool", ToolCallID: "call_abc", Content: `{"result":3}`},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	input, ok := gotBody["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("input = %v, want 3 items", gotBody["input"])
	}
	user, _ := input[0].(map[string]any)
	if user["role"] != "user" || user["content"] != "add 1 and 2" {
		t.Fatalf("input[0] = %v, want user message", user)
	}
	call, _ := input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_abc" || call["arguments"] != `{"a":1,"b":2}` {
		t.Fatalf("input[1] = %v, want function_call", call)
	}
	// The prior assistant function_call turn's name is sanitized on the wire too.
	if call["name"] != "math_add" {
		t.Fatalf("input[1] name = %v, want sanitized math_add", call["name"])
	}
	output, _ := input[2].(map[string]any)
	if output["type"] != "function_call_output" || output["call_id"] != "call_abc" || output["output"] != `{"result":3}` {
		t.Fatalf("input[2] = %v, want function_call_output", output)
	}
}

func TestSanitizeAndOriginalToolName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"echo", "echo"},
		{"math.add", "math_add"},
		{"mock.fail_once", "mock_fail_once"},
		{"a.b.c", "a_b_c"},
	} {
		if got := sanitizeToolName(tc.in); got != tc.want {
			t.Fatalf("sanitizeToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	tools := []tool.ToolSchema{{Name: "math.add"}, {Name: "time.now"}}
	if got := originalToolName(tools, "math_add"); got != "math.add" {
		t.Fatalf("originalToolName(math_add) = %q, want math.add", got)
	}
	// Unknown sanitized names fall back unchanged.
	if got := originalToolName(tools, "unknown_x"); got != "unknown_x" {
		t.Fatalf("originalToolName(unknown_x) = %q, want unknown_x", got)
	}
}
