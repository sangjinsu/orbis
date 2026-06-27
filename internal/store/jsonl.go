package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sangjinsu/orbis/internal/domain"
)

type FileStore struct {
	root string
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: root}
}

func (s *FileStore) Root() string {
	return s.root
}

func (s *FileStore) AppendEvent(ctx context.Context, event domain.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(s.root, "events", event.SessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create events dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer file.Close()

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *FileStore) LoadSession(ctx context.Context, sessionID string) (domain.SessionState, error) {
	if err := ctx.Err(); err != nil {
		return domain.SessionState{}, err
	}
	var state domain.SessionState
	if err := readJSON(filepath.Join(s.root, "sessions", sessionID+".json"), &state); err != nil {
		return domain.SessionState{}, err
	}
	return state, nil
}

func (s *FileStore) SaveSession(ctx context.Context, state domain.SessionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.root, "sessions", state.SessionID+".json"), state)
}

func (s *FileStore) LoadRun(ctx context.Context, runID string) (domain.RunState, error) {
	if err := ctx.Err(); err != nil {
		return domain.RunState{}, err
	}
	var state domain.RunState
	if err := readJSON(filepath.Join(s.root, "runs", runID+".json"), &state); err != nil {
		return domain.RunState{}, err
	}
	return state, nil
}

func (s *FileStore) SaveRun(ctx context.Context, state domain.RunState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.root, "runs", state.RunID+".json"), state)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	return nil
}
