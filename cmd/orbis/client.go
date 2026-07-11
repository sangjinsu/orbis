package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiClient is a thin client for the orbis gateway HTTP API. An empty Token
// omits the Authorization header entirely — reads are open, and the server
// decides which operations need which role.
type apiClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// apiError carries a non-2xx gateway response. Body is the plain-text message
// the server wrote via http.Error (e.g. "invalid token", "insufficient role
// for this operation"), which is already user-facing.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// do sends one request and returns the raw response body so -json output can
// pass the server's encoding through unchanged. A non-nil body is sent as JSON.
func (c *apiClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, &apiError{Status: res.StatusCode, Body: strings.TrimSpace(string(payload))}
	}
	return payload, nil
}

// httpBaseURLFromAddr mirrors wsURLFromAddr for the HTTP API: a bare ":8080"
// becomes "http://127.0.0.1:8080", a scheme-less host:port gains http://, and
// explicit http(s) URLs pass through with the trailing slash trimmed.
func httpBaseURLFromAddr(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + strings.TrimRight(addr, "/")
}
