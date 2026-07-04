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
// learning loop into data/skills, it always rejects an id that already exists
// (ErrSkillConflict), and it is never invoked without an explicit approval —
// the proposal lifecycle has no pending->promoted transition.
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
	for _, m := range metas {
		if m.ID == proposal.SkillID {
			return Metadata{}, fmt.Errorf("%w: %s", ErrSkillConflict, proposal.SkillID)
		}
	}

	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return Metadata{}, fmt.Errorf("create skills dir: %w", err)
	}
	bodyPath := proposal.SkillID + ".md"
	fullBodyPath := filepath.Join(p.dir, bodyPath)
	if _, err := os.Stat(fullBodyPath); err == nil {
		return Metadata{}, fmt.Errorf("%w: body file %s", ErrSkillConflict, bodyPath)
	}
	// Body first, index second: a failed index write leaves only a harmless
	// orphan .md file, while the index (the load source of truth) stays intact.
	if err := os.WriteFile(fullBodyPath, []byte(proposal.Body), 0o644); err != nil {
		return Metadata{}, fmt.Errorf("write promoted skill body: %w", err)
	}

	meta := Metadata{
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
	metas = append(metas, meta)
	data, err := json.MarshalIndent(indexFile{Skills: metas}, "", "  ")
	if err != nil {
		return Metadata{}, fmt.Errorf("marshal skill index: %w", err)
	}
	if err := os.WriteFile(p.indexPath(), append(data, '\n'), 0o644); err != nil {
		return Metadata{}, fmt.Errorf("write skill index: %w", err)
	}
	return meta, nil
}
