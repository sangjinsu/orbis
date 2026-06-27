package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
