package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/sangjinsu/orbis/internal/protocol"
)

// skillsMain dispatches `orbis skills <list|get|reload>`. Positional arguments
// come before flags because std flag stops parsing at the first non-flag.
func skillsMain(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return usagef("orbis skills list | get <skillID> | reload  [-addr] [-token] [-json] [-timeout]")
	}
	sub, rest := args[0], args[1:]

	var id string
	if sub == "get" {
		if len(rest) < 1 || strings.HasPrefix(rest[0], "-") {
			return usagef("orbis skills get <skillID> [-addr] [-json]")
		}
		id, rest = rest[0], rest[1:]
	}

	fs := flag.NewFlagSet("skills "+sub, flag.ContinueOnError)
	common := registerCommon(fs)
	if err := fs.Parse(rest); err != nil {
		return errUsage
	}
	client := common.client()

	switch sub {
	case "list":
		return runSkillsList(ctx, client, common.asJSON, out)
	case "get":
		return runSkillsGet(ctx, client, id, common.asJSON, out)
	case "reload":
		return runSkillsReload(ctx, client, common.asJSON, out)
	default:
		return usagef("orbis skills list | get <skillID> | reload")
	}
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
