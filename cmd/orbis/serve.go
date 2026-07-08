package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/sangjinsu/orbis/internal/app"
	"github.com/sangjinsu/orbis/internal/config"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the orbis server (loads .env)",
		Args:  exactArgs(0, "orbis serve"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(".env")
			if err != nil {
				return err
			}
			server, runtime, err := app.NewHTTPServer(cfg)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			slog.Info("orbis server starting", "addr", cfg.Addr, "data_dir", cfg.DataDir, "llm_provider", cfg.LLMProvider, "llm_model", cfg.LLMModel)
			serveErr := make(chan error, 1)
			go func() { serveErr <- server.ListenAndServe() }()

			select {
			case err := <-serveErr:
				// The HTTP server stopped on its own (bind failure, etc.). Drain the
				// runtime before exiting so in-flight background writes finish.
				runtime.Close()
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
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
			return nil
		},
	}
}
