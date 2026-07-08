package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newRootCmd assembles the orbis command tree. Usage and runtime errors are
// separated by the errUsage sentinel: main maps it to exit code 2, everything
// else to exit code 1 (the pre-cobra convention).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "orbis",
		Short:         "Orbis agent runtime: server, smoke clients, and the skill learning loop CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", errUsage, err)
	})
	root.AddCommand(
		newServeCmd(),
		newWSCmd(),
		newSkillsCmd(),
		newProposalCmd(),
		newWatchCmd(),
		newChatCmd(),
	)
	return root
}

// exactArgs is cobra.ExactArgs wrapped in the errUsage sentinel so positional
// mistakes exit 2 with a one-line hint instead of a runtime error.
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("%w: %s", errUsage, usage)
		}
		return nil
	}
}
