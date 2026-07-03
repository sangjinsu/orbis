package skill

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testProposal(id string, now time.Time) SkillProposal {
	return SkillProposal{
		ProposalID:       id,
		SourceRunID:      "run_1",
		Title:            "WebSocket smoke workflow",
		SkillID:          "ws-smoke-workflow",
		Purpose:          "Test the runtime over WebSocket",
		Procedure:        []string{"start the server", "run the smoke client"},
		RelatedTools:     []string{"math.add"},
		RationaleSummary: "run completed using tools and skills",
		Body:             "# WebSocket smoke workflow\n\nStart the server, then run the smoke client.",
		Status:           ProposalPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func TestProposalStoreCreateGetList(t *testing.T) {
	store, err := NewProposalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()

	first := testProposal("prop_1", now)
	second := testProposal("prop_2", now.Add(time.Second))
	if err := store.Create(first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if err := store.Create(second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if err := store.Create(first); err == nil {
		t.Fatal("Create(duplicate) error = nil, want already-exists error")
	}

	got, err := store.Get("prop_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.SkillID != "ws-smoke-workflow" || got.Status != ProposalPending {
		t.Fatalf("Get() = %#v, want pending ws-smoke-workflow", got)
	}

	pending, err := store.List(ProposalPending)
	if err != nil {
		t.Fatalf("List(pending) error = %v", err)
	}
	if len(pending) != 2 || pending[0].ProposalID != "prop_1" || pending[1].ProposalID != "prop_2" {
		t.Fatalf("List(pending) = %#v, want [prop_1 prop_2] in CreatedAt order", pending)
	}

	if _, err := store.Get("missing"); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrProposalNotFound", err)
	}
}

func TestProposalStoreStatusTransitionsMoveFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProposalStore(dir)
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()
	p := testProposal("prop_1", now)
	if err := store.Create(p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Approve: the file moves from pending/ to approved/.
	approvedAt := now.Add(time.Minute)
	p.Status = ProposalApproved
	p.ApprovedAt = &approvedAt
	p.UpdatedAt = approvedAt
	if err := store.Update(p); err != nil {
		t.Fatalf("Update(approved) error = %v", err)
	}
	name := sanitizeProposalName("prop_1") + ".json"
	if _, err := os.Stat(filepath.Join(dir, "pending", name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending file still exists after approval: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "approved", name)); err != nil {
		t.Fatalf("approved file missing: %v", err)
	}

	// Promote: stays in the approved bucket with status promoted.
	p.Status = ProposalPromoted
	p.PromotedSkillID = p.SkillID
	if err := store.Update(p); err != nil {
		t.Fatalf("Update(promoted) error = %v", err)
	}
	got, err := store.Get("prop_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != ProposalPromoted || got.PromotedSkillID != "ws-smoke-workflow" {
		t.Fatalf("proposal = %#v, want promoted", got)
	}
	promoted, err := store.List(ProposalPromoted)
	if err != nil || len(promoted) != 1 {
		t.Fatalf("List(promoted) = %v, %v; want one", promoted, err)
	}
}

func TestProposalStoreRejectsIllegalTransitions(t *testing.T) {
	store, err := NewProposalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewProposalStore() error = %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()
	p := testProposal("prop_1", now)
	if err := store.Create(p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// A pending proposal can never jump straight to promoted: promotion requires
	// an explicit approval first.
	p.Status = ProposalPromoted
	if err := store.Update(p); err == nil {
		t.Fatal("Update(pending -> promoted) error = nil, want illegal transition")
	}

	p.Status = ProposalRejected
	rejectedAt := now.Add(time.Minute)
	p.RejectedAt = &rejectedAt
	if err := store.Update(p); err != nil {
		t.Fatalf("Update(rejected) error = %v", err)
	}
	p.Status = ProposalApproved
	if err := store.Update(p); err == nil {
		t.Fatal("Update(rejected -> approved) error = nil, want illegal transition")
	}

	missing := testProposal("prop_missing", now)
	missing.Status = ProposalApproved
	if err := store.Update(missing); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("Update(missing) error = %v, want ErrProposalNotFound", err)
	}
}

func TestCanTransitionTable(t *testing.T) {
	for _, tc := range []struct {
		from, to SkillProposalStatus
		want     bool
	}{
		{ProposalPending, ProposalApproved, true},
		{ProposalPending, ProposalRejected, true},
		{ProposalPending, ProposalPromoted, false},
		{ProposalApproved, ProposalPromoted, true},
		{ProposalApproved, ProposalFailed, true},
		{ProposalApproved, ProposalRejected, false},
		{ProposalFailed, ProposalPromoted, true},
		{ProposalRejected, ProposalApproved, false},
		{ProposalPromoted, ProposalPending, false},
	} {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Fatalf("CanTransition(%s, %s) = %t, want %t", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestProposalValidateRejectsMissingFields(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	for name, mutate := range map[string]func(*SkillProposal){
		"proposal_id": func(p *SkillProposal) { p.ProposalID = "" },
		"source_run":  func(p *SkillProposal) { p.SourceRunID = "" },
		"skill_id":    func(p *SkillProposal) { p.SkillID = "" },
		"title":       func(p *SkillProposal) { p.Title = "" },
		"body":        func(p *SkillProposal) { p.Body = "" },
		"status":      func(p *SkillProposal) { p.Status = "bogus" },
	} {
		p := testProposal("prop_1", now)
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Fatalf("Validate() with missing %s = nil, want error", name)
		}
	}
}

func TestAuditLogAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "skill_audit.jsonl")
	log := NewAuditLog(path)
	now := time.Unix(1700000000, 0).UTC()

	records := []AuditRecord{
		{AuditID: "audit_1", EventType: "SkillProposalCreated", ProposalID: "prop_1", SourceRunID: "run_1", Actor: ActorAdmin, Timestamp: now, Summary: "proposal created from run"},
		{AuditID: "audit_2", EventType: "SkillPromoted", ProposalID: "prop_1", SkillID: "ws-smoke-workflow", Actor: ActorAdmin, Timestamp: now.Add(time.Minute), Summary: "proposal promoted"},
	}
	for _, record := range records {
		if err := log.Append(record); err != nil {
			t.Fatalf("Append(%s) error = %v", record.AuditID, err)
		}
	}
	if err := log.Append(AuditRecord{EventType: "x"}); err == nil {
		t.Fatal("Append without audit_id error = nil, want error")
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var got []AuditRecord
	for scanner.Scan() {
		var record AuditRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode audit line: %v", err)
		}
		got = append(got, record)
	}
	if len(got) != 2 || got[0].AuditID != "audit_1" || got[1].EventType != "SkillPromoted" {
		t.Fatalf("audit records = %#v, want the two appended records in order", got)
	}
}

func TestVersioningHelpers(t *testing.T) {
	if contentHash("body") != contentHash("body") {
		t.Fatal("contentHash is not deterministic")
	}
	if contentHash("a") == contentHash("b") {
		t.Fatal("contentHash collides for different bodies")
	}
	if got := normalizeVersion(""); got != "1" {
		t.Fatalf("normalizeVersion(\"\") = %q, want 1", got)
	}
	if got := normalizeVersion(" 2 "); got != "2" {
		t.Fatalf("normalizeVersion(\" 2 \") = %q, want 2", got)
	}

	index := fakeIndexWith("existing-skill")
	if err := EnsureSkillIDAvailable(index, "existing-skill"); !errors.Is(err, ErrSkillConflict) {
		t.Fatalf("EnsureSkillIDAvailable(existing) error = %v, want ErrSkillConflict", err)
	}
	if err := EnsureSkillIDAvailable(index, "new-skill"); err != nil {
		t.Fatalf("EnsureSkillIDAvailable(new) error = %v, want nil", err)
	}
	if err := EnsureSkillIDAvailable(nil, "any"); err != nil {
		t.Fatalf("EnsureSkillIDAvailable(nil index) error = %v, want nil", err)
	}
}

type staticIndex struct{ entries []Entry }

func (s staticIndex) Snapshot() []Entry { return s.entries }

func fakeIndexWith(ids ...string) Index {
	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, Entry{Metadata: Metadata{ID: id}})
	}
	return staticIndex{entries: entries}
}
