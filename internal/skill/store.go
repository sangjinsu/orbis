package skill

import (
	"path/filepath"
	"sort"
	"sync"
)

// Store is a file-based skill store. It loads the index and all skill bodies
// into memory once (and again on Reload) so the reducer can perform pure
// in-memory selection and the dispatcher can build context without disk I/O.
// It implements both Index and Bodies.
type Store struct {
	dir string

	mu      sync.RWMutex
	entries []Entry
	byID    map[string]Entry
}

// NewStore creates a store rooted at dir (e.g. data/skills) and loads it once.
func NewStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload re-reads the index and all bodies from disk and atomically swaps them
// in. On any error the previous in-memory state is left untouched.
func (s *Store) Reload() error {
	metas, err := parseIndex(filepath.Join(s.dir, "index.json"))
	if err != nil {
		return err
	}
	entries := make([]Entry, 0, len(metas))
	byID := make(map[string]Entry, len(metas))
	for _, m := range metas {
		entry, err := loadEntry(s.dir, m)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		byID[entry.ID] = entry
	}
	s.mu.Lock()
	s.entries = entries
	s.byID = byID
	s.mu.Unlock()
	return nil
}

// Snapshot returns an immutable copy of the active entries for pure selection.
func (s *Store) Snapshot() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Body returns the body text for a skill id.
func (s *Store) Body(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.byID[id]
	if !ok {
		return "", false
	}
	return entry.Body, true
}

// List returns metadata for all active skills, ordered by priority (desc) then
// id (asc) for a stable API response.
func (s *Store) List() []Metadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Metadata, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.Metadata)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Get returns a single skill (metadata + body) by id.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.byID[id]
	return entry, ok
}
