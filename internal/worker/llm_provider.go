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
		"store": false,
	}
	if len(req.Messages) > 0 {
		body["input"] = buildResponsesInput(req.Messages)
	} else {
		body["input"] = req.Input
	}
	if len(req.Tools) > 0 {
		body["tools"] = buildResponsesTools(req.Tools)
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
	toolCall := decoded.functionCall()
	if toolCall != nil {
		// Map the sanitized wire name back to the registered tool name so the
		// reducer dispatches the real tool.
		toolCall.Name = originalToolName(req.Tools, toolCall.Name)
	}
	if text == "" && toolCall == nil {
		return LLMResponse{}, errors.New("response contained no output_text or tool call")
	}
	return LLMResponse{
		Text:               text,
		ProviderResponseID: decoded.ID,
		ToolCall:           toolCall,
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req LLMRequest) (<-chan LLMStreamEvent, error) {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan LLMStreamEvent, 2)
	if resp.Text != "" {
		ch <- LLMStreamEvent{Delta: resp.Text, ProviderResponseID: resp.ProviderResponseID}
	}
	ch <- LLMStreamEvent{Done: true, ProviderResponseID: resp.ProviderResponseID, ToolCall: resp.ToolCall}
	close(ch)
	return ch, nil
}

// buildResponsesInput converts conversation messages into Responses API input
// items, reconstructing assistant function_call turns and function_call_output
// tool results from the message linkage fields.
func buildResponsesInput(messages []LLMMessage) []map[string]any {
	items := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		switch {
		case m.Role == "tool" && m.ToolCallID != "":
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		case m.Role == "assistant" && m.ToolCallID != "":
			args := strings.TrimSpace(string(m.ToolArgs))
			if args == "" {
				args = "{}"
			}
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   m.ToolCallID,
				"name":      sanitizeToolName(m.ToolName),
				"arguments": args,
			})
		default:
			role := m.Role
			if role == "" {
				role = "user"
			}
			items = append(items, map[string]any{"role": role, "content": m.Content})
		}
	}
	return items
}

// buildResponsesTools maps tool schemas to flattened Responses API function
// tool definitions ({"type":"function","name","description","parameters"}).
func buildResponsesTools(tools []tool.ToolSchema) []map[string]any {
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		parameters := t.Parameters
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		defs = append(defs, map[string]any{
			"type":        "function",
			"name":        sanitizeToolName(t.Name),
			"description": t.Description,
			"parameters":  parameters,
		})
	}
	return defs
}

// sanitizeToolName rewrites a tool name to satisfy the OpenAI Responses API
// function-name pattern ^[a-zA-Z0-9_-]+$. Dotted registry names like "math.add"
// become "math_add". Only the wire representation is sanitized; the runtime keeps
// the original registry name and responses are mapped back via originalToolName.
func sanitizeToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// originalToolName maps a sanitized function name from a provider response back to
// the registered tool name, so the reducer dispatches by the real name. It falls
// back to the provider-returned name when no advertised tool matches.
func originalToolName(tools []tool.ToolSchema, sanitized string) string {
	for _, t := range tools {
		if sanitizeToolName(t.Name) == sanitized {
			return t.Name
		}
	}
	return sanitized
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
	Type      string                `json:"type"`
	Role      string                `json:"role"`
	Content   []openAIOutputContent `json:"content"`
	CallID    string                `json:"call_id"`
	Name      string                `json:"name"`
	Arguments string                `json:"arguments"`
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

// functionCall returns the first function_call item in the response output, if
// the model chose to call a tool.
func (r openAIResponse) functionCall() *ToolCall {
	for _, item := range r.Output {
		if item.Type != "function_call" {
			continue
		}
		args := strings.TrimSpace(item.Arguments)
		if args == "" {
			args = "{}"
		}
		return &ToolCall{
			ToolCallID: item.CallID,
			Name:       item.Name,
			Args:       json.RawMessage(args),
		}
	}
	return nil
}
