package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/sangjinsu/orbis/internal/app"
	"github.com/sangjinsu/orbis/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		cfg, err := config.Load(".env")
		if err != nil {
			slog.Error("load config", "error", err)
			os.Exit(1)
		}
		server := app.NewHTTPServer(cfg)
		slog.Info("orbis server starting", "addr", cfg.Addr, "data_dir", cfg.DataDir, "llm_provider", cfg.LLMProvider, "llm_model", cfg.LLMModel)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case "ws":
		if len(os.Args) < 3 || os.Args[2] != "smoke" {
			printUsage()
			os.Exit(2)
		}
		cfg, err := config.Load(".env")
		if err != nil {
			slog.Error("load config", "error", err)
			os.Exit(1)
		}
		smokeCfg := smokeConfigFromEnv(cfg)
		if len(os.Args) >= 4 && os.Args[3] == "tool" {
			smokeCfg = toolSmokeConfigFromEnv(cfg)
		}
		if err := runWSSmoke(context.Background(), smokeCfg, os.Stdout); err != nil {
			slog.Error("websocket smoke failed", "error", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: orbis serve | orbis ws smoke [tool]")
}
