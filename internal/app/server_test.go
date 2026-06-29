package app

import (
	"testing"

	"github.com/sangjinsu/orbis/internal/config"
)

func TestNewHTTPServerReturnsRuntimeAndClosesCleanly(t *testing.T) {
	cfg := config.Config{
		Addr:         ":0",
		DataDir:      t.TempDir(),
		LLMProvider:  "openai",
		LLMModel:     "gpt-test",
		OpenAIAPIKey: "test-key",
		// SkillsEnabled defaults to false so the test does not depend on a
		// data/skills directory in the working tree.
	}

	server, runtime, err := NewHTTPServer(cfg)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("server is nil, want a configured *http.Server")
	}
	if runtime == nil {
		t.Fatal("runtime is nil, want the RuntimeService for graceful shutdown")
	}

	// Close drains background goroutines and must be safe to call repeatedly.
	runtime.Close()
	runtime.Close()
}
