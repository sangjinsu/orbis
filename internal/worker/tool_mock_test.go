package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMockToolRegistryExecutesEchoAndMathAdd(t *testing.T) {
	registry := NewMockToolRegistry(func() time.Time {
		return time.Unix(1700000000, 0).UTC()
	})
	ctx := context.Background()

	echo, err := registry.Execute(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("echo error = %v", err)
	}
	if string(echo) != `{"text":"hello"}` {
		t.Fatalf("echo = %s, want text hello", echo)
	}

	sum, err := registry.Execute(ctx, "math.add", json.RawMessage(`{"a":2,"b":3}`))
	if err != nil {
		t.Fatalf("math.add error = %v", err)
	}
	if string(sum) != `{"result":5}` {
		t.Fatalf("sum = %s, want result 5", sum)
	}
}

func TestMockToolRegistryFailOnceFailsOnlyFirstCall(t *testing.T) {
	registry := NewMockToolRegistry(time.Now)
	ctx := context.Background()

	if _, err := registry.Execute(ctx, "mock.fail_once", json.RawMessage(`{}`)); err == nil {
		t.Fatal("first mock.fail_once error = nil, want error")
	}
	result, err := registry.Execute(ctx, "mock.fail_once", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("second mock.fail_once error = %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s, want ok true", result)
	}
}

func TestMockToolRegistryTimeNowUsesInjectedClock(t *testing.T) {
	registry := NewMockToolRegistry(func() time.Time {
		return time.Unix(1700000000, 0).UTC()
	})
	result, err := registry.Execute(context.Background(), "time.now", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("time.now error = %v", err)
	}
	if string(result) != `{"time":"2023-11-14T22:13:20Z"}` {
		t.Fatalf("time.now = %s, want fixed timestamp", result)
	}
}
