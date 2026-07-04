package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillDir creates a data/skills-style directory with the given index.json
// content and body files, returning the directory path.
func writeSkillDir(t *testing.T, index string, bodies map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	for name, content := range bodies {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	return dir
}

const validIndex = `{
  "skills": [
    {"id":"a","name":"a","title":"Alpha","tags":["x"],"triggers":["alpha"],"path":"a.md","version":"1.0.0","status":"active","priority":100,"related_tools":["math.add"]},
    {"id":"b","name":"b","title":"Beta","tags":["y"],"triggers":["beta"],"path":"b.md","version":"1.0.0","status":"active","priority":90}
  ]
}`

func TestStoreLoadsIndexAndBodies(t *testing.T) {
	dir := writeSkillDir(t, validIndex, map[string]string{
		"a.md": "# Alpha body",
		"b.md": "# Beta body",
	})

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	snap := store.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	body, ok := store.Body("a")
	if !ok || body != "# Alpha body" {
		t.Fatalf("Body(a) = %q, %v", body, ok)
	}
	entry, ok := store.Get("a")
	if !ok || entry.ContentHash == "" || entry.Chars == 0 {
		t.Fatalf("Get(a) = %+v, %v; want hash and chars set", entry, ok)
	}
	list := store.List()
	if len(list) != 2 || list[0].ID != "a" {
		t.Fatalf("List() = %+v, want priority-ordered with a first", list)
	}
}

func TestStoreInvalidIndexReturnsError(t *testing.T) {
	tests := []struct {
		name  string
		index string
		want  string
	}{
		{"malformed json", `{"skills": [`, "parse skill index"},
		{"missing id", `{"skills":[{"path":"a.md"}]}`, "missing id"},
		{"missing path", `{"skills":[{"id":"a"}]}`, "missing path"},
		{"duplicate id", `{"skills":[{"id":"a","path":"a.md"},{"id":"a","path":"b.md"}]}`, "duplicate id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeSkillDir(t, tc.index, map[string]string{"a.md": "x", "b.md": "y"})
			_, err := NewStore(dir)
			if err == nil {
				t.Fatal("NewStore() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want contains %q", err.Error(), tc.want)
			}
		})
	}
}

func TestStoreMissingBodyReturnsClearError(t *testing.T) {
	dir := writeSkillDir(t, validIndex, map[string]string{"a.md": "# Alpha body"}) // b.md missing
	_, err := NewStore(dir)
	if err == nil {
		t.Fatal("NewStore() error = nil, want missing-body error")
	}
	if !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "read skill body") {
		t.Fatalf("error = %q, want it to name the missing skill body", err.Error())
	}
}

func TestStoreEmptyBodyReturnsClearError(t *testing.T) {
	dir := writeSkillDir(t, validIndex, map[string]string{"a.md": "# Alpha body", "b.md": "   \n"})
	_, err := NewStore(dir)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("NewStore() error = %v, want empty-body error", err)
	}
}

func TestStoreReloadPicksUpChanges(t *testing.T) {
	dir := writeSkillDir(t, validIndex, map[string]string{"a.md": "v1", "b.md": "b"})
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("v2 updated"), 0o644); err != nil {
		t.Fatalf("rewrite a.md: %v", err)
	}
	if err := store.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if body, _ := store.Body("a"); body != "v2 updated" {
		t.Fatalf("Body(a) after reload = %q, want %q", body, "v2 updated")
	}
}

func entries() []Entry {
	return []Entry{
		{Metadata: Metadata{ID: "ws", Name: "ws", Title: "WebSocket", Tags: []string{"runtime"}, Triggers: []string{"websocket"}, Status: "active", Priority: 100, RelatedTools: []string{"session.message"}}, Chars: 100},
		{Metadata: Metadata{ID: "tool", Name: "tool", Title: "Tool Calling", Tags: []string{"policy"}, Triggers: []string{"tool calling"}, Status: "active", Priority: 90, RelatedTools: []string{"math.add"}}, Chars: 100},
		{Metadata: Metadata{ID: "reducer", Name: "reducer", Title: "Reducer", Tags: []string{"go"}, Triggers: []string{"reducer"}, Status: "active", Priority: 80}, Chars: 100},
	}
}

func ids(sel []Selected) []string {
	out := make([]string, 0, len(sel))
	for _, s := range sel {
		out = append(out, s.Ref.ID)
	}
	return out
}

func TestSelectMatchesExactTrigger(t *testing.T) {
	sel := Select(entries(), SelectionInput{Text: "How do I test over WebSocket?"}, SelectConfig{MaxSelected: 3})
	if got := ids(sel); len(got) != 1 || got[0] != "ws" {
		t.Fatalf("selected = %v, want [ws]", got)
	}
	if !strings.Contains(sel[0].Reason, "trigger") {
		t.Fatalf("reason = %q, want it to mention trigger", sel[0].Reason)
	}
}

func TestSelectMatchesTag(t *testing.T) {
	// "policy" matches the tool skill's tag but none of its triggers.
	sel := Select(entries(), SelectionInput{Text: "explain the policy please"}, SelectConfig{MaxSelected: 3})
	if got := ids(sel); len(got) != 1 || got[0] != "tool" {
		t.Fatalf("selected = %v, want [tool]", got)
	}
	if !strings.Contains(sel[0].Reason, "tag") {
		t.Fatalf("reason = %q, want it to mention tag", sel[0].Reason)
	}
}

func TestSelectMatchesRelatedToolMention(t *testing.T) {
	sel := Select(entries(), SelectionInput{Text: "please run math.add"}, SelectConfig{MaxSelected: 3})
	if got := ids(sel); len(got) != 1 || got[0] != "tool" {
		t.Fatalf("selected = %v, want [tool]", got)
	}
}

