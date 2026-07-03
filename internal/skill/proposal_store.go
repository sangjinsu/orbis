package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ErrProposalNotFound is returned when no stored proposal matches the id.
var ErrProposalNotFound = errors.New("skill proposal not found")

// proposalDirs are the on-disk review buckets. Statuses map onto them coarsely:
// the JSON status field is the source of truth, the directory is the review
// queue a proposal sits in (promoted/failed proposals stay under approved/).
var proposalDirs = []string{"pending", "approved", "rejected"}

// ProposalStore persists skill proposals as JSON files under
// {dir}/{pending|approved|rejected}/{id}.json. It is runtime data and lives
// under data/, never .workspace.
type ProposalStore struct {
	dir string
	mu  sync.Mutex
}

// NewProposalStore creates the store rooted at dir (e.g. data/skill_proposals)
// and ensures the review bucket directories exist.
func NewProposalStore(dir string) (*ProposalStore, error) {
	for _, sub := range proposalDirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create proposal dir %s: %w", sub, err)
		}
	}
	return &ProposalStore{dir: dir}, nil
}

// statusDir maps a proposal status onto its review bucket.
func statusDir(status SkillProposalStatus) string {
	switch status {
	case ProposalPending:
		return "pending"
	case ProposalRejected:
		return "rejected"
	default:
		// approved, promoted, and failed all live in the approved bucket; the
		// status field inside the JSON distinguishes them.
		return "approved"
	}
}

var unsafeProposalChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeProposalName converts a proposal id into a filesystem-safe file stem.
// A short hash of the original id is appended so distinct ids never collide
// after sanitization (mirrors the tool-call record naming).
func sanitizeProposalName(id string) string {
	cleaned := unsafeProposalChars.ReplaceAllString(id, "_")
	cleaned = strings.Trim(cleaned, "._-")
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	sum := sha256.Sum256([]byte(id))
	suffix := hex.EncodeToString(sum[:6])
	if cleaned == "" {
		return suffix
	}
	return cleaned + "_" + suffix
}

func (s *ProposalStore) path(status SkillProposalStatus, id string) string {
	return filepath.Join(s.dir, statusDir(status), sanitizeProposalName(id)+".json")
}

// find returns the current on-disk path of a proposal, scanning the buckets.
func (s *ProposalStore) find(id string) (string, bool) {
	name := sanitizeProposalName(id) + ".json"
	for _, sub := range proposalDirs {
		path := filepath.Join(s.dir, sub, name)
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

func (s *ProposalStore) write(path string, p SkillProposal) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal proposal %q: %w", p.ProposalID, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write proposal %q: %w", p.ProposalID, err)
	}
	return nil
}

// Create persists a new proposal. The proposal must validate and its id must
// not already exist in any bucket. An empty status defaults to pending.
func (s *ProposalStore) Create(p SkillProposal) error {
	if p.Status == "" {
		p.Status = ProposalPending
	}
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.find(p.ProposalID); exists {
		return fmt.Errorf("proposal %q already exists", p.ProposalID)
	}
	return s.write(s.path(p.Status, p.ProposalID), p)
}

// Get loads a proposal by id from whichever bucket holds it.
func (s *ProposalStore) Get(id string) (SkillProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *ProposalStore) get(id string) (SkillProposal, error) {
	path, ok := s.find(id)
	if !ok {
		return SkillProposal{}, fmt.Errorf("%w: %s", ErrProposalNotFound, id)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillProposal{}, fmt.Errorf("read proposal %q: %w", id, err)
	}
	var p SkillProposal
	if err := json.Unmarshal(data, &p); err != nil {
		return SkillProposal{}, fmt.Errorf("decode proposal %q: %w", id, err)
	}
	return p, nil
}

// List returns proposals with the given status, or every proposal when status
// is empty. Results are ordered by CreatedAt then ProposalID for a stable
// review queue.
func (s *ProposalStore) List(status SkillProposalStatus) ([]SkillProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	proposals := []SkillProposal{}
	for _, sub := range proposalDirs {
		entries, err := os.ReadDir(filepath.Join(s.dir, sub))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("list proposal dir %s: %w", sub, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(s.dir, sub, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("read proposal file %s: %w", entry.Name(), err)
			}
			var p SkillProposal
			if err := json.Unmarshal(data, &p); err != nil {
				return nil, fmt.Errorf("decode proposal file %s: %w", entry.Name(), err)
			}
			if status != "" && p.Status != status {
				continue
			}
			proposals = append(proposals, p)
		}
	}
	sort.Slice(proposals, func(i, j int) bool {
		if !proposals[i].CreatedAt.Equal(proposals[j].CreatedAt) {
			return proposals[i].CreatedAt.Before(proposals[j].CreatedAt)
		}
		return proposals[i].ProposalID < proposals[j].ProposalID
	})
	return proposals, nil
}

// Update rewrites an existing proposal, enforcing the review lifecycle and
// moving the file between buckets when the status changes. It refuses unknown
// ids and illegal transitions, so a pending proposal can never jump straight to
// promoted.
func (s *ProposalStore) Update(p SkillProposal) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.get(p.ProposalID)
	if err != nil {
		return err
	}
	if current.Status != p.Status && !CanTransition(current.Status, p.Status) {
		return fmt.Errorf("proposal %q: illegal transition %s -> %s", p.ProposalID, current.Status, p.Status)
	}
	oldPath, _ := s.find(p.ProposalID)
	newPath := s.path(p.Status, p.ProposalID)
	if err := s.write(newPath, p); err != nil {
		return err
	}
	if oldPath != newPath {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("move proposal %q: %w", p.ProposalID, err)
		}
	}
	return nil
}
