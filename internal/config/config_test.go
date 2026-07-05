package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/auth"
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

func TestLoadToolDefaults(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\n")
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Toolsets != "safe" {
		t.Fatalf("Toolsets = %q, want safe", cfg.Toolsets)
	}
	if cfg.ToolTimeoutDefault != 5*time.Second {
		t.Fatalf("ToolTimeoutDefault = %v, want 5s", cfg.ToolTimeoutDefault)
	}
	if cfg.ToolTimeoutMax != 30*time.Second {
		t.Fatalf("ToolTimeoutMax = %v, want 30s", cfg.ToolTimeoutMax)
	}
	if cfg.ToolRetryMaxAttempts != 2 {
		t.Fatalf("ToolRetryMaxAttempts = %d, want 2", cfg.ToolRetryMaxAttempts)
	}
	if cfg.ToolRetryInitialDelay != 500*time.Millisecond {
		t.Fatalf("ToolRetryInitialDelay = %v, want 500ms", cfg.ToolRetryInitialDelay)
	}
	if cfg.ToolRetryBackoffFactor != 2.0 {
		t.Fatalf("ToolRetryBackoffFactor = %v, want 2.0", cfg.ToolRetryBackoffFactor)
	}
}

func TestLoadSkillDefaults(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\n")
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SkillsEnabled {
		t.Fatal("SkillsEnabled = false, want true by default")
	}
	if cfg.SkillsDir != "data/skills" {
		t.Fatalf("SkillsDir = %q, want data/skills", cfg.SkillsDir)
	}
	if cfg.SkillsMaxSelected != 3 {
		t.Fatalf("SkillsMaxSelected = %d, want 3", cfg.SkillsMaxSelected)
	}
	if cfg.SkillsMaxChars != 12000 {
		t.Fatalf("SkillsMaxChars = %d, want 12000", cfg.SkillsMaxChars)
	}
	if !cfg.SkillsReloadOnStart {
		t.Fatal("SkillsReloadOnStart = false, want true by default")
	}
}

func TestLoadSkillOverrides(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`ORBIS_LLM_MODEL=gpt-test
OPENAI_API_KEY=test-key
ORBIS_SKILLS_ENABLED=false
ORBIS_SKILLS_DIR=/tmp/skills
ORBIS_SKILLS_MAX_SELECTED=5
ORBIS_SKILLS_MAX_CHARS=2000
ORBIS_SKILLS_RELOAD_ON_START=false
`)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SkillsEnabled {
		t.Fatal("SkillsEnabled = true, want false")
	}
	if cfg.SkillsDir != "/tmp/skills" {
		t.Fatalf("SkillsDir = %q, want /tmp/skills", cfg.SkillsDir)
	}
	if cfg.SkillsMaxSelected != 5 {
		t.Fatalf("SkillsMaxSelected = %d, want 5", cfg.SkillsMaxSelected)
	}
	if cfg.SkillsMaxChars != 2000 {
		t.Fatalf("SkillsMaxChars = %d, want 2000", cfg.SkillsMaxChars)
	}
	if cfg.SkillsReloadOnStart {
		t.Fatal("SkillsReloadOnStart = true, want false")
	}
}

func TestLoadSkillLearningDefaults(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SkillLearningEnabled {
		t.Fatal("SkillLearningEnabled = false, want true by default")
	}
	if cfg.SkillProposalsDir != "data/skill_proposals" {
		t.Fatalf("SkillProposalsDir = %q, want data/skill_proposals", cfg.SkillProposalsDir)
	}
	if cfg.SkillAuditPath != "data/audit/skill_audit.jsonl" {
		t.Fatalf("SkillAuditPath = %q, want data/audit/skill_audit.jsonl", cfg.SkillAuditPath)
	}
	if cfg.AuthTokens != nil {
		t.Fatalf("AuthTokens = %#v, want none by default (mutating endpoints disabled)", cfg.AuthTokens)
	}
	if cfg.SkillAutoPropose {
		t.Fatal("SkillAutoPropose = true, want false by default")
	}
}

func TestLoadSkillLearningOverrides(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`ORBIS_LLM_MODEL=gpt-test
OPENAI_API_KEY=test-key
ORBIS_SKILL_LEARNING_ENABLED=false
ORBIS_SKILL_PROPOSALS_DIR=/tmp/proposals
ORBIS_SKILL_AUDIT_PATH=/tmp/audit.jsonl
ORBIS_ADMIN_TOKEN=dev-orbis-admin
ORBIS_SKILL_AUTO_PROPOSE=true
`)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SkillLearningEnabled {
		t.Fatal("SkillLearningEnabled = true, want false")
	}
	if cfg.SkillProposalsDir != "/tmp/proposals" {
		t.Fatalf("SkillProposalsDir = %q, want /tmp/proposals", cfg.SkillProposalsDir)
	}
	if cfg.SkillAuditPath != "/tmp/audit.jsonl" {
		t.Fatalf("SkillAuditPath = %q, want /tmp/audit.jsonl", cfg.SkillAuditPath)
	}
	// The legacy ORBIS_ADMIN_TOKEN is synthesized into an admin entry.
	want := []auth.TokenEntry{{Name: "admin", Role: auth.RoleAdmin, Token: "dev-orbis-admin"}}
	if !reflect.DeepEqual(cfg.AuthTokens, want) {
		t.Fatalf("AuthTokens = %#v, want %#v", cfg.AuthTokens, want)
	}
	if !cfg.SkillAutoPropose {
		t.Fatal("SkillAutoPropose = false, want true")
	}
}

