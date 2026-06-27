package store

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ToolCallRecord is the persisted state of a single tool call, keyed by its
// idempotency key. It lets the runtime deduplicate already-succeeded calls and
// makes tool execution inspectable under data/tool_calls/.
type ToolCallRecord struct {
	IdempotencyKey string          `json:"idempotency_key"`
	SessionID      string          `json:"session_id"`
	RunID          string          `json:"run_id"`
	ToolCallID     string          `json:"tool_call_id"`
	ToolName       string          `json:"tool_name"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ToolCallStore persists and retrieves tool call records by idempotency key.
type ToolCallStore interface {
	LoadToolCall(ctx context.Context, idempotencyKey string) (ToolCallRecord, error)
	SaveToolCall(ctx context.Context, record ToolCallRecord) error
}

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeKey converts an idempotency key into a filesystem-safe token for use
// as a file name under data/tool_calls/. A short hash of the original key is
// appended so that distinct keys never collide after sanitization.
func sanitizeKey(key string) string {
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

func (s *FileStore) toolCallPath(idempotencyKey string) string {
	return filepath.Join(s.root, "tool_calls", sanitizeKey(idempotencyKey)+".json")
}

func (s *FileStore) LoadToolCall(ctx context.Context, idempotencyKey string) (ToolCallRecord, error) {
	if err := ctx.Err(); err != nil {
		return ToolCallRecord{}, err
	}
	var record ToolCallRecord
	if err := readJSON(s.toolCallPath(idempotencyKey), &record); err != nil {
		return ToolCallRecord{}, err
	}
	return record, nil
}

func (s *FileStore) SaveToolCall(ctx context.Context, record ToolCallRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeJSON(s.toolCallPath(record.IdempotencyKey), record)
}
