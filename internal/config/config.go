package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr          string
	DataDir       string
	LLMProvider   string
	LLMModel      string
	OpenAIAPIKey  string
	OpenAIBaseURL string
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

	cfg := Config{
		Addr:          valueOrDefault(values, "ORBIS_ADDR", ":8080"),
		DataDir:       valueOrDefault(values, "ORBIS_DATA_DIR", "data"),
		LLMProvider:   valueOrDefault(values, "ORBIS_LLM_PROVIDER", "openai"),
		LLMModel:      values["ORBIS_LLM_MODEL"],
		OpenAIAPIKey:  values["OPENAI_API_KEY"],
		OpenAIBaseURL: valueOrDefault(values, "OPENAI_BASE_URL", "https://api.openai.com"),
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
