package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Audit actors. Actor identifies who drove a lifecycle transition; it is never
// a secret or a reasoning trace.
const (
	ActorSystem    = "system"
	ActorAdmin     = "admin"
	ActorDeveloper = "developer"
	ActorUnknown   = "unknown"
)

// AuditRecord is one line of the skill-learning audit trail. Summary is a
// short, user-visible sentence; secrets and hidden chain-of-thought are never
// stored here.
type AuditRecord struct {
	AuditID     string    `json:"audit_id"`
	EventType   string    `json:"event_type"`
	ProposalID  string    `json:"proposal_id,omitempty"`
	SkillID     string    `json:"skill_id,omitempty"`
	SourceRunID string    `json:"source_run_id,omitempty"`
	Actor       string    `json:"actor"`
	Timestamp   time.Time `json:"timestamp"`
	Summary     string    `json:"summary,omitempty"`
}

// AuditLog appends skill-learning audit records to a JSONL file
// (e.g. data/audit/skill_audit.jsonl). Appends are serialized so concurrent
// lifecycle operations never interleave partial lines.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Append writes one record as a single JSON line, creating the parent
// directory on first use.
func (l *AuditLog) Append(record AuditRecord) error {
	if record.AuditID == "" {
		return fmt.Errorf("audit_id is required")
	}
	if record.EventType == "" {
		return fmt.Errorf("audit %q: event_type is required", record.AuditID)
	}
	if record.Actor == "" {
		record.Actor = ActorUnknown
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append audit record: %w", err)
	}
	return nil
}
