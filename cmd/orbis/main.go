package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, errUsage) || strings.Contains(err.Error(), "unknown command") {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, "run 'orbis --help' for usage")
			os.Exit(2)
		}
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
