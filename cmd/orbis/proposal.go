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

const proposalUsage = "orbis proposal list [-status <s>] | get <id> | create <runID> | edit <id> [field flags] | approve <id> | reject <id> [-reason <r>]  [-addr] [-token] [-json] [-timeout]"

// proposalMain dispatches `orbis proposal <verb>`. Positional ids come before
// flags because std flag stops parsing at the first non-flag.
func proposalMain(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return usagef(proposalUsage)
	}
	sub, rest := args[0], args[1:]

	var id string
	switch sub {
	case "get", "create", "edit", "approve", "reject":
		if len(rest) < 1 || strings.HasPrefix(rest[0], "-") {
			return usagef("orbis proposal %s <id> [flags]", sub)
		}
		id, rest = rest[0], rest[1:]
	}

	fs := flag.NewFlagSet("proposal "+sub, flag.ContinueOnError)
	common := registerCommon(fs)
	status := fs.String("status", "", "filter by status (pending, approved, rejected, promoted, failed)")
	reason := fs.String("reason", "", "rejection reason")
	title := fs.String("title", "", "replace the title")
	purpose := fs.String("purpose", "", "replace the purpose")
	whenToUse := fs.String("when-to-use", "", "replace the when-to-use guidance")
	var requiredContext, procedure, relatedTools, verification, pitfalls stringList
	fs.Var(&requiredContext, "required-context", "replace required context (repeatable; one empty value clears)")
	fs.Var(&procedure, "procedure", "replace procedure steps (repeatable; one empty value clears)")
	fs.Var(&relatedTools, "related-tools", "replace related tools (repeatable; one empty value clears)")
	fs.Var(&verification, "verification", "replace verification steps (repeatable; one empty value clears)")
	fs.Var(&pitfalls, "pitfalls", "replace pitfalls (repeatable; one empty value clears)")
	if err := fs.Parse(rest); err != nil {
		return errUsage
	}
	client := common.client()

	switch sub {
	case "list":
		return runProposalList(ctx, client, *status, common.asJSON, out)
	case "get":
		return runProposalGet(ctx, client, id, common.asJSON, out)
	case "create":
		return runProposalCreate(ctx, client, id, common.asJSON, out)
	case "edit":
		var fields protocol.SkillProposalUpdateRequest
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "title":
				fields.Title = title
			case "purpose":
				fields.Purpose = purpose
			case "when-to-use":
				fields.WhenToUse = whenToUse
			}
		})
		fields.RequiredContext = requiredContext.toPtr()
		fields.Procedure = procedure.toPtr()
		fields.RelatedTools = relatedTools.toPtr()
		fields.Verification = verification.toPtr()
		fields.Pitfalls = pitfalls.toPtr()
		if fields == (protocol.SkillProposalUpdateRequest{}) {
			return usagef("orbis proposal edit <id> needs at least one field flag (-title, -purpose, -when-to-use, -required-context, -procedure, -related-tools, -verification, -pitfalls)")
		}
		return runProposalEdit(ctx, client, id, fields, common.asJSON, out)
	case "approve":
		return runProposalApprove(ctx, client, id, common.asJSON, out)
	case "reject":
		return runProposalReject(ctx, client, id, *reason, common.asJSON, out)
	default:
		return usagef(proposalUsage)
	}
}

func runProposalList(ctx context.Context, c *apiClient, status string, asJSON bool, out io.Writer) error {
	path := "/skill-proposals"
	if status != "" {
		path += "?status=" + status
	}
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var payload protocol.SkillProposalListPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode proposal list: %w", err)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROPOSAL\tSTATUS\tREV\tVERSION\tSOURCE_RUN\tTITLE")
	for _, p := range payload.Proposals {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", p.ProposalID, p.Status, p.Revision, p.Version, p.SourceRunID, p.Title)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d proposals\n", len(payload.Proposals))
	return nil
}

func runProposalGet(ctx context.Context, c *apiClient, id string, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodGet, "/skill-proposals/"+id, nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var p protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode proposal detail: %w", err)
	}
	fmt.Fprintf(out, "proposal:   %s (revision %d)\n", p.ProposalID, p.Revision)
	fmt.Fprintf(out, "status:     %s\n", p.Status)
	fmt.Fprintf(out, "skill_id:   %s\n", p.SkillID)
	fmt.Fprintf(out, "source_run: %s\n", p.SourceRunID)
	if p.PromotedSkillID != "" {
		fmt.Fprintf(out, "promoted:   %s (v%s)\n", p.PromotedSkillID, p.Version)
	}
	if p.RationaleSummary != "" {
		fmt.Fprintf(out, "rationale:  %s\n", p.RationaleSummary)
	}
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, strings.TrimRight(p.Body, "\n"))
	return nil
}

func runProposalCreate(ctx context.Context, c *apiClient, runID string, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodPost, "/runs/"+runID+"/skill-proposals", nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var p protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode created proposal: %w", err)
	}
	fmt.Fprintf(out, "created %s (skill_id=%s, status=%s) from %s\n", p.ProposalID, p.SkillID, p.Status, runID)
	return nil
}

func runProposalEdit(ctx context.Context, c *apiClient, id string, fields protocol.SkillProposalUpdateRequest, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodPatch, "/skill-proposals/"+id, fields)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var p protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode updated proposal: %w", err)
	}
	fmt.Fprintf(out, "updated %s (revision %d): %s\n", p.ProposalID, p.Revision, strings.Join(editedFieldNames(fields), ", "))
	return nil
}

// editedFieldNames lists the wire names of the fields a PATCH carried, for the
// human-readable confirmation line.
func editedFieldNames(fields protocol.SkillProposalUpdateRequest) []string {
	var names []string
	if fields.Title != nil {
		names = append(names, "title")
	}
	if fields.Purpose != nil {
		names = append(names, "purpose")
	}
	if fields.WhenToUse != nil {
		names = append(names, "when_to_use")
	}
	if fields.RequiredContext != nil {
		names = append(names, "required_context")
	}
	if fields.Procedure != nil {
		names = append(names, "procedure")
	}
	if fields.RelatedTools != nil {
		names = append(names, "related_tools")
	}
	if fields.Verification != nil {
		names = append(names, "verification")
	}
	if fields.Pitfalls != nil {
		names = append(names, "pitfalls")
	}
	return names
}

func runProposalApprove(ctx context.Context, c *apiClient, id string, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodPost, "/skill-proposals/"+id+"/approve", nil)
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var p protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode approved proposal: %w", err)
	}
	fmt.Fprintf(out, "approved %s (status=%s, promoted_skill_id=%s, version=%s)\n", p.ProposalID, p.Status, p.PromotedSkillID, p.Version)
	return nil
}

func runProposalReject(ctx context.Context, c *apiClient, id, reason string, asJSON bool, out io.Writer) error {
	body, err := c.do(ctx, http.MethodPost, "/skill-proposals/"+id+"/reject", map[string]string{"reason": reason})
	if err != nil {
		return err
	}
	if asJSON {
		printRawJSON(out, body)
		return nil
	}
	var p protocol.SkillProposalDetailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode rejected proposal: %w", err)
	}
	fmt.Fprintf(out, "rejected %s (status=%s)\n", p.ProposalID, p.Status)
	return nil
}
