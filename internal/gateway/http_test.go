package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/auth"
	"github.com/sangjinsu/orbis/internal/protocol"
)

// testGatewayAuth configures an admin token "tok" (name alice) and a reviewer
// token "rev-tok" (name bob) for the endpoint role matrices.
func testGatewayAuth() *auth.Authenticator {
	return auth.New([]auth.TokenEntry{
		{Name: "alice", Role: auth.RoleAdmin, Token: "tok"},
		{Name: "bob", Role: auth.RoleReviewer, Token: "rev-tok"},
	})
}

var errSkillReload = errors.New("reload failed")

type recordingSkills struct {
	lastActor   string
	list        protocol.SkillListPayload
	detail      map[string]protocol.SkillDetailPayload
	reloadCount int
	reloadErr   error
}

func (s *recordingSkills) ListSkills() protocol.SkillListPayload { return s.list }

func (s *recordingSkills) GetSkill(id string) (protocol.SkillDetailPayload, bool) {
	detail, ok := s.detail[id]
	return detail, ok
}

func (s *recordingSkills) ReloadSkills(actor string) error {
	s.lastActor = actor
	s.reloadCount++
	return s.reloadErr
}

func TestHTTPHealthEndpoints(t *testing.T) {
	handler := NewHTTPHandler(&recordingRuntime{})

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestHTTPDebugEndpoints(t *testing.T) {
	handler := NewHTTPHandler(&recordingRuntime{})

	t.Run("index", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /debug status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "Runtime Visualizer") {
			t.Fatalf("GET /debug body missing Runtime Visualizer")
		}
	})

	t.Run("asset", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/app.js", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /debug/app.js status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "session.message") {
			t.Fatalf("GET /debug/app.js body missing session.message")
		}
	})

	t.Run("missing asset", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/missing.js", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /debug/missing.js status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHTTPSkillsEndpoints(t *testing.T) {
	skills := &recordingSkills{
		list: protocol.SkillListPayload{Skills: []protocol.SkillSummary{
			{ID: "ws-test", Name: "ws", Title: "WebSocket Runtime Test"},
		}},
		detail: map[string]protocol.SkillDetailPayload{
			"ws-test": {
				SkillSummary: protocol.SkillSummary{ID: "ws-test", Name: "ws"},
				Body:         "WS BODY",
				ContentHash:  "hash-ws",
				Chars:        7,
			},
		},
	}
	handler := NewHTTPHandler(&recordingRuntime{}, WithSkills(skills))

	t.Run("list", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /skills status = %d, want 200", rec.Code)
		}
		var list protocol.SkillListPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		if len(list.Skills) != 1 || list.Skills[0].ID != "ws-test" {
			t.Fatalf("list = %#v, want one ws-test", list.Skills)
		}
	})

	t.Run("get found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/ws-test", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /skills/ws-test status = %d, want 200", rec.Code)
		}
		var detail protocol.SkillDetailPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("unmarshal detail: %v", err)
		}
		if detail.ID != "ws-test" || detail.Body != "WS BODY" {
			t.Fatalf("detail = %#v, want ws-test with body", detail)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/unknown", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /skills/unknown status = %d, want 404", rec.Code)
		}
	})

	t.Run("reload disabled without admin token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/reload", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("POST /skills/reload status = %d, want 403 with no admin token", rec.Code)
		}
	})

	t.Run("reload with admin token", func(t *testing.T) {
		adminHandler := NewHTTPHandler(&recordingRuntime{}, WithSkills(skills), WithAuth(testGatewayAuth()))

		rec := httptest.NewRecorder()
		wrong := httptest.NewRequest(http.MethodPost, "/skills/reload", nil)
		wrong.Header.Set("Authorization", "Bearer nope")
		adminHandler.ServeHTTP(rec, wrong)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong token status = %d, want 401", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/skills/reload", nil)
		req.Header.Set("Authorization", "Bearer tok")
		adminHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /skills/reload status = %d, want 200", rec.Code)
		}
		if skills.reloadCount != 1 {
			t.Fatalf("reloadCount = %d, want 1", skills.reloadCount)
		}
		var reload protocol.SkillReloadPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &reload); err != nil {
			t.Fatalf("unmarshal reload: %v", err)
		}
		if reload.Count != 1 {
			t.Fatalf("reload count = %d, want 1", reload.Count)
		}
	})
}

