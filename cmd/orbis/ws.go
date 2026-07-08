package main

import (
	"fmt"

	"github.com/sangjinsu/orbis/internal/config"
	"github.com/spf13/cobra"
)

func newWSCmd() *cobra.Command {
	ws := &cobra.Command{
		Use:   "ws",
		Short: "WebSocket clients",
	}
	smoke := &cobra.Command{
		Use:   "smoke [tool|skill]",
		Short: "Run a real-LLM smoke test against the server from .env",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 || (len(args) == 1 && args[0] != "tool" && args[0] != "skill") {
				return fmt.Errorf("%w: orbis ws smoke [tool|skill]", errUsage)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(".env")
			if err != nil {
				return err
			}
			smokeCfg := smokeConfigFromEnv(cfg)
			if len(args) == 1 {
				switch args[0] {
				case "tool":
					smokeCfg = toolSmokeConfigFromEnv(cfg)
				case "skill":
					smokeCfg = skillSmokeConfigFromEnv(cfg)
				}
			}
			return runWSSmoke(cmd.Context(), smokeCfg, cmd.OutOrStdout())
		},
	}
	ws.AddCommand(smoke)
	return ws
}
