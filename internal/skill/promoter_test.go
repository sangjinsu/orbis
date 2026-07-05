package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func approvedProposal(id, skillID string, now time.Time) SkillProposal {
	p := testProposal(id, now)
	p.SkillID = skillID
	p.Status = ProposalApproved
	return p
}

func TestPromoterPromotesProposalAndBootstrapsIndex(t *testing.T) {
	dir := t.TempDir()
	promoter := NewPromoter(dir)
	now := time.Unix(1700000000, 0).UTC()
	proposal := approvedProposal("prop_1", "learned-ws-smoke", now)

	meta, err := promoter.Promote(proposal, now)
	if err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if meta.ID != "learned-ws-smoke" || meta.Version != "1" || meta.Status != "active" {
		t.Fatalf("meta = %#v, want active learned-ws-smoke v1", meta)
	}
	if meta.SourceProposalID != "prop_1" || meta.SourceRunID != "run_1" || meta.CreatedAt.IsZero() {
		t.Fatalf("meta provenance = %#v, want proposal/run ids and created_at", meta)
	}
	if meta.Priority != learnedSkillPriority {
		t.Fatalf("priority = %d, want %d (below curated seeds)", meta.Priority, learnedSkillPriority)
	}

	body, err := os.ReadFile(filepath.Join(dir, "learned-ws-smoke.md"))
	if err != nil || string(body) != proposal.Body {
		t.Fatalf("promoted body = %q, %v; want the proposal body", body, err)
	}

	// The store loads the promoted skill and re-derives the same content hash.
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() after promotion error = %v", err)
	}
	entry, ok := store.Get("learned-ws-smoke")
	if !ok {
		t.Fatal("promoted skill not loadable from the index")
	}
	if entry.ContentHash != proposal.ContentHash && proposal.ContentHash != "" {
		t.Fatalf("loaded hash %q != proposal hash %q", entry.ContentHash, proposal.ContentHash)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "learned" {
		t.Fatalf("tags = %v, want [learned]", entry.Tags)
	}
}

