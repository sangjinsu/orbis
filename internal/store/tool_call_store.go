package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/sangjinsu/orbis/internal/tool"
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

func (s *FileStore) toolCallPath(idempotencyKey string) string {
	return filepath.Join(s.root, "tool_calls", tool.SanitizeKey(idempotencyKey)+".json")
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
