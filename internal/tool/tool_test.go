package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) Registry {
	t.Helper()
	r := NewRegistry()
	if err := RegisterMockTools(r, func() time.Time { return time.Unix(1700000000, 0).UTC() }); err != nil {
		t.Fatalf("RegisterMockTools error = %v", err)
	}
	return r
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := newTestRegistry(t)
	if _, ok := r.Get("echo"); !ok {
		t.Fatal("echo not found after registration")
	}
	if _, ok := r.Get("does.not.exist"); ok {
		t.Fatal("unexpected tool found")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(echoTool{}); err != nil {
		t.Fatalf("first register error = %v", err)
	}
	err := r.Register(echoTool{})
	if !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("duplicate register error = %v, want ErrDuplicateTool", err)
	}
}

func TestRegistryListByToolsetAndSchemas(t *testing.T) {
	r := newTestRegistry(t)
	safe := r.ListByToolset([]Toolset{ToolsetSafe})
	for _, tl := range safe {
		if tl.Metadata().Toolset != ToolsetSafe {
			t.Fatalf("ListByToolset returned non-safe tool %q", tl.Name())
		}
		if tl.Name() == "mock.dangerous" {
			t.Fatal("dangerous tool leaked into safe toolset")
		}
	}
	schemas := r.SchemasForLLM([]Toolset{ToolsetSafe})
	if len(schemas) != len(safe) {
		t.Fatalf("SchemasForLLM count = %d, want %d", len(schemas), len(safe))
	}
	for _, s := range schemas {
		if s.Name == "" || !json.Valid(s.Parameters) {
			t.Fatalf("invalid schema %+v", s)
		}
	}
}

func TestPolicyAllowsSafeTool(t *testing.T) {
	p := NewPolicy(newTestRegistry(t), DefaultPolicyConfig())
	d := p.Check(CheckRequest{ToolName: "math.add", Args: json.RawMessage(`{"a":1,"b":2}`)})
	if !d.Allowed {
		t.Fatalf("math.add denied: %s (%s)", d.Reason, d.Message)
	}
}

func TestPolicyDecisions(t *testing.T) {
	p := NewPolicy(newTestRegistry(t), DefaultPolicyConfig())
	tests := []struct {
		name string
		req  CheckRequest
		want ReasonCode
	}{
		{"unknown", CheckRequest{ToolName: "nope"}, ReasonUnknownTool},
		{"dangerous", CheckRequest{ToolName: "mock.dangerous"}, ReasonToolsetNotAllowed},
		{"missing idempotency", CheckRequest{ToolName: "mock.fail_once"}, ReasonMissingIdempotency},
		{"timeout too large", CheckRequest{ToolName: "echo", Timeout: time.Minute, Args: json.RawMessage(`{"text":"hi"}`)}, ReasonTimeoutTooLarge},
		{"invalid args", CheckRequest{ToolName: "echo", Args: json.RawMessage(`{not json`)}, ReasonInvalidArgs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := p.Check(tc.req)
			if d.Allowed {
				t.Fatalf("request unexpectedly allowed")
			}
			if d.Reason != tc.want {
				t.Fatalf("reason = %s, want %s", d.Reason, tc.want)
			}
		})
	}
}

func TestPolicyAllowDangerousOptIn(t *testing.T) {
	cfg := DefaultPolicyConfig()
	cfg.AllowedToolsets = []Toolset{ToolsetSafe, ToolsetDangerous}
	cfg.AllowDangerous = true
	p := NewPolicy(newTestRegistry(t), cfg)
	d := p.Check(CheckRequest{ToolName: "mock.dangerous"})
	if !d.Allowed {
		t.Fatalf("dangerous tool denied with opt-in: %s", d.Reason)
	}
}

func TestRetryShouldRetryAndDelay(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 3, InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second, BackoffMultiplier: 2}
	if !p.ShouldRetry(1, errors.New("boom")) {
		t.Fatal("attempt 1 should retry")
	}
	if p.ShouldRetry(3, errors.New("boom")) {
		t.Fatal("attempt 3 should not retry (max reached)")
	}
	if p.ShouldRetry(1, nil) {
		t.Fatal("nil error should not retry")
	}
	if got := p.NextDelay(1); got != 0 {
		t.Fatalf("NextDelay(1) = %v, want 0", got)
	}
	if got := p.NextDelay(2); got != 100*time.Millisecond {
		t.Fatalf("NextDelay(2) = %v, want 100ms", got)
	}
	if got := p.NextDelay(3); got != 200*time.Millisecond {
		t.Fatalf("NextDelay(3) = %v, want 200ms", got)
	}
	if got := p.NextDelay(10); got != time.Second {
		t.Fatalf("NextDelay(10) = %v, want capped 1s", got)
	}
}

func TestRetryErrorRetryableFilter(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 2, RetryableErrors: []string{"timeout"}}
	if !p.ErrorRetryable("context deadline timeout") {
		t.Fatal("timeout error should be retryable")
	}
	if p.ErrorRetryable("permission denied") {
		t.Fatal("non-matching error should not be retryable")
	}
}

func TestMockToolsExecute(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	echo, _ := r.Get("echo")
	out, err := echo.Execute(ctx, json.RawMessage(`{"text":"hi"}`))
	if err != nil || string(out) != `{"text":"hi"}` {
		t.Fatalf("echo = %s, err = %v", out, err)
	}

	add, _ := r.Get("math.add")
	out, err = add.Execute(ctx, json.RawMessage(`{"a":2,"b":3}`))
	if err != nil || string(out) != `{"result":5}` {
		t.Fatalf("math.add = %s, err = %v", out, err)
	}

	now, _ := r.Get("time.now")
	out, err = now.Execute(ctx, json.RawMessage(`{}`))
	if err != nil || string(out) != `{"time":"2023-11-14T22:13:20Z"}` {
		t.Fatalf("time.now = %s, err = %v", out, err)
	}
}

func TestMockFailOnceUsesAttempt(t *testing.T) {
	r := newTestRegistry(t)
	tool, _ := r.Get("mock.fail_once")

	if _, err := tool.Execute(ContextWithAttempt(context.Background(), 1), json.RawMessage(`{}`)); err == nil {
		t.Fatal("attempt 1 should fail")
	}
	out, err := tool.Execute(ContextWithAttempt(context.Background(), 2), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("attempt 2 error = %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("attempt 2 = %s, want ok", out)
	}
}

func TestMockSleepRespectsCancellation(t *testing.T) {
	r := newTestRegistry(t)
	tool, _ := r.Get("mock.sleep")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := tool.Execute(ctx, json.RawMessage(`{"duration":"5s"}`))
	if err == nil {
		t.Fatal("sleep should be cancelled by context")
	}
	if time.Since(start) > time.Second {
		t.Fatal("sleep did not return promptly on cancellation")
	}
}
