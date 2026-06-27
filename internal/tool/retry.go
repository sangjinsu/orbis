package tool

import (
	"strings"
	"time"
)

// RetryPolicy controls how the runtime retries a failed tool call. It is a
// plain value type so the reducer can hold it as static config and compute
// retry decisions deterministically without performing any side effects.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts including the first.
	// A value <= 1 disables retries.
	MaxAttempts       int
	InitialDelay      time.Duration
	MaxDelay          time.Duration
	BackoffMultiplier float64
	// RetryableErrors is a list of substrings matched against an error message.
	// Empty means "any error is retryable".
	RetryableErrors []string
}

// DefaultRetryPolicy returns the conservative v0.2 default.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       2,
		InitialDelay:      500 * time.Millisecond,
		MaxDelay:          5 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// ShouldRetry reports whether another attempt is permitted after the given
// (1-based) attempt failed with err.
func (p RetryPolicy) ShouldRetry(failedAttempt int, err error) bool {
	if err == nil {
		return false
	}
	if failedAttempt >= p.MaxAttempts {
		return false
	}
	return p.ErrorRetryable(err.Error())
}

// ErrorRetryable reports whether an error message is considered retryable.
// Exposed separately so a worker (which has the typed error) and a reducer
// (which only has the persisted message) can agree on the same classification.
func (p RetryPolicy) ErrorRetryable(message string) bool {
	if len(p.RetryableErrors) == 0 {
		return true
	}
	for _, frag := range p.RetryableErrors {
		if frag != "" && strings.Contains(message, frag) {
			return true
		}
	}
	return false
}

// NextDelay returns the backoff delay to wait before running the given (1-based)
// attempt. Attempt 1 (the first run) has no delay; retries grow geometrically
// and are capped at MaxDelay.
func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	multiplier := p.BackoffMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	delay := p.InitialDelay
	for i := 2; i < attempt; i++ {
		delay = time.Duration(float64(delay) * multiplier)
		if p.MaxDelay > 0 && delay > p.MaxDelay {
			return p.MaxDelay
		}
	}
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}
