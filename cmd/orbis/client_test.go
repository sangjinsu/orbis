package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPBaseURLFromAddr(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want string
	}{
		{":8080", "http://127.0.0.1:8080"},
		{"localhost:9090", "http://localhost:9090"},
		{"http://example.test/", "http://example.test"},
		{"https://example.test", "https://example.test"},
	} {
		if got := httpBaseURLFromAddr(tc.addr); got != tc.want {
			t.Fatalf("httpBaseURLFromAddr(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func testClient(serverURL string) *apiClient {
	return &apiClient{BaseURL: serverURL, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func TestAPIClientTokenHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	withToken := testClient(server.URL)
	withToken.Token = "tok"
	if _, err := withToken.do(context.Background(), http.MethodGet, "/x", nil); err != nil {
		t.Fatalf("do() error = %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("Authorization = %q, want Bearer tok", gotAuth)
	}

	// An empty token omits the header entirely (reads are open).
	if _, err := testClient(server.URL).do(context.Background(), http.MethodGet, "/x", nil); err != nil {
		t.Fatalf("do() without token error = %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want no header without a token", gotAuth)
	}
}

func TestAPIClientSurfacesErrorStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := testClient(server.URL).do(context.Background(), http.MethodPost, "/x", nil)
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("do() error = %v, want *apiError", err)
	}
	if apiErr.Status != http.StatusUnauthorized || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("apiError = %v, want status 401 with the server body", err)
	}
}