func TestLoadAuthTokens(t *testing.T) {
	load := func(t *testing.T, extra string) (Config, error) {
		t.Helper()
		envPath := filepath.Join(t.TempDir(), ".env")
		content := "ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\n" + extra
		if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}
		return Load(envPath)
	}

	t.Run("new tokens only", func(t *testing.T) {
		cfg, err := load(t, "ORBIS_AUTH_TOKENS=alice:admin:atok,bob:reviewer:rtok\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		want := []auth.TokenEntry{
			{Name: "alice", Role: auth.RoleAdmin, Token: "atok"},
			{Name: "bob", Role: auth.RoleReviewer, Token: "rtok"},
		}
		if !reflect.DeepEqual(cfg.AuthTokens, want) {
			t.Fatalf("AuthTokens = %#v, want %#v", cfg.AuthTokens, want)
		}
	})

	t.Run("both set merges the legacy admin token", func(t *testing.T) {
		cfg, err := load(t, "ORBIS_AUTH_TOKENS=bob:reviewer:rtok\nORBIS_ADMIN_TOKEN=legacy\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		want := []auth.TokenEntry{
			{Name: "bob", Role: auth.RoleReviewer, Token: "rtok"},
			{Name: "admin", Role: auth.RoleAdmin, Token: "legacy"},
		}
		if !reflect.DeepEqual(cfg.AuthTokens, want) {
			t.Fatalf("AuthTokens = %#v, want %#v", cfg.AuthTokens, want)
		}
	})

	t.Run("legacy conflicts fail loudly", func(t *testing.T) {
		if _, err := load(t, "ORBIS_AUTH_TOKENS=admin:admin:atok\nORBIS_ADMIN_TOKEN=legacy\n"); err == nil {
			t.Fatal("Load() error = nil, want admin-name conflict error")
		}
		if _, err := load(t, "ORBIS_AUTH_TOKENS=alice:admin:legacy\nORBIS_ADMIN_TOKEN=legacy\n"); err == nil {
			t.Fatal("Load() error = nil, want duplicate-token conflict error")
		}
	})

	t.Run("malformed tokens fail", func(t *testing.T) {
		if _, err := load(t, "ORBIS_AUTH_TOKENS=alice:root:atok\n"); err == nil {
			t.Fatal("Load() error = nil, want unknown-role error")
		}
	})
}

func TestLoadRejectsInvalidSkillBool(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\nORBIS_SKILLS_ENABLED=maybe\n")
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if _, err := Load(envPath); err == nil {
		t.Fatal("Load() error = nil, want error for invalid bool")
	}
}

func TestLoadToolOverrides(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`ORBIS_LLM_MODEL=gpt-test
OPENAI_API_KEY=test-key
ORBIS_TOOLSETS=safe,read
ORBIS_TOOL_TIMEOUT_DEFAULT=2s
ORBIS_TOOL_RETRY_MAX_ATTEMPTS=4
ORBIS_TOOL_RETRY_BACKOFF=1.5
`)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Toolsets != "safe,read" {
		t.Fatalf("Toolsets = %q, want safe,read", cfg.Toolsets)
	}
	if cfg.ToolTimeoutDefault != 2*time.Second {
		t.Fatalf("ToolTimeoutDefault = %v, want 2s", cfg.ToolTimeoutDefault)
	}
	if cfg.ToolRetryMaxAttempts != 4 {
		t.Fatalf("ToolRetryMaxAttempts = %d, want 4", cfg.ToolRetryMaxAttempts)
	}
	if cfg.ToolRetryBackoffFactor != 1.5 {
		t.Fatalf("ToolRetryBackoffFactor = %v, want 1.5", cfg.ToolRetryBackoffFactor)
	}
}

func TestLoadRejectsInvalidToolDuration(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\nORBIS_TOOL_TIMEOUT_DEFAULT=notaduration\n")
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if _, err := Load(envPath); err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}
}

func TestLoadToolDenialContinuationMax(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	if err := os.WriteFile(envPath, []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ToolDenialContinuationMax != 2 {
		t.Fatalf("ToolDenialContinuationMax = %d, want default 2", cfg.ToolDenialContinuationMax)
	}

	if err := os.WriteFile(envPath, []byte("ORBIS_LLM_MODEL=gpt-test\nOPENAI_API_KEY=test-key\nORBIS_TOOL_DENIAL_CONTINUATION_MAX=0\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	cfg, err = Load(envPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ToolDenialContinuationMax != 0 {
		t.Fatalf("ToolDenialContinuationMax = %d, want 0 override", cfg.ToolDenialContinuationMax)
	}
}
