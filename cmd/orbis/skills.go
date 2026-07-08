package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/sangjinsu/orbis/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Inspect and reload the skill catalog",
	}
	common := registerCommon(cmd)
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List the active skills",
			Args:  exactArgs(0, "orbis skills list"),
			RunE: func(c *cobra.Command, _ []string) error {
				return runSkillsList(c.Context(), common.client(), common.asJSON, c.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "get <skillID>",
			Short: "Show one skill including its body",
			Args:  exactArgs(1, "orbis skills get <skillID>"),
			RunE: func(c *cobra.Command, args []string) error {
				return runSkillsGet(c.Context(), common.client(), args[0], common.asJSON, c.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "reload",
			Short: "Reload the skill index from disk (admin)",
			Args:  exactArgs(0, "orbis skills reload"),
			RunE: func(c *cobra.Command, _ []string) error {
				return runSkillsReload(c.Context(), common.client(), common.asJSON, c.OutOrStdout())
			},
		},
	)
	return cmd
}

func runSkillsList(ctx context.Context, c *apiClient, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodGet, "/skills", nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var payload protocol.SkillListPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode skills list: %w", err)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tVERSION\tPRIORITY\tTITLE")
	for _, s := range payload.Skills {
		fmt.Fprintf(w, "%s\t%s\tv%s\t%d\t%s\n", s.ID, s.Status, s.Version, s.Priority, s.Title)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d skills\n", len(payload.Skills))
	return nil
}

func runSkillsGet(ctx context.Context, c *apiClient, id string, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodGet, "/skills/"+id, nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var payload protocol.SkillDetailPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode skill detail: %w", err)
	}
	fmt.Fprintf(out, "id:            %s\n", payload.ID)
	fmt.Fprintf(out, "title:         %s\n", payload.Title)
	fmt.Fprintf(out, "status:        %s (v%s, priority %d)\n", payload.Status, payload.Version, payload.Priority)
	if len(payload.Tags) > 0 {
		fmt.Fprintf(out, "tags:          %s\n", strings.Join(payload.Tags, ", "))
	}
	if len(payload.RelatedTools) > 0 {
		fmt.Fprintf(out, "related_tools: %s\n", strings.Join(payload.RelatedTools, ", "))
	}
	fmt.Fprintf(out, "chars:         %d\n", payload.Chars)
	fmt.Fprintf(out, "content_hash:  %s\n", payload.ContentHash)
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, strings.TrimRight(payload.Body, "\n"))
	return nil
}

func runSkillsReload(ctx context.Context, c *apiClient, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodPost, "/skills/reload", nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var payload protocol.SkillReloadPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode reload response: %w", err)
	}
	fmt.Fprintf(out, "reloaded: %d skills\n", payload.Count)
	return nil
}
