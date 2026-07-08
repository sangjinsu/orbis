package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sangjinsu/orbis/internal/auth"
)

type Config struct {
	Addr          string
	DataDir       string
	LLMProvider   string
	LLMModel      string
	RunTimeout    time.Duration
	OpenAIAPIKey  string
	OpenAIBaseURL string

	// Tool calling (v0.2). Defaults are safe: only the "safe" toolset, dangerous
	// tools denied, bounded timeouts, and at most one retry.
	Toolsets               string
	ToolTimeoutDefault     time.Duration
	ToolTimeoutMax         time.Duration
	ToolRetryMaxAttempts   int
	ToolRetryInitialDelay  time.Duration
	ToolRetryMaxDelay      time.Duration
	ToolRetryBackoffFactor float64

	// ToolDenialContinuationMax (v1.5) bounds how many times a run continues after
	// a tool-policy rejection by feeding the denial back to the LLM. 0 fails the
	// run on the first rejection (v0.2 behavior).
	ToolDenialContinuationMax int

	// WSReadTimeout bounds how long a WebSocket read may block. 0 disables it,
	// which is the default because subscriber connections idle between events.
	WSReadTimeout time.Duration

	// Skills (v1). Procedural knowledge loaded into the LLM context before
	// planning. Enabled by default; disabling skips skill selection entirely.
	SkillsEnabled       bool
	SkillsDir           string
	SkillsMaxSelected   int
	SkillsMaxChars      int
	SkillsReloadOnStart bool

	// Skill learning (v2). The runtime may create reviewable skill proposals
	// from runs; promotion always requires human approval (never automatic).
	// AuthTokens guards mutating endpoints — empty leaves them disabled. It is
	// parsed from ORBIS_AUTH_TOKENS (name:role:token, comma-separated); the
	// legacy ORBIS_ADMIN_TOKEN is merged in as the admin-role token "admin".
	SkillLearningEnabled bool
	SkillProposalsDir    string
	SkillAuditPath       string
	AuthTokens           []auth.TokenEntry
	SkillAutoPropose     bool
}

