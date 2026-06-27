package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadUsesDotEnvValues(t *testing.T) {
	t.Setenv("ORBIS_ADDR", ":9999")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`
ORBIS_ADDR=:8081
ORBIS_DATA_DIR=tmp-data
ORBIS_LLM_PROVIDER=openai
ORBIS_LLM_MODEL=gpt-test
ORBIS_RUN_TIMEOUT=250ms
OPENAI_API_KEY=test-key
OPENAI_BASE_URL=https://api.openai.test
`)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Addr != ":8081" {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, ":8081")
	}
	if cfg.DataDir != "tmp-data" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "tmp-data")
	}
	if cfg.LLMProvider != "openai" {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, "openai")
	}
	if cfg.LLMModel != "gpt-test" {
		t.Fatalf("LLMModel = %q, want %q", cfg.LLMModel, "gpt-test")
	}
	if cfg.OpenAIAPIKey != "test-key" {
		t.Fatalf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "test-key")
	}
	if cfg.OpenAIBaseURL != "https://api.openai.test" {
		t.Fatalf("OpenAIBaseURL = %q, want %q", cfg.OpenAIBaseURL, "https://api.openai.test")
	}
	if cfg.RunTimeout != 250*time.Millisecond {
		t.Fatalf("RunTimeout = %v, want 250ms", cfg.RunTimeout)
	}
}

func TestLoadRequiresRealLLMSettings(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`
ORBIS_ADDR=:8080
ORBIS_DATA_DIR=data
ORBIS_LLM_PROVIDER=openai
`)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_, err := Load(envPath)
	if err == nil {
		t.Fatal("Load() error = nil, want missing LLM settings error")
	}
}
