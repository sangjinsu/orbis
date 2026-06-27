package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/sangjinsu/orbis/internal/broker"
	"github.com/sangjinsu/orbis/internal/config"
	"github.com/sangjinsu/orbis/internal/gateway"
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

func NewHTTPServer(cfg config.Config) *http.Server {
	ensureDataDirs(cfg.DataDir)

	fileStore := store.NewFileStore(cfg.DataDir)
	eventBroker := broker.New()
	provider := worker.NewOpenAIProvider(worker.OpenAIProviderConfig{
		APIKey:  cfg.OpenAIAPIKey,
		BaseURL: cfg.OpenAIBaseURL,
		Model:   cfg.LLMModel,
	})

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
		ReducerConfig: orbisruntime.ReducerConfig{
			ToolTimeout: cfg.ToolTimeoutDefault,
			Retry:       retryPolicy,
		},
		RunTimeout: cfg.RunTimeout,
	})
	return &http.Server{
		Addr:    cfg.Addr,
		Handler: gateway.NewHTTPHandler(runtime, gateway.WithBroker(eventBroker)),
	}
}

// ensureDataDirs creates the runtime data directories so the first write of each
// kind does not race on directory creation and so an operator can inspect them.
func ensureDataDirs(root string) {
	for _, dir := range []string{"events", "sessions", "runs", "traces", "tool_calls", "snapshots"} {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
}
