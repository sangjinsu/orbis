package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
