package tool

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
)

// CallStatus is the lifecycle status of a persisted tool call record.
type CallStatus string

const (
	CallStatusRunning   CallStatus = "running"
	CallStatusSucceeded CallStatus = "succeeded"
	CallStatusFailed    CallStatus = "failed"
)

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeKey converts an idempotency key into a filesystem-safe token suitable
// for use as a file name under data/tool_calls/. A short hash of the original
// key is appended so that distinct keys never collide after sanitization.
func SanitizeKey(key string) string {
	cleaned := unsafeKeyChars.ReplaceAllString(key, "_")
	cleaned = strings.Trim(cleaned, "._-")
	if len(cleaned) > 180 {
		cleaned = cleaned[:180]
	}
	sum := sha1.Sum([]byte(key))
	suffix := hex.EncodeToString(sum[:6])
	if cleaned == "" {
		return suffix
	}
	return cleaned + "_" + suffix
}

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
