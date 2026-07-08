package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sangjinsu/orbis/internal/protocol"
)

func TestRunProposalListPassesStatusQuery(t *testing.T) {
	var gotStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStatus = r.URL.Query().Get("status")
		w.Write([]byte(`{"proposals":[{"proposal_id":"prop_1","source_run_id":"run_1","skill_id":"s1","title":"T","status":"pending","revision":2,"created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z"}]}`))
	}))
	defer server.Close()

	var out strings.Builder
	if err := runProposalList(context.Background(), testClient(server.URL), "pending", false, &out); err != nil {
		t.Fatalf("runProposalList() error = %v", err)
	}
	if gotStatus != "pending" {
		t.Fatalf("status query = %q, want pending", gotStatus)
	}
	if !strings.Contains(out.String(), "prop_1") || !strings.Contains(out.String(), "1 proposals") {
		t.Fatalf("output = %q, want the proposal row and count", out.String())
	}
}

func TestRunProposalCreatePostsToRunPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runs/run_x/skill-proposals" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"proposal_id":"prop_run_x","source_run_id":"run_x","skill_id":"learned-x","title":"T","status":"pending","created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z","body":"# T"}`))
	}))
	defer server.Close()

	var out strings.Builder
	if err := runProposalCreate(context.Background(), testClient(server.URL), "run_x", false, &out); err != nil {
		t.Fatalf("runProposalCreate() error = %v", err)
	}
	if !strings.Contains(out.String(), "created prop_run_x") || !strings.Contains(out.String(), "skill_id=learned-x") {
		t.Fatalf("output = %q, want the created confirmation", out.String())
	}
}

// The PATCH body must contain exactly the fields the reviewer set: absent
// flags stay absent (nil pointer => omitted key => server leaves unchanged).
func TestProposalEditSendsOnlyProvidedFields(t *testing.T) {
	var gotBody map[string]any
	server := newEditServer(t, &gotBody)
	defer server.Close()

	title := "New title"
	procedure := []string{"one", "two"}
	fields := protocol.SkillProposalUpdateRequest{Title: &title, Procedure: &procedure}
	var out strings.Builder
	if err := runProposalEdit(context.Background(), testClient(server.URL), "prop_1", fields, false, &out); err != nil {
		t.Fatalf("runProposalEdit() error = %v", err)
	}
	if len(gotBody) != 2 {
		t.Fatalf("PATCH body keys = %v, want exactly title and procedure", gotBody)
	}
	if gotBody["title"] != "New title" {
		t.Fatalf("title = %v, want New title", gotBody["title"])
	}
	if steps, ok := gotBody["procedure"].([]any); !ok || len(steps) != 2 {
		t.Fatalf("procedure = %v, want two steps", gotBody["procedure"])
	}
	if !strings.Contains(out.String(), "revision 3") || !strings.Contains(out.String(), "title, procedure") {
		t.Fatalf("output = %q, want the revision and edited field names", out.String())
	}
}

func TestProposalEditClearsListWithEmptyValue(t *testing.T) {
	var gotBody map[string]any
	server := newEditServer(t, &gotBody)
	defer server.Close()

	// Simulate `-procedure ""`: the flag was set once with an empty value.
	var procedure stringList
	if err := procedure.Set(""); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	fields := protocol.SkillProposalUpdateRequest{Procedure: procedure.toPtr()}
	var out strings.Builder
	if err := runProposalEdit(context.Background(), testClient(server.URL), "prop_1", fields, false, &out); err != nil {
		t.Fatalf("runProposalEdit() error = %v", err)
	}
	steps, ok := gotBody["procedure"].([]any)
	if !ok || len(steps) != 0 {
		t.Fatalf("procedure = %v (%T), want an explicit empty list", gotBody["procedure"], gotBody["procedure"])
	}
}

// newEditServer records the PATCH body as a generic map so tests can assert
// which keys were (and were not) sent.
func newEditServer(t *testing.T, into *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/skill-proposals/prop_1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(into); err != nil {
			t.Errorf("decode PATCH body: %v", err)
		}
		w.Write([]byte(`{"proposal_id":"prop_1","source_run_id":"run_1","skill_id":"s1","title":"New title","status":"pending","revision":3,"created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z","body":"# New title"}`))
	}))
}

func TestRunProposalRejectSendsReason(t *testing.T) {
	var gotReason string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skill-proposals/prop_1/reject" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotReason = body.Reason
		w.Write([]byte(`{"proposal_id":"prop_1","source_run_id":"run_1","skill_id":"s1","title":"T","status":"rejected","created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z","body":"# T"}`))
	}))
	defer server.Close()

	var out strings.Builder
	if err := runProposalReject(context.Background(), testClient(server.URL), "prop_1", "too narrow", false, &out); err != nil {
		t.Fatalf("runProposalReject() error = %v", err)
	}
	if gotReason != "too narrow" {
		t.Fatalf("reason = %q, want too narrow", gotReason)
	}
	if !strings.Contains(out.String(), "rejected prop_1") {
		t.Fatalf("output = %q, want the rejected confirmation", out.String())
	}
}

func TestRunProposalApprove(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skill-proposals/prop_1/approve" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"proposal_id":"prop_1","source_run_id":"run_1","skill_id":"s1","title":"T","status":"promoted","promoted_skill_id":"s1","version":"2","created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z","body":"# T"}`))
	}))
	defer server.Close()

	var out strings.Builder
	if err := runProposalApprove(context.Background(), testClient(server.URL), "prop_1", false, &out); err != nil {
		t.Fatalf("runProposalApprove() error = %v", err)
	}
	if !strings.Contains(out.String(), "status=promoted") || !strings.Contains(out.String(), "version=2") {
		t.Fatalf("output = %q, want promoted at version 2", out.String())
	}
}

func TestProposalEditWithoutFieldsIsUsageError(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"proposal", "edit", "prop_1"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	err := root.Execute()
	if !errors.Is(err, errUsage) {
		t.Fatalf("proposal edit without fields error = %v, want errUsage", err)
	}
}

// The cobra tree keeps the usage/runtime error split: bad flags and missing
// positionals surface as errUsage (exit 2), unknown subcommands as errors.
func TestRootCommandUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"skills", "get"},                 // missing positional
		{"proposal", "approve"},           // missing positional
		{"watch", "--no-such-flag"},       // unknown flag
		{"ws", "smoke", "bogus"},          // invalid smoke variant
		{"proposal", "reject"},            // missing positional
		{"skills", "list", "--bad-flag"},  // unknown flag on subcommand
		{"proposal", "list", "extra-arg"}, // unexpected positional
		{"skills", "reload", "extra-arg"}, // unexpected positional
		{"watch", "extra"},                // unexpected positional
		{"proposal", "edit"},              // missing positional
	} {
		root := newRootCmd()
		root.SetArgs(args)
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		err := root.Execute()
		if !errors.Is(err, errUsage) {
			t.Fatalf("args %v error = %v, want errUsage", args, err)
		}
	}
}