func Load(path string) (Config, error) {
	if path == "" {
		path = ".env"
	}

	values := map[string]string{}
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if ok {
			values[key] = value
		}
	}

	fileValues, err := parseDotEnv(path)
	if err != nil {
		return Config{}, err
	}
	for key, value := range fileValues {
		values[key] = value
	}

	runTimeout, err := parseDurationValue(values["ORBIS_RUN_TIMEOUT"])
	if err != nil {
		return Config{}, err
	}

	toolTimeoutDefault, err := durationOrDefault(values, "ORBIS_TOOL_TIMEOUT_DEFAULT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	toolTimeoutMax, err := durationOrDefault(values, "ORBIS_TOOL_TIMEOUT_MAX", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	toolRetryInitialDelay, err := durationOrDefault(values, "ORBIS_TOOL_RETRY_INITIAL_DELAY", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	toolRetryMaxDelay, err := durationOrDefault(values, "ORBIS_TOOL_RETRY_MAX_DELAY", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	toolRetryMaxAttempts, err := intOrDefault(values, "ORBIS_TOOL_RETRY_MAX_ATTEMPTS", 2)
	if err != nil {
		return Config{}, err
	}
	toolRetryBackoffFactor, err := floatOrDefault(values, "ORBIS_TOOL_RETRY_BACKOFF", 2.0)
	if err != nil {
		return Config{}, err
	}
	toolDenialContinuationMax, err := intOrDefault(values, "ORBIS_TOOL_DENIAL_CONTINUATION_MAX", 2)
	if err != nil {
		return Config{}, err
	}
	wsReadTimeout, err := durationOrDefault(values, "ORBIS_WS_READ_TIMEOUT", 0)
	if err != nil {
		return Config{}, err
	}

	skillsEnabled, err := boolOrDefault(values, "ORBIS_SKILLS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	skillsMaxSelected, err := intOrDefault(values, "ORBIS_SKILLS_MAX_SELECTED", 3)
	if err != nil {
		return Config{}, err
	}
	skillsMaxChars, err := intOrDefault(values, "ORBIS_SKILLS_MAX_CHARS", 12000)
	if err != nil {
		return Config{}, err
	}
	skillsReloadOnStart, err := boolOrDefault(values, "ORBIS_SKILLS_RELOAD_ON_START", true)
	if err != nil {
		return Config{}, err
	}
	skillLearningEnabled, err := boolOrDefault(values, "ORBIS_SKILL_LEARNING_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	skillAutoPropose, err := boolOrDefault(values, "ORBIS_SKILL_AUTO_PROPOSE", false)
	if err != nil {
		return Config{}, err
	}
	authTokens, err := loadAuthTokens(values)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:          valueOrDefault(values, "ORBIS_ADDR", ":8080"),
		DataDir:       valueOrDefault(values, "ORBIS_DATA_DIR", "data"),
		LLMProvider:   valueOrDefault(values, "ORBIS_LLM_PROVIDER", "openai"),
		LLMModel:      values["ORBIS_LLM_MODEL"],
		RunTimeout:    runTimeout,
		OpenAIAPIKey:  values["OPENAI_API_KEY"],
		OpenAIBaseURL: valueOrDefault(values, "OPENAI_BASE_URL", "https://api.openai.com"),

		Toolsets:               valueOrDefault(values, "ORBIS_TOOLSETS", "safe"),
		ToolTimeoutDefault:     toolTimeoutDefault,
		ToolTimeoutMax:         toolTimeoutMax,
		ToolRetryMaxAttempts:   toolRetryMaxAttempts,
		ToolRetryInitialDelay:  toolRetryInitialDelay,
		ToolRetryMaxDelay:      toolRetryMaxDelay,
		ToolRetryBackoffFactor: toolRetryBackoffFactor,

		ToolDenialContinuationMax: toolDenialContinuationMax,

		WSReadTimeout: wsReadTimeout,

		SkillsEnabled:       skillsEnabled,
		SkillsDir:           valueOrDefault(values, "ORBIS_SKILLS_DIR", "data/skills"),
		SkillsMaxSelected:   skillsMaxSelected,
		SkillsMaxChars:      skillsMaxChars,
		SkillsReloadOnStart: skillsReloadOnStart,

		SkillLearningEnabled: skillLearningEnabled,
		SkillProposalsDir:    valueOrDefault(values, "ORBIS_SKILL_PROPOSALS_DIR", "data/skill_proposals"),
		SkillAuditPath:       valueOrDefault(values, "ORBIS_SKILL_AUDIT_PATH", "data/audit/skill_audit.jsonl"),
		AuthTokens:           authTokens,
		SkillAutoPropose:     skillAutoPropose,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadAuthTokens parses ORBIS_AUTH_TOKENS and merges the legacy
// ORBIS_ADMIN_TOKEN as an admin-role entry named "admin". A name collision
// with a configured "admin" entry fails loudly rather than silently shadowing
// either token.
func loadAuthTokens(values map[string]string) ([]auth.TokenEntry, error) {
	entries, err := auth.ParseTokens(values["ORBIS_AUTH_TOKENS"])
	if err != nil {
		return nil, err
	}
	legacy := strings.TrimSpace(values["ORBIS_ADMIN_TOKEN"])
	if legacy == "" {
		return entries, nil
	}
	for _, entry := range entries {
		if entry.Name == "admin" {
			return nil, errors.New("ORBIS_ADMIN_TOKEN conflicts with an ORBIS_AUTH_TOKENS entry named \"admin\"; drop one of them")
		}
		if entry.Token == legacy {
			return nil, errors.New("ORBIS_ADMIN_TOKEN duplicates a token in ORBIS_AUTH_TOKENS; drop one of them")
		}
	}
	return append(entries, auth.TokenEntry{Name: "admin", Role: auth.RoleAdmin, Token: legacy}), nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.LLMModel) == "" {
		return errors.New("ORBIS_LLM_MODEL is required")
	}
	if c.LLMProvider == "openai" && strings.TrimSpace(c.OpenAIAPIKey) == "" {
		return errors.New("OPENAI_API_KEY is required for ORBIS_LLM_PROVIDER=openai")
	}
	return nil
}

func parseDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty key", path, lineNumber)
		}
		values[key] = trimEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return values, nil
}

func trimEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func valueOrDefault(values map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(values[key]); value != "" {
		return value
	}
	return fallback
}

func parseDurationValue(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse ORBIS_RUN_TIMEOUT: %w", err)
	}
	return duration, nil
}

func durationOrDefault(values map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return duration, nil
}

func intOrDefault(values map[string]string, key string, fallback int) (int, error) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func floatOrDefault(values map[string]string, key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func boolOrDefault(values map[string]string, key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
