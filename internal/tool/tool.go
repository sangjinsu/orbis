// Package tool defines the runtime's first-class tool-calling primitives:
// the Tool interface, schema/metadata describing each tool, the registry,
// execution policy, retry policy, and idempotency helpers.
//
// This package is a dependency leaf: it imports only the standard library so
// that both the worker (tool execution) and runtime (reducer policy injection)
// can depend on it without creating an import cycle.
package tool

import (
	"context"
	"encoding/json"
	"time"
)

// SideEffectLevel describes how impactful executing a tool is. Policy uses it
// to decide whether a tool may run under the current configuration.
type SideEffectLevel string

const (
	SideEffectNone      SideEffectLevel = "none"
	SideEffectRead      SideEffectLevel = "read"
	SideEffectWrite     SideEffectLevel = "write"
	SideEffectNetwork   SideEffectLevel = "network"
	SideEffectDangerous SideEffectLevel = "dangerous"
)

// ToolSchema is the LLM-facing contract for a tool. Parameters and Output are
// JSON Schema documents so the schema can be forwarded to an LLM provider's
// function/tool-calling API.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Output      json.RawMessage `json:"output,omitempty"`
}

// ToolMetadata captures the runtime policy attributes for a tool.
type ToolMetadata struct {
	SideEffectLevel     SideEffectLevel
	Toolset             Toolset
	Timeout             time.Duration
	RetryPolicy         RetryPolicy
	RequiresApproval    bool
	IdempotencyRequired bool
}

// Tool is a unit of side-effecting work the runtime can dispatch on behalf of
// an LLM tool-call proposal.
//
// Invariant: tools never run inside reducers or WebSocket handlers. Only the
// Tool Worker calls Execute, and it must respect context cancellation/timeout.
type Tool interface {
	Name() string
	Description() string
	Schema() ToolSchema
	Metadata() ToolMetadata
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}
