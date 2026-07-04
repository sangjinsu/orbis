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

func TestPromoterRejectsConflictsAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	promoter := NewPromoter(dir)
	now := time.Unix(1700000000, 0).UTC()

	if _, err := promoter.Promote(approvedProposal("prop_1", "learned-dup", now), now); err != nil {
		t.Fatalf("first Promote() error = %v", err)
	}
	// Same skill id again: reject-on-conflict.
	if _, err := promoter.Promote(approvedProposal("prop_2", "learned-dup", now), now); !errors.Is(err, ErrSkillConflict) {
		t.Fatalf("duplicate Promote() error = %v, want ErrSkillConflict", err)
	}
	// Path-escaping or otherwise unsafe ids are rejected outright.
	if _, err := promoter.Promote(approvedProposal("prop_3", "../evil", now), now); err == nil || strings.Contains(err.Error(), "conflict") {
		t.Fatalf("invalid id Promote() error = %v, want invalid skill id error", err)
	}
}
