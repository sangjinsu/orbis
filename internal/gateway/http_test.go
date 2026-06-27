package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestDefaultReadTimeoutDisabledForLongRunningSubscriptions(t *testing.T) {
	if got := defaultReadTimeout(); got != 0 {
		t.Fatalf("defaultReadTimeout() = %v, want 0 for long-running subscriptions", got)
	}
}
