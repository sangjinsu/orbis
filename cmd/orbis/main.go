package main

import (
	"fmt"
	"log/slog"
	"os"

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
		slog.Info("orbis server bootstrap", "addr", cfg.Addr, "data_dir", cfg.DataDir, "llm_provider", cfg.LLMProvider, "llm_model", cfg.LLMModel)
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: orbis serve")
}
