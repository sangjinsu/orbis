package tool

import "context"

// CallStatus is the lifecycle status of a persisted tool call record.
type CallStatus string

const (
	CallStatusRunning   CallStatus = "running"
	CallStatusSucceeded CallStatus = "succeeded"
	CallStatusFailed    CallStatus = "failed"
)

type attemptContextKey struct{}

// ContextWithAttempt attaches the current (1-based) attempt number to ctx so a
// tool can vary behavior across retries (e.g. mock.fail_once).
func ContextWithAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptContextKey{}, attempt)
}

// AttemptFromContext returns the attempt number set by ContextWithAttempt, or 1
// when none is present.
func AttemptFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(attemptContextKey{}).(int); ok && v > 0 {
		return v
	}
	return 1
}