func TestSelectRespectsMaxSelected(t *testing.T) {
	// A query matching every skill, capped to 2.
	sel := Select(entries(), SelectionInput{Text: "websocket tool calling reducer"}, SelectConfig{MaxSelected: 2})
	if len(sel) != 2 {
		t.Fatalf("selected = %v, want 2", ids(sel))
	}
	// Results are ordered by score (desc); the cap drops the lowest-scoring skill.
	if sel[0].Score < sel[1].Score {
		t.Fatalf("results not score-ordered: %d before %d", sel[0].Score, sel[1].Score)
	}
}

func TestSelectNoMatchReturnsEmpty(t *testing.T) {
	sel := Select(entries(), SelectionInput{Text: "completely unrelated gardening question"}, SelectConfig{MaxSelected: 3})
	if len(sel) != 0 {
		t.Fatalf("selected = %v, want empty", ids(sel))
	}
}

func TestSelectRespectsMaxChars(t *testing.T) {
	in := SelectionInput{Text: "websocket tool calling reducer"}
	// Budget fits only the first 100-char skill.
	sel := Select(entries(), in, SelectConfig{MaxSelected: 3, MaxChars: 150})
	if len(sel) != 1 {
		t.Fatalf("selected = %v, want 1 within 150-char budget", ids(sel))
	}
	// Zero budget means unlimited.
	sel = Select(entries(), in, SelectConfig{MaxSelected: 3, MaxChars: 0})
	if len(sel) != 3 {
		t.Fatalf("selected = %v, want 3 with unlimited budget", ids(sel))
	}
}

func TestSelectSkipsInactive(t *testing.T) {
	es := entries()
	es[0].Status = "draft"
	sel := Select(es, SelectionInput{Text: "websocket"}, SelectConfig{MaxSelected: 3})
	if len(sel) != 0 {
		t.Fatalf("selected = %v, want empty (inactive skipped)", ids(sel))
	}
}

func TestSelectScoresAvailableRelatedTools(t *testing.T) {
	// Text matches nothing, but the tool skill's related tool (math.add) is enabled
	// for the run, so the availability signal selects it.
	in := SelectionInput{Text: "completely unrelated gardening question", ToolNames: []string{"math.add"}}
	sel := Select(entries(), in, SelectConfig{MaxSelected: 3})
	if got := ids(sel); len(got) != 1 || got[0] != "tool" {
		t.Fatalf("selected = %v, want [tool] from tool availability", got)
	}
	if !strings.Contains(sel[0].Reason, "tool_available") {
		t.Fatalf("reason = %q, want it to mention tool_available", sel[0].Reason)
	}

	// Without the available tool the same query matches nothing (no regression to
	// the text-only path).
	none := Select(entries(), SelectionInput{Text: "completely unrelated gardening question"}, SelectConfig{MaxSelected: 3})
	if len(none) != 0 {
		t.Fatalf("selected = %v, want empty without available tools", ids(none))
	}
}

func TestBuildContextIncludesContentAndDelimits(t *testing.T) {
	out := BuildContext([]string{"# Alpha\nbody", "# Beta\nbody"})
	if !strings.HasPrefix(out, "<orbis_skills>") || !strings.HasSuffix(out, "</orbis_skills>") {
		t.Fatalf("context not delimited: %q", out)
	}
	if !strings.Contains(out, "# Alpha") || !strings.Contains(out, "# Beta") {
		t.Fatalf("context missing skill bodies: %q", out)
	}
}

func TestBuildContextEmptyReturnsEmpty(t *testing.T) {
	if out := BuildContext(nil); out != "" {
		t.Fatalf("BuildContext(nil) = %q, want empty", out)
	}
	if out := BuildContext([]string{"", "   "}); out != "" {
		t.Fatalf("BuildContext(blank) = %q, want empty", out)
	}
}

// TestSeedSkillsSelectExpected validates the committed data/skills seed: it must
// load cleanly and the documented manual-test prompts must select the intended
// skill. The path is relative to this package (repo/internal/skill).
func TestSeedSkillsSelectExpected(t *testing.T) {
	const seedDir = "../../data/skills"
	if _, err := os.Stat(seedDir); err != nil {
		t.Skipf("seed skills dir not found: %v", err)
	}
	store, err := NewStore(seedDir)
	if err != nil {
		t.Fatalf("NewStore(seed) error = %v", err)
	}
	if len(store.Snapshot()) != 8 {
		t.Fatalf("seed skills = %d, want 8", len(store.Snapshot()))
	}

	tests := []struct {
		name   string
		text   string
		wantID string
	}{
		{"websocket", "WebSocket으로 Orbis 런타임 테스트 방법 알려줘", "websocket-runtime-test"},
		{"tool", "tool calling 정책에 맞게 math.add를 호출해줘", "tool-calling-policy"},
		{"reducer", "reducer를 어떻게 구현해야 해?", "go-reducer-pattern"},
		{"web search", "Use web_search to check the latest release details", "web-search"},
		{"docs lookup", "Check the official docs for this SDK configuration", "docs-lookup"},
		{"github search", "Search GitHub pull request history for this repository", "github-search"},
		{"runtime debug", "Use /debug to inspect the runtime event flow", "runtime-debug"},
		{"test plan", "Create a smoke test and acceptance test plan", "test-plan"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel := Select(store.Snapshot(), SelectionInput{Text: tc.text}, SelectConfig{MaxSelected: 3, MaxChars: 12000})
			if len(sel) == 0 {
				t.Fatalf("no skill selected for %q", tc.text)
			}
			if sel[0].Ref.ID != tc.wantID {
				t.Fatalf("top skill = %q, want %q (all=%v)", sel[0].Ref.ID, tc.wantID, ids(sel))
			}
		})
	}
}
