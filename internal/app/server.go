package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sangjinsu/orbis/internal/auth"
	"github.com/sangjinsu/orbis/internal/broker"
	"github.com/sangjinsu/orbis/internal/config"
	"github.com/sangjinsu/orbis/internal/gateway"
	orbisruntime "github.com/sangjinsu/orbis/internal/runtime"
	"github.com/sangjinsu/orbis/internal/skill"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/tool"
	"github.com/sangjinsu/orbis/internal/worker"
)

// NewHTTPServer builds the HTTP server and returns the runtime service so the
// caller can drain it on shutdown (server.Shutdown then runtime.Close).
func NewHTTPServer(cfg config.Config) (*http.Server, *RuntimeService, error) {
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
	var skillCatalog SkillCatalog
	if cfg.SkillsEnabled {
		skillStore, err := skill.NewStore(cfg.SkillsDir)
		if err != nil {
			return nil, nil, fmt.Errorf("load skills from %s: %w", cfg.SkillsDir, err)
		}
		skillIndex = skillStore
		skillBodies = skillStore
		skillCatalog = skillStore
	}

	// Skill learning (v2): reviewable proposals plus an audit trail. Runtime
	// data lives under data/ (never .workspace); nil when learning is disabled.
	var proposalStore *skill.ProposalStore
	var auditLog *skill.AuditLog
	var promoter *skill.Promoter
	if cfg.SkillLearningEnabled {
		ps, err := skill.NewProposalStore(cfg.SkillProposalsDir)
		if err != nil {
			return nil, nil, fmt.Errorf("init skill proposals at %s: %w", cfg.SkillProposalsDir, err)
		}
		proposalStore = ps
		auditLog = skill.NewAuditLog(cfg.SkillAuditPath)
		// Promotion writes into the active skills directory, so it additionally
		// requires skills to be enabled.
		if cfg.SkillsEnabled {
			promoter = skill.NewPromoter(cfg.SkillsDir)
		}
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

	// The enabled tool schemas are advertised to the LLM and their names also feed
	// skill selection (skills whose related tools are enabled rank higher).
	toolSchemas := registry.SchemasForLLM(enabledToolsets)
	toolNames := make([]string, 0, len(toolSchemas))
	for _, s := range toolSchemas {
		toolNames = append(toolNames, s.Name)
	}

	authenticator := auth.New(cfg.AuthTokens)
	runtime := NewRuntimeService(RuntimeServiceConfig{
		Store:            fileStore,
		Broker:           eventBroker,
		LLMProvider:      provider,
		ToolRunner:       toolWorker,
		ToolSchemas:      toolSchemas,
		SkillBodies:      skillBodies,
		SkillCatalog:     skillCatalog,
		ProposalStore:    proposalStore,
		AuditLog:         auditLog,
		Promoter:         promoter,
		Authenticator:    authenticator,
		SkillAutoPropose: cfg.SkillAutoPropose,
		ReducerConfig: orbisruntime.ReducerConfig{
			ToolTimeout:               cfg.ToolTimeoutDefault,
			Retry:                     retryPolicy,
			ToolDenialContinuationMax: cfg.ToolDenialContinuationMax,
			SkillsEnabled:             cfg.SkillsEnabled,
			SkillIndex:                skillIndex,
			SkillSelect: skill.SelectConfig{
				MaxSelected: cfg.SkillsMaxSelected,
				MaxChars:    cfg.SkillsMaxChars,
			},
			ToolNames: toolNames,
		},
		RunTimeout: cfg.RunTimeout,
	})
	handlerOpts := []gateway.HandlerOption{
		gateway.WithBroker(eventBroker),
		gateway.WithReadTimeout(cfg.WSReadTimeout),
		gateway.WithAuth(authenticator),
	}
	// Expose the read-only skill HTTP endpoints only when skills are enabled, so
	// /skills 404s in a skills-disabled deployment.
	if cfg.SkillsEnabled {
		handlerOpts = append(handlerOpts, gateway.WithSkills(runtime))
	}
	// Expose the skill-proposal review endpoints only when learning is enabled.
	if proposalStore != nil {
		handlerOpts = append(handlerOpts, gateway.WithSkillLearning(runtime))
	}
	return &http.Server{
		Addr:    cfg.Addr,
		Handler: gateway.NewHTTPHandler(runtime, handlerOpts...),
	}, runtime, nil
}

// ensureDataDirs creates the runtime data directories so the first write of each
// kind does not race on directory creation and so an operator can inspect them.
func ensureDataDirs(root string) {
	for _, dir := range []string{"events", "sessions", "runs", "traces", "tool_calls", "snapshots"} {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
}
