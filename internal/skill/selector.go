package skill

import (
	"sort"
	"strings"

	"github.com/sangjinsu/orbis/internal/domain"
)

// SelectConfig bounds skill selection.
type SelectConfig struct {
	MaxSelected int
	MaxChars    int
}

// SelectionInput is the deterministic input to skill selection.
type SelectionInput struct {
	Text      string
	ToolNames []string
}

// Selected is a chosen skill with the score and reason that justified it, used
// by the reducer to record the run snapshot and emit skill lifecycle events.
type Selected struct {
	Ref    domain.SkillRef
	Score  int
	Reason string
}

// Score weights. Triggers are the strongest signal, then exact name, then tags
// and related tools mentioned in the query.
const (
	scoreTrigger     = 10
	scoreName        = 6
	scoreTag         = 5
	scoreRelatedTool = 4
	scoreTitle       = 3
)

// Select deterministically scores the snapshot against the input and returns the
// top skills within the MaxSelected and MaxChars budgets. Selection is a pure
// function: the same snapshot + input + cfg always yields the same result, and
// it never calls an LLM. Skills with no positive signal are not selected, so an
// unrelated query returns an empty slice.
func Select(snapshot []Entry, in SelectionInput, cfg SelectConfig) []Selected {
	text := strings.ToLower(in.Text)

	candidates := make([]scoredEntry, 0, len(snapshot))
	for _, e := range snapshot {
		if e.Status != "" && e.Status != "active" {
			continue
		}
		score, reason := scoreEntry(e, text)
		if score <= 0 {
			continue
		}
		candidates = append(candidates, scoredEntry{entry: e, score: score, reason: reason})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].entry.Priority != candidates[j].entry.Priority {
			return candidates[i].entry.Priority > candidates[j].entry.Priority
		}
		return candidates[i].entry.ID < candidates[j].entry.ID
	})

	maxSelected := cfg.MaxSelected
	if maxSelected <= 0 {
		maxSelected = 3
	}

	selected := make([]Selected, 0, maxSelected)
	usedChars := 0
	for _, c := range candidates {
		if len(selected) >= maxSelected {
			break
		}
		// Stop at the first skill that would overflow the character budget so the
		// most relevant contiguous set is kept. A non-positive budget is unlimited.
		if cfg.MaxChars > 0 && usedChars+c.entry.Chars > cfg.MaxChars {
			break
		}
		usedChars += c.entry.Chars
		selected = append(selected, Selected{Ref: c.entry.Ref(), Score: c.score, Reason: c.reason})
	}
	return selected
}

type scoredEntry struct {
	entry  Entry
	score  int
	reason string
}

// scoreEntry scores one skill against the lowercased query text. Matching is
// substring-based on triggers, tags, related tool names, and the skill
// name/title. Description text is intentionally not free-text matched in v1 to
// keep selection predictable and avoid spurious matches.
func scoreEntry(e Entry, text string) (int, string) {
	score := 0
	reasons := make([]string, 0, 4)

	if n := countContains(text, e.Triggers); n > 0 {
		score += scoreTrigger * n
		reasons = append(reasons, "trigger")
	}
	if name := strings.ToLower(strings.TrimSpace(e.Name)); name != "" && strings.Contains(text, name) {
		score += scoreName
		reasons = append(reasons, "name")
	}
	if n := countContains(text, e.Tags); n > 0 {
		score += scoreTag * n
		reasons = append(reasons, "tag")
	}
	if n := countContains(text, e.RelatedTools); n > 0 {
		score += scoreRelatedTool * n
		reasons = append(reasons, "related_tool")
	}
	if title := strings.ToLower(strings.TrimSpace(e.Title)); title != "" && strings.Contains(text, title) {
		score += scoreTitle
		reasons = append(reasons, "title")
	}

	return score, strings.Join(reasons, ",")
}

// countContains returns how many of the values appear as a substring of text.
func countContains(text string, values []string) int {
	count := 0
	for _, v := range values {
		needle := strings.ToLower(strings.TrimSpace(v))
		if needle != "" && strings.Contains(text, needle) {
			count++
		}
	}
	return count
}
