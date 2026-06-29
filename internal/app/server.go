package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sangjinsu/orbis/internal/broker"
	"github.com/sangjinsu/orbis/internal/config"
	"github.com/sangjinsu/orbis/internal/gateway"
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

func NewHTTPServer(cfg config.Config) (*http.Server, error) {
	ensureDataDirs(cfg.DataDir)

	fileStore := store.NewFileStore(cfg.DataDir)
	eventBroker := broker.New()
	provider := worker.NewOpenAIProvider(worker.OpenAIProviderConfig{
		APIKey:  cfg.OpenAIAPIKey,
		BaseURL: cfg.OpenAIBaseURL,
		Model:   cfg.LLMModel,
	})

	// Load the skill store once at startup; the reducer selects from its
	// in-memory snapshot and the dispatcher renders bodies, so this is the only
	// skill disk I/O. When skills are disabled the index/bodies stay nil and the
	// reducer skips selection, preserving v0.2 behavior.
	var skillIndex skill.Index
	var skillBodies skill.Bodies
	if cfg.SkillsEnabled {
		skillStore, err := skill.NewStore(cfg.SkillsDir)
		if err != nil {
			return nil, fmt.Errorf("load skills from %s: %w", cfg.SkillsDir, err)
		}
		skillIndex = skillStore
		skillBodies = skillStore
	}

	registry := tool.NewRegistry()
	_ = tool.RegisterMockTools(registry, nil)
	enabledToolsets := tool.ParseToolsets(cfg.Toolsets)
	retryPolicy := tool.RetryPolicy{
		MaxAttempts:       cfg.ToolRetryMaxAttempts,
		InitialDelay:      cfg.ToolRetryInitialDelay,
		MaxDelay:          cfg.ToolRetryMaxDelay,
		BackoffMultiplier: cfg.ToolRetryBackoffFactor,
	}
	policy := tool.NewPolicy(registry, tool.PolicyConfig{
		AllowedToolsets: enabledToolsets,
		AllowDangerous:  false,
		MaxTimeout:      cfg.ToolTimeoutMax,
	})
	toolWorker := worker.NewToolWorker(worker.ToolWorkerConfig{
		Registry:       registry,
		Policy:         policy,
		Store:          fileStore,
		DefaultTimeout: cfg.ToolTimeoutDefault,
		RetryPolicy:    retryPolicy,
	})

	runtime := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		Broker:      eventBroker,
		LLMProvider: provider,
		ToolRunner:  toolWorker,
		ToolSchemas: registry.SchemasForLLM(enabledToolsets),
		SkillBodies: skillBodies,
		ReducerConfig: orbisruntime.ReducerConfig{
			ToolTimeout:   cfg.ToolTimeoutDefault,
			Retry:         retryPolicy,
			SkillsEnabled: cfg.SkillsEnabled,
			SkillIndex:    skillIndex,
			SkillSelect: skill.SelectConfig{
				MaxSelected: cfg.SkillsMaxSelected,
				MaxChars:    cfg.SkillsMaxChars,
			},
		},
		RunTimeout: cfg.RunTimeout,
	})
	return &http.Server{
		Addr: cfg.Addr,
		Handler: gateway.NewHTTPHandler(runtime,
			gateway.WithBroker(eventBroker),
			gateway.WithReadTimeout(cfg.WSReadTimeout),
		),
	}, nil
}

// ensureDataDirs creates the runtime data directories so the first write of each
// kind does not race on directory creation and so an operator can inspect them.
func ensureDataDirs(root string) {
	for _, dir := range []string{"events", "sessions", "runs", "traces", "tool_calls", "snapshots"} {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
}
