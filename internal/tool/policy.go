package tool

import (
	"encoding/json"
	"fmt"
	"time"
)

// ReasonCode is a stable, machine-readable code explaining a policy decision.
type ReasonCode string

const (
	ReasonOK                 ReasonCode = "ok"
	ReasonUnknownTool        ReasonCode = "unknown_tool"
	ReasonToolsetNotAllowed  ReasonCode = "toolset_not_allowed"
	ReasonSideEffectDenied   ReasonCode = "side_effect_denied"
	ReasonMissingIdempotency ReasonCode = "missing_idempotency_key"
	ReasonApprovalRequired   ReasonCode = "approval_required"
	ReasonTimeoutTooLarge    ReasonCode = "timeout_exceeds_max"
	ReasonInvalidArgs        ReasonCode = "invalid_args"
)

// Decision is the result of a policy check.
type Decision struct {
	Allowed bool
	Reason  ReasonCode
	Message string
}

// PolicyConfig controls which tools may run.
type PolicyConfig struct {
	AllowedToolsets []Toolset
	// AllowDangerous must be explicitly true to permit dangerous tools.
	// It defaults to false so dangerous tools are denied by default.
	AllowDangerous bool
	MaxTimeout     time.Duration
}

// DefaultPolicyConfig returns the safe v0.2 default: only the safe toolset,
// dangerous denied, 30s maximum timeout.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		AllowedToolsets: DefaultEnabledToolsets(),
		AllowDangerous:  false,
		MaxTimeout:      30 * time.Second,
	}
}

// CheckRequest is the input to a policy check for a single tool call.
type CheckRequest struct {
	ToolName       string
	IdempotencyKey string
	Args           json.RawMessage
	Timeout        time.Duration
}

// Policy validates tool calls against the configured rules before execution.
type Policy struct {
	registry Registry
	cfg      PolicyConfig
}

// NewPolicy builds a policy bound to a registry. A zero-value AllowedToolsets
// falls back to the safe default so the policy never silently allows everything.
func NewPolicy(registry Registry, cfg PolicyConfig) *Policy {
	if len(cfg.AllowedToolsets) == 0 {
		cfg.AllowedToolsets = DefaultEnabledToolsets()
	}
	return &Policy{registry: registry, cfg: cfg}
}

// Check evaluates a tool call. The order is intentional: existence and
// authorization failures are reported before formatting failures so the reason
// code reflects the most fundamental cause.
func (p *Policy) Check(req CheckRequest) Decision {
	t, ok := p.registry.Get(req.ToolName)
	if !ok {
		return deny(ReasonUnknownTool, fmt.Sprintf("tool %q is not registered", req.ToolName))
	}
	meta := t.Metadata()

	if !ToolsetAllowed(meta.Toolset, p.cfg.AllowedToolsets) {
		return deny(ReasonToolsetNotAllowed, fmt.Sprintf("toolset %q is not enabled", meta.Toolset))
	}
	if meta.SideEffectLevel == SideEffectDangerous && !p.cfg.AllowDangerous {
		return deny(ReasonSideEffectDenied, "dangerous tools are denied by default")
	}
	if meta.IdempotencyRequired && req.IdempotencyKey == "" {
		return deny(ReasonMissingIdempotency, fmt.Sprintf("tool %q requires an idempotency key", req.ToolName))
	}
	if meta.RequiresApproval {
		// Approval workflow is a v0.2 placeholder; until it exists, deny.
		return deny(ReasonApprovalRequired, fmt.Sprintf("tool %q requires approval", req.ToolName))
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = meta.Timeout
	}
	if p.cfg.MaxTimeout > 0 && timeout > p.cfg.MaxTimeout {
		return deny(ReasonTimeoutTooLarge, fmt.Sprintf("timeout %s exceeds max %s", timeout, p.cfg.MaxTimeout))
	}

	if len(req.Args) > 0 && !json.Valid(req.Args) {
		return deny(ReasonInvalidArgs, "args is not valid JSON")
	}

	return Decision{Allowed: true, Reason: ReasonOK}
}

func deny(reason ReasonCode, message string) Decision {
	return Decision{Allowed: false, Reason: reason, Message: message}
}
