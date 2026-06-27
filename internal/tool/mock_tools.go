package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RegisterMockTools registers the v0.2 mock/safe tool set into r. The now clock
// is injected so time.now is deterministic in tests.
func RegisterMockTools(r Registry, now func() time.Time) error {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	tools := []Tool{
		echoTool{},
		timeNowTool{now: now},
		mathAddTool{},
		failOnceTool{},
		sleepTool{},
		dangerousTool{},
	}
	for _, t := range tools {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func safeMeta(level SideEffectLevel) ToolMetadata {
	return ToolMetadata{
		SideEffectLevel: level,
		Toolset:         ToolsetSafe,
		RetryPolicy:     DefaultRetryPolicy(),
	}
}

func schemaObject(properties, required string) json.RawMessage {
	if properties == "" {
		properties = "{}"
	}
	req := ""
	if required != "" {
		req = `,"required":` + required
	}
	return json.RawMessage(`{"type":"object","properties":` + properties + req + `}`)
}

// echo ------------------------------------------------------------------------

type echoTool struct{}

func (echoTool) Name() string           { return "echo" }
func (echoTool) Description() string    { return "Echo the provided text back to the caller." }
func (echoTool) Metadata() ToolMetadata { return safeMeta(SideEffectNone) }
func (echoTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "echo",
		Description: "Echo the provided text back to the caller.",
		Parameters:  schemaObject(`{"text":{"type":"string"}}`, `["text"]`),
	}
}
func (echoTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, fmt.Errorf("decode echo args: %w", err)
	}
	return json.Marshal(payload)
}

// time.now --------------------------------------------------------------------

type timeNowTool struct {
	now func() time.Time
}

func (timeNowTool) Name() string           { return "time.now" }
func (timeNowTool) Description() string    { return "Return the current server time in RFC3339." }
func (timeNowTool) Metadata() ToolMetadata { return safeMeta(SideEffectRead) }
func (timeNowTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "time.now",
		Description: "Return the current server time in RFC3339.",
		Parameters:  schemaObject("{}", ""),
	}
}
func (t timeNowTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"time": t.now().UTC().Format(time.RFC3339)})
}

// math.add --------------------------------------------------------------------

type mathAddTool struct{}

func (mathAddTool) Name() string           { return "math.add" }
func (mathAddTool) Description() string    { return "Return the sum of two numbers a and b." }
func (mathAddTool) Metadata() ToolMetadata { return safeMeta(SideEffectNone) }
func (mathAddTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "math.add",
		Description: "Return the sum of two numbers a and b.",
		Parameters:  schemaObject(`{"a":{"type":"number"},"b":{"type":"number"}}`, `["a","b"]`),
	}
}
func (mathAddTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var payload struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, fmt.Errorf("decode math.add args: %w", err)
	}
	return json.Marshal(map[string]float64{"result": payload.A + payload.B})
}

// mock.fail_once --------------------------------------------------------------

type failOnceTool struct{}

func (failOnceTool) Name() string        { return "mock.fail_once" }
func (failOnceTool) Description() string { return "Fail on the first attempt, succeed on retry." }
func (failOnceTool) Metadata() ToolMetadata {
	meta := safeMeta(SideEffectNone)
	meta.IdempotencyRequired = true
	return meta
}
func (failOnceTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "mock.fail_once",
		Description: "Fail on the first attempt and succeed when retried.",
		Parameters:  schemaObject("{}", ""),
	}
}
func (failOnceTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if AttemptFromContext(ctx) <= 1 {
		return nil, errors.New("mock fail once: transient failure on first attempt")
	}
	return json.Marshal(map[string]bool{"ok": true})
}

// mock.sleep ------------------------------------------------------------------

type sleepTool struct{}

func (sleepTool) Name() string { return "mock.sleep" }
func (sleepTool) Description() string {
	return "Sleep for the requested Go duration, honoring cancellation."
}
func (sleepTool) Metadata() ToolMetadata { return safeMeta(SideEffectNone) }
func (sleepTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "mock.sleep",
		Description: "Sleep for the requested Go duration (e.g. \"5s\"), honoring cancellation.",
		Parameters:  schemaObject(`{"duration":{"type":"string","description":"Go duration such as 5s"}}`, `["duration"]`),
	}
}
func (sleepTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var payload struct {
		Duration string `json:"duration"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, fmt.Errorf("decode mock.sleep args: %w", err)
		}
	}
	d, err := time.ParseDuration(payload.Duration)
	if err != nil {
		return nil, fmt.Errorf("parse mock.sleep duration: %w", err)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return json.Marshal(map[string]string{"slept": payload.Duration})
	}
}

// mock.dangerous --------------------------------------------------------------
// Registered but denied by default so policy denial can be tested distinctly
// from an unknown tool.

type dangerousTool struct{}

func (dangerousTool) Name() string        { return "mock.dangerous" }
func (dangerousTool) Description() string { return "A dangerous tool that policy denies by default." }
func (dangerousTool) Metadata() ToolMetadata {
	return ToolMetadata{
		SideEffectLevel: SideEffectDangerous,
		Toolset:         ToolsetDangerous,
		RetryPolicy:     DefaultRetryPolicy(),
	}
}
func (dangerousTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "mock.dangerous",
		Description: "A dangerous tool that policy denies by default.",
		Parameters:  schemaObject("{}", ""),
	}
}
func (dangerousTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("dangerous tool must never execute in v0.2")
}
