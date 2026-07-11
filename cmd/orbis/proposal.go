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

func newProposalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proposal",
		Short: "Review skill proposals (list, edit, approve, reject)",
	}
	common := registerCommon(cmd)

	var status string
	list := &cobra.Command{
		Use:   "list",
		Short: "List proposals, optionally filtered by status",
		Args:  exactArgs(0, "orbis proposal list [--status s]"),
		RunE: func(c *cobra.Command, _ []string) error {
			return runProposalList(c.Context(), common.client(), status, common.asJSON, c.OutOrStdout())
		},
	}
	list.Flags().StringVar(&status, "status", "", "filter by status (pending, approved, rejected, promoted, failed)")

	get := &cobra.Command{
		Use:   "get <proposalID>",
		Short: "Show one proposal including its rendered body",
		Args:  exactArgs(1, "orbis proposal get <proposalID>"),
		RunE: func(c *cobra.Command, args []string) error {
			return runProposalGet(c.Context(), common.client(), args[0], common.asJSON, c.OutOrStdout())
		},
	}

	create := &cobra.Command{
		Use:   "create <runID>",
		Short: "Create a proposal from a completed run (reviewer+)",
		Args:  exactArgs(1, "orbis proposal create <runID>"),
		RunE: func(c *cobra.Command, args []string) error {
			return runProposalCreate(c.Context(), common.client(), args[0], common.asJSON, c.OutOrStdout())
		},
	}

	var title, purpose, whenToUse string
	var requiredContext, procedure, relatedTools, verification, pitfalls stringList
	edit := &cobra.Command{
		Use:   "edit <proposalID>",
		Short: "Edit a pending proposal's structured fields (reviewer+)",
		Args:  exactArgs(1, "orbis proposal edit <proposalID> [field flags]"),
		RunE: func(c *cobra.Command, args []string) error {
			var fields protocol.SkillProposalUpdateRequest
			if c.Flags().Changed("title") {
				fields.Title = &title
			}
			if c.Flags().Changed("purpose") {
				fields.Purpose = &purpose
			}
			if c.Flags().Changed("when-to-use") {
				fields.WhenToUse = &whenToUse
			}
			fields.RequiredContext = requiredContext.toPtr()
			fields.Procedure = procedure.toPtr()
			fields.RelatedTools = relatedTools.toPtr()
			fields.Verification = verification.toPtr()
			fields.Pitfalls = pitfalls.toPtr()
			if fields == (protocol.SkillProposalUpdateRequest{}) {
				return fmt.Errorf("%w: orbis proposal edit <id> needs at least one field flag (--title, --purpose, --when-to-use, --required-context, --procedure, --related-tools, --verification, --pitfalls)", errUsage)
			}
			return runProposalEdit(c.Context(), common.client(), args[0], fields, common.asJSON, c.OutOrStdout())
		},
	}
	edit.Flags().StringVar(&title, "title", "", "replace the title")
	edit.Flags().StringVar(&purpose, "purpose", "", "replace the purpose")
	edit.Flags().StringVar(&whenToUse, "when-to-use", "", "replace the when-to-use guidance")
	edit.Flags().Var(&requiredContext, "required-context", "replace required context (repeatable; one empty value clears)")
	edit.Flags().Var(&procedure, "procedure", "replace procedure steps (repeatable; one empty value clears)")
	edit.Flags().Var(&relatedTools, "related-tools", "replace related tools (repeatable; one empty value clears)")
	edit.Flags().Var(&verification, "verification", "replace verification steps (repeatable; one empty value clears)")
	edit.Flags().Var(&pitfalls, "pitfalls", "replace pitfalls (repeatable; one empty value clears)")

	approve := &cobra.Command{
		Use:   "approve <proposalID>",
		Short: "Approve and promote a pending proposal (reviewer+)",
		Args:  exactArgs(1, "orbis proposal approve <proposalID>"),
		RunE: func(c *cobra.Command, args []string) error {
			return runProposalApprove(c.Context(), common.client(), args[0], common.asJSON, c.OutOrStdout())
		},
	}

	var reason string
	reject := &cobra.Command{
		Use:   "reject <proposalID>",
		Short: "Reject a pending proposal (reviewer+)",
		Args:  exactArgs(1, "orbis proposal reject <proposalID> [--reason r]"),
		RunE: func(c *cobra.Command, args []string) error {
			return runProposalReject(c.Context(), common.client(), args[0], reason, common.asJSON, c.OutOrStdout())
		},
	}
	reject.Flags().StringVar(&reason, "reason", "", "rejection reason")

	cmd.AddCommand(list, get, create, edit, approve, reject)
	return cmd
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
