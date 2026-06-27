package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Tool interface {
	Name() string
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

type MockToolRegistry struct {
	now            func() time.Time
	failOnceFailed bool
}

func NewMockToolRegistry(now func() time.Time) *MockToolRegistry {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MockToolRegistry{now: now}
}

func (r *MockToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch name {
	case "echo":
		return executeEcho(args)
	case "time.now":
		return json.Marshal(map[string]string{"time": r.now().UTC().Format(time.RFC3339)})
	case "math.add":
		return executeMathAdd(args)
	case "mock.fail_once":
		if !r.failOnceFailed {
			r.failOnceFailed = true
			return nil, errors.New("mock fail once")
		}
		return json.Marshal(map[string]bool{"ok": true})
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func executeEcho(args json.RawMessage) (json.RawMessage, error) {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, fmt.Errorf("decode echo args: %w", err)
	}
	return json.Marshal(payload)
}

func executeMathAdd(args json.RawMessage) (json.RawMessage, error) {
	var payload struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, fmt.Errorf("decode math.add args: %w", err)
	}
	return json.Marshal(map[string]float64{"result": payload.A + payload.B})
}
