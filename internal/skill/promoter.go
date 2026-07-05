package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// learnedSkillPriority ranks promoted skills below the curated seed skills so a
// learned skill never outranks hand-written guidance on score ties.
const learnedSkillPriority = 50

// validSkillID keeps promoted skill ids safe as file names and index keys.
var validSkillID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Promoter writes an approved proposal into the active skill directory: the
// markdown body as {skill_id}.md plus an index.json entry carrying version,
// provenance, and active status. Promotion is the only write path from the
// learning loop into data/skills and is never invoked without an explicit
// approval — the proposal lifecycle has no pending->promoted transition.
// Re-promoting an id that already exists bumps the entry's version in place
// when it has learned provenance, archiving the previous body; an id without
// provenance is a curated seed and always conflicts (ErrSkillConflict).
type Promoter struct {
	dir string
}

// NewPromoter creates a promoter rooted at the skills directory (data/skills).
func NewPromoter(dir string) *Promoter {
	return &Promoter{dir: dir}
}

func (p *Promoter) indexPath() string {
	return filepath.Join(p.dir, "index.json")
}

// archiveBody preserves the current body of a learned skill as
// archive/{id}@{version}.md before a new version replaces it. The '@' is
// outside validSkillID's charset so archived files can never collide with an
// active skill body, and the loader only reads paths from the index, so
// archive/ stays inert. A missing body (orphaned index entry) archives
// nothing; re-archiving the same version overwrites the identical content.
func (p *Promoter) archiveBody(existing Metadata) error {
	current, err := os.ReadFile(filepath.Join(p.dir, existing.Path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read body for archive: %w", err)
	}
	archiveDir := filepath.Join(p.dir, "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}
	name := fmt.Sprintf("%s@%s.md", existing.ID, existing.Version)
	if err := os.WriteFile(filepath.Join(archiveDir, name), current, 0o644); err != nil {
		return fmt.Errorf("archive previous skill body: %w", err)
	}
	return nil
}

// loadIndex reads the current index, treating a missing file as empty so a
// fresh skills directory is bootstrapped by the first promotion.
func (p *Promoter) loadIndex() ([]Metadata, error) {
	metas, err := parseIndex(p.indexPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Metadata{}, nil
		}
		return nil, err
	}
	return metas, nil
}

// Promote writes the proposal body and index entry and returns the promoted
// metadata. The caller owns the proposal status transition, audit records,
// lifecycle events, and the in-memory index reload.
func (p *Promoter) Promote(proposal SkillProposal, now time.Time) (Metadata, error) {
	if err := proposal.Validate(); err != nil {
		return Metadata{}, err
	}
	if !validSkillID.MatchString(proposal.SkillID) {
		return Metadata{}, fmt.Errorf("invalid skill id %q", proposal.SkillID)
	}

	metas, err := p.loadIndex()
	if err != nil {
		return Metadata{}, err
	}
	existingIdx := -1
	for i, m := range metas {
		if m.ID == proposal.SkillID {
			if m.SourceProposalID == "" {
				// No learned provenance: a curated seed, never replaced.
				return Metadata{}, fmt.Errorf("%w: %s", ErrSkillConflict, proposal.SkillID)
			}
			existingIdx = i
			break
		}
	}

	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return Metadata{}, fmt.Errorf("create skills dir: %w", err)
	}

	var meta Metadata
	bodyPath := proposal.SkillID + ".md"
	if existingIdx >= 0 {
		existing := metas[existingIdx]
		version, err := nextVersion(existing.Version)
		if err != nil {
			return Metadata{}, fmt.Errorf("bump version of %s: %w", existing.ID, err)
		}
		bodyPath = existing.Path
		if err := p.archiveBody(existing); err != nil {
			return Metadata{}, err
		}
		// The entry keeps its identity, path, tags, and curation fields; only
		// the content-bearing fields describe the newly promoted version.
		existing.Title = proposal.Title
		existing.Description = proposal.Purpose
		existing.Version = version
		existing.RelatedTools = proposal.RelatedTools
		existing.SourceProposalID = proposal.ProposalID
		existing.SourceRunID = proposal.SourceRunID
		existing.CreatedAt = now
		meta = existing
	} else {
		meta = Metadata{
			ID:               proposal.SkillID,
			Name:             proposal.SkillID,
			Title:            proposal.Title,
			Description:      proposal.Purpose,
			Tags:             []string{"learned"},
			Path:             bodyPath,
			Version:          normalizeVersion(proposal.Version),
			Status:           "active",
			Priority:         learnedSkillPriority,
			RelatedTools:     proposal.RelatedTools,
			SourceProposalID: proposal.ProposalID,
			SourceRunID:      proposal.SourceRunID,
			CreatedAt:        now,
		}
	}

	// Body first, index second: a failed index write leaves only a harmless
	// orphan .md file, while the index (the load source of truth) stays intact.
	// An orphan left by such a failure is overwritten by the retry.
	if err := os.WriteFile(filepath.Join(p.dir, bodyPath), []byte(proposal.Body), 0o644); err != nil {
		return Metadata{}, fmt.Errorf("write promoted skill body: %w", err)
	}

	if existingIdx >= 0 {
		metas[existingIdx] = meta
	} else {
		metas = append(metas, meta)
	}
	data, err := json.MarshalIndent(indexFile{Skills: metas}, "", "  ")
	if err != nil {
		return Metadata{}, fmt.Errorf("marshal skill index: %w", err)
	}
	if err := os.WriteFile(p.indexPath(), append(data, '\n'), 0o644); err != nil {
		return Metadata{}, fmt.Errorf("write skill index: %w", err)
	}
	return meta, nil
}
