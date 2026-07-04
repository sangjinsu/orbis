package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NewProposalFromRun deterministically renders a reviewable skill proposal from
// run facts. The same facts always produce the same proposal (and content
// hash): no LLM is consulted and no hidden reasoning is stored — only the
// structured sections a reviewer needs plus a concise rationale summary.
func NewProposalFromRun(proposalID string, facts RunFacts, reason string, now time.Time) SkillProposal {
	goal := strings.TrimSpace(facts.UserGoal)
	tools := uniqueStrings(facts.ToolsUsed)

	title := "Learned workflow from run " + facts.RunID
	if goal != "" {
		title = "Learned workflow: " + truncate(goal, 60)
	}

	purpose := fmt.Sprintf("Reproduce the workflow that completed run %s.", facts.RunID)
	whenToUse := "When a request resembles the original goal of this workflow."
	if goal != "" {
		purpose = fmt.Sprintf("Reproduce the workflow that completed run %s: %s", facts.RunID, truncate(goal, 200))
		whenToUse = fmt.Sprintf("When a request resembles: %q", truncate(goal, 120))
	}

	required := []string{"The user's goal or request text."}
	if len(tools) > 0 {
		required = append(required, "Access to the tools: "+strings.Join(tools, ", ")+".")
	}

	procedure := []string{}
	if goal != "" {
		procedure = append(procedure, "Restate the goal: "+truncate(goal, 120)+".")
	}
	for _, tool := range tools {
		procedure = append(procedure, fmt.Sprintf("Propose a `%s` tool call with the arguments the task requires.", tool))
	}
	for _, ref := range facts.SkillsUsed {
		procedure = append(procedure, fmt.Sprintf("Apply the `%s` skill guidance.", ref.ID))
	}
	procedure = append(procedure, "Return the final answer once the results satisfy the goal.")

	verification := []string{"The run reaches RunCompleted."}
	for _, tool := range tools {
		verification = append(verification, fmt.Sprintf("A ToolCallSucceeded event is observed for `%s`.", tool))
	}

	pitfalls := []string{}
	if facts.ToolFailures > 0 {
		pitfalls = append(pitfalls, fmt.Sprintf("Tool calls failed %d time(s) before succeeding; expect retries and keep idempotency keys stable.", facts.ToolFailures))
	}
	pitfalls = append(pitfalls, "This skill is procedural knowledge only — never execute side effects from it; propose tool calls instead.")

	rationale := fmt.Sprintf("Run %s completed using %d tool call(s) and %d skill(s); detected: %s.",
		facts.RunID, len(facts.ToolsUsed), len(facts.SkillsUsed), reason)

	proposal := SkillProposal{
		ProposalID:       proposalID,
		SourceRunID:      facts.RunID,
		SourceEventIDs:   facts.SourceEventIDs,
		Title:            title,
		SkillID:          proposedSkillID(goal, facts.RunID),
		Purpose:          purpose,
		WhenToUse:        whenToUse,
		RequiredContext:  required,
		Procedure:        procedure,
		RelatedTools:     tools,
		Verification:     verification,
		Pitfalls:         pitfalls,
		RationaleSummary: rationale,
		Status:           ProposalPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	proposal.Body = renderProposalBody(proposal)
	proposal.ContentHash = contentHash(proposal.Body)
	return proposal
}

// renderProposalBody renders the markdown body that would become the skill file
// on promotion, in the same section format as the seed skills.
func renderProposalBody(p SkillProposal) string {
	var b strings.Builder
	b.WriteString("# " + p.Title + "\n")
	writeSection(&b, "Purpose", []string{p.Purpose})
	writeSection(&b, "When To Use", []string{p.WhenToUse})
	writeList(&b, "Required Context", p.RequiredContext)
	writeSteps(&b, "Procedure", p.Procedure)
	writeList(&b, "Related Tools", p.RelatedTools)
	writeList(&b, "Verification", p.Verification)
	writeList(&b, "Pitfalls", p.Pitfalls)
	return b.String()
}

func writeSection(b *strings.Builder, heading string, lines []string) {
	b.WriteString("\n## " + heading + "\n\n")
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
}

func writeList(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n## " + heading + "\n\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
}

func writeSteps(b *strings.Builder, heading string, steps []string) {
	if len(steps) == 0 {
		return
	}
	b.WriteString("\n## " + heading + "\n\n")
	for i, step := range steps {
		fmt.Fprintf(b, "%d. %s\n", i+1, step)
	}
}

// proposedSkillID derives a deterministic, filesystem- and index-safe skill id
// from the goal text plus a short run-id hash for uniqueness, e.g.
// "learned-websocket-runtime-test-a1b2c3". Only ASCII letters and digits are
// kept so the id stays safe as a file name and index key regardless of the
// goal's language.
func proposedSkillID(goal, runID string) string {
	words := []string{}
	current := strings.Builder{}
	for _, r := range strings.ToLower(goal) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
		if len(words) >= 4 {
			break
		}
	}
	if current.Len() > 0 && len(words) < 4 {
		words = append(words, current.String())
	}
	sum := sha256.Sum256([]byte(runID))
	suffix := hex.EncodeToString(sum[:3])
	slug := strings.Join(words, "-")
	if slug == "" {
		return "learned-run-" + suffix
	}
	if len(slug) > 48 {
		slug = slug[:48]
	}
	return "learned-" + slug + "-" + suffix
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// uniqueStrings returns the input without duplicates, preserving first-seen order.
func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
