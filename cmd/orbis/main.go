package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
		server, runtime, err := app.NewHTTPServer(cfg)
		if err != nil {
			slog.Error("build server", "error", err)
			os.Exit(1)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		slog.Info("orbis server starting", "addr", cfg.Addr, "data_dir", cfg.DataDir, "llm_provider", cfg.LLMProvider, "llm_model", cfg.LLMModel)
		serveErr := make(chan error, 1)
		go func() { serveErr <- server.ListenAndServe() }()

		select {
		case err := <-serveErr:
			// The HTTP server stopped on its own (bind failure, etc.). Drain the
			// runtime before exiting so in-flight background writes finish.
			runtime.Close()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("server stopped", "error", err)
				os.Exit(1)
			}
		case <-ctx.Done():
			// SIGINT/SIGTERM: stop accepting HTTP, then drain background goroutines.
			slog.Info("orbis server shutting down")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := server.Shutdown(shutdownCtx); err != nil {
				slog.Error("server shutdown", "error", err)
			}
			cancel()
			runtime.Close()
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
		if len(os.Args) >= 4 {
			switch os.Args[3] {
			case "tool":
				smokeCfg = toolSmokeConfigFromEnv(cfg)
			case "skill":
				smokeCfg = skillSmokeConfigFromEnv(cfg)
			}
		}
		if err := runWSSmoke(context.Background(), smokeCfg, os.Stdout); err != nil {
			slog.Error("websocket smoke failed", "error", err)
			os.Exit(1)
		}
	case "skills", "proposal", "watch":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		var err error
		switch os.Args[1] {
		case "skills":
			err = skillsMain(ctx, os.Args[2:], os.Stdout)
		case "proposal":
			err = proposalMain(ctx, os.Args[2:], os.Stdout)
		case "watch":
			err = watchMain(ctx, os.Args[2:], os.Stdout)
		}
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		if err != nil {
			slog.Error(os.Args[1]+" failed", "error", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  orbis serve
  orbis ws smoke [tool|skill]
  orbis skills list | get <skillID> | reload
  orbis proposal list | get <id> | create <runID> | edit <id> | approve <id> | reject <id>
  orbis watch

common flags: -addr <host:port> -token <bearer> -json -timeout <dur>
environment:  ORBIS_ADDR (server address), ORBIS_TOKEN (bearer token)`)
}
