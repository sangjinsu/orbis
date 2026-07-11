package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSkillsListPrintsTableAndJSON(t *testing.T) {
	raw := `{"skills":[{"id":"s1","name":"s1","title":"First skill","status":"active","version":"1","priority":100},{"id":"s2","name":"s2","title":"Second","status":"active","version":"2","priority":50}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skills" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(raw))
	}))
	defer server.Close()

	var out strings.Builder
	if err := runSkillsList(context.Background(), testClient(server.URL), false, &out); err != nil {
		t.Fatalf("runSkillsList() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "s1") || !strings.Contains(got, "v2") || !strings.Contains(got, "2 skills") {
		t.Fatalf("table output = %q, want both skills and the count", got)
	}

	// -json passes the server body through unchanged.
	out.Reset()
	if err := runSkillsList(context.Background(), testClient(server.URL), true, &out); err != nil {
		t.Fatalf("runSkillsList(json) error = %v", err)
	}
	if strings.TrimSpace(out.String()) != raw {
		t.Fatalf("json output = %q, want the raw body", out.String())
	}
}

func TestRunSkillsGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "skill not found", http.StatusNotFound)
	}))
	defer server.Close()

	var out strings.Builder
	err := runSkillsGet(context.Background(), testClient(server.URL), "missing", false, &out)
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("runSkillsGet() error = %v, want 404 apiError", err)
	}
}

func TestRunSkillsReloadAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/skills/reload" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer admin-tok" {
			http.Error(w, "insufficient role for this operation", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"count":3}`))
	}))
	defer server.Close()

	// A reviewer-level token is rejected and the server message surfaces.
	reviewer := testClient(server.URL)
	reviewer.Token = "rev-tok"
	var out strings.Builder
	err := runSkillsReload(context.Background(), reviewer, false, &out)
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "insufficient role") {
		t.Fatalf("runSkillsReload(reviewer) error = %v, want 403 with the server body", err)
	}

	admin := testClient(server.URL)
	admin.Token = "admin-tok"
	out.Reset()
	if err := runSkillsReload(context.Background(), admin, false, &out); err != nil {
		t.Fatalf("runSkillsReload(admin) error = %v", err)
	}
	if strings.TrimSpace(out.String()) != "reloaded: 3 skills" {
		t.Fatalf("output = %q, want reloaded: 3 skills", out.String())
	}
}
