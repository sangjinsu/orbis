package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sangjinsu/orbis/internal/tool"
)

type LLMProvider interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
	Stream(ctx context.Context, req LLMRequest) (<-chan LLMStreamEvent, error)
}

// LLMMessage is a provider-neutral conversation turn. Tool turns carry the
// linkage fields so a provider can reconstruct function_call /
// function_call_output items.
type LLMMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolArgs   json.RawMessage `json:"tool_args,omitempty"`
}

type LLMRequest struct {
	Input        string
	Instructions string
	// Messages is the full conversation context. When present a provider should
	// prefer it over Input. Tools advertises the callable tool schemas.
	Messages []LLMMessage
	Tools    []tool.ToolSchema
}

type LLMResponse struct {
	Text               string
	ProviderResponseID string
	ToolCall           *ToolCall
}

type ToolCall struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
}

type LLMStreamEvent struct {
	Delta              string
	ProviderResponseID string
	ToolCall           *ToolCall
	Done               bool
	Err                error
}

type OpenAIProviderConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

type OpenAIProvider struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewOpenAIProvider(cfg OpenAIProviderConfig) *OpenAIProvider {
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   cfg.Model,
		client:  client,
	}
}

func (p *OpenAIProvider) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	body := map[string]any{
		"model": p.model,
		"input": req.Input,
		"store": false,
	}
	if req.Instructions != "" {
		body["instructions"] = req.Instructions
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("marshal response request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/responses", bytes.NewReader(encoded))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("new response request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("create response: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return LLMResponse{}, fmt.Errorf("create response status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(data)))
	}

	var decoded openAIResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
		return LLMResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if decoded.Error != nil {
		return LLMResponse{}, errors.New(decoded.Error.Message)
	}

	text := decoded.outputText()
	if text == "" {
		return LLMResponse{}, errors.New("response contained no output_text")
	}
	return LLMResponse{
		Text:               text,
		ProviderResponseID: decoded.ID,
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req LLMRequest) (<-chan LLMStreamEvent, error) {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan LLMStreamEvent, 2)
	ch <- LLMStreamEvent{Delta: resp.Text, ProviderResponseID: resp.ProviderResponseID}
	ch <- LLMStreamEvent{Done: true, ProviderResponseID: resp.ProviderResponseID}
	close(ch)
	return ch, nil
}

type openAIResponse struct {
	ID     string             `json:"id"`
	Status string             `json:"status"`
	Error  *openAIError       `json:"error"`
	Output []openAIOutputItem `json:"output"`
}

type openAIError struct {
	Message string `json:"message"`
}

type openAIOutputItem struct {
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	Content []openAIOutputContent `json:"content"`
}

type openAIOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (r openAIResponse) outputText() string {
	var b strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				b.WriteString(content.Text)
			}
		}
	}
	return b.String()
}
