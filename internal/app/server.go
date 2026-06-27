package app

import (
	"net/http"

	"github.com/sangjinsu/orbis/internal/config"
	"github.com/sangjinsu/orbis/internal/gateway"
	"github.com/sangjinsu/orbis/internal/store"
	"github.com/sangjinsu/orbis/internal/worker"
)

func NewHTTPServer(cfg config.Config) *http.Server {
	fileStore := store.NewFileStore(cfg.DataDir)
	provider := worker.NewOpenAIProvider(worker.OpenAIProviderConfig{
		APIKey:  cfg.OpenAIAPIKey,
		BaseURL: cfg.OpenAIBaseURL,
		Model:   cfg.LLMModel,
	})
	runtime := NewRuntimeService(RuntimeServiceConfig{
		Store:       fileStore,
		LLMProvider: provider,
	})
	return &http.Server{
		Addr:    cfg.Addr,
		Handler: gateway.NewHTTPHandler(runtime),
	}
}
