package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

	// WSReadTimeout bounds how long a WebSocket read may block. 0 disables it,
	// which is the default because subscriber connections idle between events.
	WSReadTimeout time.Duration
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
	wsReadTimeout, err := durationOrDefault(values, "ORBIS_WS_READ_TIMEOUT", 0)
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

		WSReadTimeout: wsReadTimeout,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
