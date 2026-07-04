// Package skill implements Orbis v1 skills: reusable procedural knowledge that
// is loaded into the LLM context before planning. Skills are not tools — they
// never execute side effects. The store loads the index and all skill bodies
// into memory once so that selection can stay a pure, deterministic in-memory
// computation inside the reducer, while disk I/O is confined to load/reload.
package skill

import (
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

// Metadata is a Level 0 skill index entry: enough to find and score a skill
// without loading its body. It mirrors the shape of data/skills/index.json.
type Metadata struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags"`
	Triggers     []string `json:"triggers"`
	Path         string   `json:"path"`
	Version      string   `json:"version"`
	Status       string   `json:"status"`
	Priority     int      `json:"priority"`
	RelatedTools []string `json:"related_tools"`

	// Provenance for promoted (learned) skills; empty on curated seed skills.
	// The promoted body's content hash is recorded on the source proposal and
	// re-derived as Entry.ContentHash at load, so it is not duplicated here.
	SourceProposalID string    `json:"source_proposal_id,omitempty"`
	SourceRunID      string    `json:"source_run_id,omitempty"`
	CreatedAt        time.Time `json:"created_at,omitzero"`
}

// Entry is an in-memory skill: index metadata plus the loaded body and the
// derived content hash and character count used for budgeting and snapshots.
type Entry struct {
	Metadata
	Body        string
	ContentHash string
	Chars       int
}

// Ref converts an Entry into a domain.SkillRef snapshot.
func (e Entry) Ref() domain.SkillRef {
	return domain.SkillRef{
		ID:          e.ID,
		Name:        e.Name,
		Version:     e.Version,
		Path:        e.Path,
		ContentHash: e.ContentHash,
		Chars:       e.Chars,
	}
}

// Index exposes an immutable snapshot of the active skill set for pure
// selection. Implementations must return a copy so the reducer never observes a
// concurrent reload mid-computation.
type Index interface {
	Snapshot() []Entry
}

// Bodies resolves a skill body by id for context building at dispatch time.
type Bodies interface {
	Body(id string) (string, bool)
}