func TestHTTPSkillsReloadErrorReturns500(t *testing.T) {
	skills := &recordingSkills{reloadErr: errSkillReload}
	handler := NewHTTPHandler(&recordingRuntime{}, WithSkills(skills), WithAuth(testGatewayAuth()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skills/reload", nil)
	req.Header.Set("Authorization", "Bearer tok")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("reload error status = %d, want 500", rec.Code)
	}
}

func TestHTTPSkillsNotRegisteredWithoutOption(t *testing.T) {
	handler := NewHTTPHandler(&recordingRuntime{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /skills without WithSkills = %d, want 404", rec.Code)
	}
}

type recordingSkillLearning struct {
	list       protocol.SkillProposalListPayload
	detail     map[string]protocol.SkillProposalDetailPayload
	lastStatus string
	lastReason string
	lastActor  string
	created    []string
	updated    []string
	lastUpdate protocol.SkillProposalUpdateRequest
	approved   []string
	rejected   []string
}

func (l *recordingSkillLearning) ListSkillProposals(status string) (protocol.SkillProposalListPayload, error) {
	l.lastStatus = status
	return l.list, nil
}

func (l *recordingSkillLearning) GetSkillProposal(id string) (protocol.SkillProposalDetailPayload, bool, error) {
	detail, ok := l.detail[id]
	return detail, ok, nil
}

func (l *recordingSkillLearning) CreateSkillProposal(_ context.Context, runID, actor string) (protocol.SkillProposalDetailPayload, error) {
	l.created = append(l.created, runID)
	l.lastActor = actor
	return protocol.SkillProposalDetailPayload{SkillProposalSummary: protocol.SkillProposalSummary{ProposalID: "prop_" + runID, Status: "pending"}}, nil
}

func (l *recordingSkillLearning) UpdateSkillProposal(_ context.Context, id string, fields protocol.SkillProposalUpdateRequest, actor string) (protocol.SkillProposalDetailPayload, error) {
	l.updated = append(l.updated, id)
	l.lastUpdate = fields
	l.lastActor = actor
	return protocol.SkillProposalDetailPayload{SkillProposalSummary: protocol.SkillProposalSummary{ProposalID: id, Status: "pending", Revision: 1}}, nil
}

func (l *recordingSkillLearning) ApproveSkillProposal(_ context.Context, id, actor string) (protocol.SkillProposalDetailPayload, error) {
	l.approved = append(l.approved, id)
	l.lastActor = actor
	return protocol.SkillProposalDetailPayload{SkillProposalSummary: protocol.SkillProposalSummary{ProposalID: id, Status: "promoted"}}, nil
}

func (l *recordingSkillLearning) RejectSkillProposal(_ context.Context, id, reason, actor string) (protocol.SkillProposalDetailPayload, error) {
	l.rejected = append(l.rejected, id)
	l.lastReason = reason
	l.lastActor = actor
	return protocol.SkillProposalDetailPayload{SkillProposalSummary: protocol.SkillProposalSummary{ProposalID: id, Status: "rejected"}}, nil
}

func TestHTTPSkillProposalEndpoints(t *testing.T) {
	learning := &recordingSkillLearning{
		list: protocol.SkillProposalListPayload{Proposals: []protocol.SkillProposalSummary{{ProposalID: "prop_1", Status: "pending"}}},
		detail: map[string]protocol.SkillProposalDetailPayload{
			"prop_1": {SkillProposalSummary: protocol.SkillProposalSummary{ProposalID: "prop_1", Status: "pending"}, Body: "BODY"},
		},
	}
	handler := NewHTTPHandler(&recordingRuntime{}, WithSkillLearning(learning), WithAuth(testGatewayAuth()))
	admin := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Header.Set("Authorization", "Bearer tok")
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("list with status filter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skill-proposals?status=pending", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /skill-proposals status = %d, want 200", rec.Code)
		}
		if learning.lastStatus != "pending" {
			t.Fatalf("status filter = %q, want pending", learning.lastStatus)
		}
		var list protocol.SkillProposalListPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Proposals) != 1 {
			t.Fatalf("list = %v, %v; want one proposal", list, err)
		}
	})

	t.Run("get found and not found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skill-proposals/prop_1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /skill-proposals/prop_1 status = %d, want 200", rec.Code)
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skill-proposals/unknown", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /skill-proposals/unknown status = %d, want 404", rec.Code)
		}
	})

	t.Run("create requires bearer token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/runs/run_1/skill-proposals", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("create without token status = %d, want 401", rec.Code)
		}
		rec = admin(http.MethodPost, "/runs/run_1/skill-proposals", "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status = %d, want 201", rec.Code)
		}
		if len(learning.created) != 1 || learning.created[0] != "run_1" {
			t.Fatalf("created = %v, want [run_1]", learning.created)
		}
	})

	t.Run("update requires bearer token and decodes fields", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/skill-proposals/prop_1", strings.NewReader(`{"title":"Edited"}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("update without token status = %d, want 401", rec.Code)
		}
		rec = admin(http.MethodPatch, "/skill-proposals/prop_1", `{"title":"Edited","procedure":["one","two"]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if len(learning.updated) != 1 || learning.updated[0] != "prop_1" {
			t.Fatalf("updated = %v, want [prop_1]", learning.updated)
		}
		if learning.lastUpdate.Title == nil || *learning.lastUpdate.Title != "Edited" {
			t.Fatalf("update title = %v, want Edited", learning.lastUpdate.Title)
		}
		if learning.lastUpdate.Procedure == nil || len(*learning.lastUpdate.Procedure) != 2 {
			t.Fatalf("update procedure = %v, want two steps", learning.lastUpdate.Procedure)
		}
		rec = admin(http.MethodPatch, "/skill-proposals/prop_1", `not json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("update with invalid body status = %d, want 400", rec.Code)
		}
	})

	t.Run("approve and reject", func(t *testing.T) {
		rec := admin(http.MethodPost, "/skill-proposals/prop_1/approve", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("approve status = %d, want 200", rec.Code)
		}
		if len(learning.approved) != 1 || learning.approved[0] != "prop_1" {
			t.Fatalf("approved = %v, want [prop_1]", learning.approved)
		}
		if learning.lastActor != "alice" {
			t.Fatalf("approve actor = %q, want the admin token's name alice", learning.lastActor)
		}
		rec = admin(http.MethodPost, "/skill-proposals/prop_2/reject", `{"reason":"too narrow"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("reject status = %d, want 200", rec.Code)
		}
		if learning.lastReason != "too narrow" {
			t.Fatalf("reject reason = %q, want too narrow", learning.lastReason)
		}
	})

	t.Run("reviewer role covers proposals but not reload", func(t *testing.T) {
		reviewer := func(method, path string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("Authorization", "Bearer rev-tok")
			handler.ServeHTTP(rec, req)
			return rec
		}
		if rec := reviewer(http.MethodPost, "/skill-proposals/prop_1/approve"); rec.Code != http.StatusOK {
			t.Fatalf("reviewer approve status = %d, want 200", rec.Code)
		}
		if learning.lastActor != "bob" {
			t.Fatalf("reviewer actor = %q, want bob", learning.lastActor)
		}
		// The reload route is admin-only; register it on a handler that has both.
		skills := &recordingSkills{}
		both := NewHTTPHandler(&recordingRuntime{}, WithSkills(skills), WithSkillLearning(learning), WithAuth(testGatewayAuth()))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/skills/reload", nil)
		req.Header.Set("Authorization", "Bearer rev-tok")
		both.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("reviewer reload status = %d, want 403", rec.Code)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/skills/reload", nil)
		req.Header.Set("Authorization", "Bearer tok")
		both.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || skills.lastActor != "alice" {
			t.Fatalf("admin reload = %d (actor %q), want 200 by alice", rec.Code, skills.lastActor)
		}
	})
}

func TestHTTPSkillProposalMutationsDisabledWithoutAdmin(t *testing.T) {
	learning := &recordingSkillLearning{}
	handler := NewHTTPHandler(&recordingRuntime{}, WithSkillLearning(learning))

	// Reads stay open.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skill-proposals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /skill-proposals status = %d, want 200", rec.Code)
	}
	// Mutations are disabled entirely with no admin token configured.
	for _, req := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/runs/run_1/skill-proposals"},
		{http.MethodPatch, "/skill-proposals/p"},
		{http.MethodPost, "/skill-proposals/p/approve"},
		{http.MethodPost, "/skill-proposals/p/reject"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(req.method, req.path, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d, want 403", req.method, req.path, rec.Code)
		}
	}
	if len(learning.created)+len(learning.updated)+len(learning.approved)+len(learning.rejected) != 0 {
		t.Fatal("mutating calls reached the service despite the disabled admin gate")
	}
}

func TestHTTPSkillProposalsNotRegisteredWithoutOption(t *testing.T) {
	handler := NewHTTPHandler(&recordingRuntime{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skill-proposals", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /skill-proposals without option = %d, want 404", rec.Code)
	}
}

func TestReadTimeoutDefaultsDisabledAndIsConfigurable(t *testing.T) {
	cfg := handlerConfig{}
	if cfg.readTimeout != 0 {
		t.Fatalf("default readTimeout = %v, want 0 so idle subscriptions stay open", cfg.readTimeout)
	}
	WithReadTimeout(120 * time.Second)(&cfg)
	if cfg.readTimeout != 120*time.Second {
		t.Fatalf("readTimeout = %v, want 120s after WithReadTimeout", cfg.readTimeout)
	}
}
