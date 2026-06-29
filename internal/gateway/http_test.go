package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/protocol"
)

var errSkillReload = errors.New("reload failed")

type recordingSkills struct {
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

func (s *recordingSkills) ReloadSkills() error {
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

	t.Run("reload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/reload", nil))
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
	handler := NewHTTPHandler(&recordingRuntime{}, WithSkills(skills))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/reload", nil))
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