func TestPromoterAppendsToExistingIndex(t *testing.T) {
	dir := t.TempDir()
	seed := `{"skills":[{"id":"seed","name":"seed","path":"seed.md","status":"active","priority":100}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.md"), []byte("seed body"), 0o644); err != nil {
		t.Fatalf("write seed body: %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()

	if _, err := NewPromoter(dir).Promote(approvedProposal("prop_1", "learned-extra", now), now); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	metas, err := parseIndex(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("parseIndex() error = %v", err)
	}
	if len(metas) != 2 || metas[0].ID != "seed" || metas[1].ID != "learned-extra" {
		t.Fatalf("index = %v, want seed then learned-extra", metas)
	}
}

func TestPromoterRejectsSeedConflictAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	// A curated seed has no learned provenance (source_proposal_id) and is
	// never replaced by a promotion.
	seed := `{"skills":[{"id":"seed-skill","name":"seed-skill","path":"seed-skill.md","status":"active","priority":100,"version":"1.0.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed-skill.md"), []byte("seed body"), 0o644); err != nil {
		t.Fatalf("write seed body: %v", err)
	}
	promoter := NewPromoter(dir)
	now := time.Unix(1700000000, 0).UTC()

	if _, err := promoter.Promote(approvedProposal("prop_1", "seed-skill", now), now); !errors.Is(err, ErrSkillConflict) {
		t.Fatalf("seed Promote() error = %v, want ErrSkillConflict", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "seed-skill.md"))
	if err != nil || string(body) != "seed body" {
		t.Fatalf("seed body = %q, %v; want untouched seed body", body, err)
	}
	// Path-escaping or otherwise unsafe ids are rejected outright.
	if _, err := promoter.Promote(approvedProposal("prop_3", "../evil", now), now); err == nil || strings.Contains(err.Error(), "conflict") {
		t.Fatalf("invalid id Promote() error = %v, want invalid skill id error", err)
	}
}

func TestPromoterBumpsVersionForLearnedSkill(t *testing.T) {
	dir := t.TempDir()
	promoter := NewPromoter(dir)
	now := time.Unix(1700000000, 0).UTC()
	later := now.Add(time.Hour)

	if _, err := promoter.Promote(approvedProposal("prop_1", "learned-dup", now), now); err != nil {
		t.Fatalf("first Promote() error = %v", err)
	}
	firstBody := approvedProposal("prop_1", "learned-dup", now).Body

	second := approvedProposal("prop_2", "learned-dup", later)
	second.Title = "WebSocket smoke workflow v2"
	second.Purpose = "Test the runtime over WebSocket, updated"
	second.RelatedTools = []string{"math.add", "math.mul"}
	second.Body = "# WebSocket smoke workflow v2\n\nUpdated procedure."
	meta, err := promoter.Promote(second, later)
	if err != nil {
		t.Fatalf("re-Promote() error = %v", err)
	}
	if meta.Version != "2" || meta.ID != "learned-dup" {
		t.Fatalf("meta = %#v, want learned-dup v2", meta)
	}
	if meta.SourceProposalID != "prop_2" || !meta.CreatedAt.Equal(later) {
		t.Fatalf("meta provenance = %#v, want prop_2 at the later time", meta)
	}
	if meta.Title != "WebSocket smoke workflow v2" || meta.Description != second.Purpose {
		t.Fatalf("meta content = %#v, want the second proposal's title/purpose", meta)
	}
	if meta.Priority != learnedSkillPriority || meta.Status != "active" || meta.Path != "learned-dup.md" {
		t.Fatalf("meta curation fields = %#v, want them unchanged", meta)
	}

	metas, err := parseIndex(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("parseIndex() error = %v", err)
	}
	if len(metas) != 1 || metas[0].Version != "2" {
		t.Fatalf("index = %v, want a single learned-dup entry at v2", metas)
	}

	live, err := os.ReadFile(filepath.Join(dir, "learned-dup.md"))
	if err != nil || string(live) != second.Body {
		t.Fatalf("live body = %q, %v; want the second proposal body", live, err)
	}
	archived, err := os.ReadFile(filepath.Join(dir, "archive", "learned-dup@1.md"))
	if err != nil || string(archived) != firstBody {
		t.Fatalf("archived body = %q, %v; want the first proposal body", archived, err)
	}
}

func TestPromoterFailsOnNonIntegerVersion(t *testing.T) {
	dir := t.TempDir()
	// A learned entry (has provenance) with a non-integer version is corrupt:
	// bumping must fail loudly instead of guessing.
	index := `{"skills":[{"id":"learned-bad","name":"learned-bad","path":"learned-bad.md","status":"active","priority":50,"version":"1.0.0","source_proposal_id":"prop_0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "learned-bad.md"), []byte("old body"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()

	_, err := NewPromoter(dir).Promote(approvedProposal("prop_1", "learned-bad", now), now)
	if err == nil || errors.Is(err, ErrSkillConflict) || !strings.Contains(err.Error(), "not a positive integer") {
		t.Fatalf("Promote() error = %v, want a non-integer version error", err)
	}
	body, readErr := os.ReadFile(filepath.Join(dir, "learned-bad.md"))
	if readErr != nil || string(body) != "old body" {
		t.Fatalf("body = %q, %v; want it untouched after the failed bump", body, readErr)
	}
}

func TestPromoterOverwritesOrphanBody(t *testing.T) {
	dir := t.TempDir()
	// An orphan .md without an index entry (e.g. left by a failed index write)
	// must not block promotion: the index is the load source of truth.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "learned-orphan.md"), []byte("orphan body"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()

	proposal := approvedProposal("prop_1", "learned-orphan", now)
	meta, err := NewPromoter(dir).Promote(proposal, now)
	if err != nil {
		t.Fatalf("Promote() over orphan error = %v", err)
	}
	if meta.Version != "1" {
		t.Fatalf("meta.Version = %q, want fresh promotion at v1", meta.Version)
	}
	body, err := os.ReadFile(filepath.Join(dir, "learned-orphan.md"))
	if err != nil || string(body) != proposal.Body {
		t.Fatalf("body = %q, %v; want the proposal body over the orphan", body, err)
	}
}
