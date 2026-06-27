package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
)

// ToolRequest is a single tool execution request handed to the Tool Worker by
// the dispatcher. Attempt and MaxAttempts/Timeout are decided by the reducer
// (from static config) so the worker stays free of policy bookkeeping.
type ToolRequest struct {
	SessionID      string
	RunID          string
	ToolCallID     string
	ToolName       string
	Args           json.RawMessage
	IdempotencyKey string
	Attempt        int
	MaxAttempts    int
	Timeout        time.Duration
}

// ToolOutcomeStatus is the high-level result class of a tool execution.
type ToolOutcomeStatus string

const (
	ToolOutcomeSucceeded    ToolOutcomeStatus = "succeeded"
	ToolOutcomeFailed       ToolOutcomeStatus = "failed"
	ToolOutcomeRejected     ToolOutcomeStatus = "rejected"
	ToolOutcomeDeduplicated ToolOutcomeStatus = "deduplicated"
)

// ToolOutcome is the structured result the dispatcher turns into events. The
// worker never emits events itself; it only reports what happened.
//
// Retryable describes the error class only (is this kind of error worth
// retrying); the reducer combines it with Attempt/MaxAttempts to make the final
// retry decision.
type ToolOutcome struct {
	Status     ToolOutcomeStatus
	Result     json.RawMessage
	ReasonCode string
	Error      string
	Retryable  bool
	TimedOut   bool
	DurationMS int64
}

// ToolWorkerConfig configures a ToolWorker.
type ToolWorkerConfig struct {
	Registry       tool.Registry
	Policy         *tool.Policy
	Store          store.ToolCallStore
	DefaultTimeout time.Duration
	RetryPolicy    tool.RetryPolicy
	Now            func() time.Time
}

// ToolWorker is the only component that executes tools. It enforces policy,
// deduplicates already-succeeded calls, applies a timeout, and persists a
// record for every executed call.
type ToolWorker struct {
	registry       tool.Registry
	policy         *tool.Policy
	store          store.ToolCallStore
	defaultTimeout time.Duration
	retryPolicy    tool.RetryPolicy
	now            func() time.Time
}

func NewToolWorker(cfg ToolWorkerConfig) *ToolWorker {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	policy := cfg.Policy
	if policy == nil {
		policy = tool.NewPolicy(cfg.Registry, tool.DefaultPolicyConfig())
	}
	retryPolicy := cfg.RetryPolicy
	if retryPolicy.MaxAttempts == 0 {
		retryPolicy = tool.DefaultRetryPolicy()
	}
	defaultTimeout := cfg.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 5 * time.Second
	}
	return &ToolWorker{
		registry:       cfg.Registry,
		policy:         policy,
		store:          cfg.Store,
		defaultTimeout: defaultTimeout,
		retryPolicy:    retryPolicy,
		now:            now,
	}
}

// Run executes a tool call and returns a structured outcome. It must be the
// only path through which tools execute.
func (w *ToolWorker) Run(ctx context.Context, req ToolRequest) ToolOutcome {
	attempt := req.Attempt
	if attempt < 1 {
		attempt = 1
	}

	decision := w.policy.Check(tool.CheckRequest{
		ToolName:       req.ToolName,
		IdempotencyKey: req.IdempotencyKey,
		Args:           req.Args,
		Timeout:        req.Timeout,
	})
	if !decision.Allowed {
		return ToolOutcome{
			Status:     ToolOutcomeRejected,
			ReasonCode: string(decision.Reason),
			Error:      decision.Message,
		}
	}

	// Deduplicate only when a previous attempt already succeeded.
	if req.IdempotencyKey != "" && w.store != nil {
		if rec, err := w.store.LoadToolCall(ctx, req.IdempotencyKey); err == nil {
			if rec.Status == string(tool.CallStatusSucceeded) {
				return ToolOutcome{Status: ToolOutcomeDeduplicated, Result: compactResult(rec.Result)}
			}
		}
	}

	t, ok := w.registry.Get(req.ToolName)
	if !ok {
		return ToolOutcome{
			Status:     ToolOutcomeRejected,
			ReasonCode: string(tool.ReasonUnknownTool),
			Error:      "unknown tool",
		}
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = t.Metadata().Timeout
	}
	if timeout <= 0 {
		timeout = w.defaultTimeout
	}

	w.persist(req, string(tool.CallStatusRunning), attempt, nil, "")

	execCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	execCtx = tool.ContextWithAttempt(execCtx, attempt)

	start := w.now()
	result, err := t.Execute(execCtx, req.Args)
	durationMS := w.now().Sub(start).Milliseconds()

	if err != nil {
		outcome := ToolOutcome{Status: ToolOutcomeFailed, Error: err.Error(), DurationMS: durationMS}
		switch {
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			outcome.TimedOut = true
			outcome.ReasonCode = "timeout"
			outcome.Retryable = w.retryPolicy.ErrorRetryable("timeout")
		case errors.Is(err, context.Canceled) || ctx.Err() != nil:
			outcome.ReasonCode = "cancelled"
			outcome.Retryable = false
		default:
			outcome.ReasonCode = "error"
			outcome.Retryable = w.retryPolicy.ErrorRetryable(err.Error())
		}
		w.persist(req, string(tool.CallStatusFailed), attempt, nil, err.Error())
		return outcome
	}

	w.persist(req, string(tool.CallStatusSucceeded), attempt, result, "")
	return ToolOutcome{Status: ToolOutcomeSucceeded, Result: result, DurationMS: durationMS}
}

// persist writes the tool call record using a detached context so the record
// survives run cancellation or timeout.
func (w *ToolWorker) persist(req ToolRequest, status string, attempt int, result json.RawMessage, errMsg string) {
	if w.store == nil || req.IdempotencyKey == "" {
		return
	}
	bg := context.Background()
	now := w.now()
	record := store.ToolCallRecord{
		IdempotencyKey: req.IdempotencyKey,
		SessionID:      req.SessionID,
		RunID:          req.RunID,
		ToolCallID:     req.ToolCallID,
		ToolName:       req.ToolName,
		Status:         status,
		Attempts:       attempt,
		Result:         result,
		Error:          errMsg,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if existing, err := w.store.LoadToolCall(bg, req.IdempotencyKey); err == nil {
		record.CreatedAt = existing.CreatedAt
	}
	_ = w.store.SaveToolCall(bg, record)
}

// compactResult removes any insignificant whitespace a persisted result may
// have picked up from indented snapshot storage, so deduplicated results match
// the compact bytes a fresh execution returns.
func compactResult(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return json.RawMessage(buf.Bytes())
}
