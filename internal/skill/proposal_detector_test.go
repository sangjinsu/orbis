package skill

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sangjinsu/orbis/internal/domain"
)

func runEvent(id string, typ domain.EventType, runID, payload string) domain.Event {
	return domain.Event{
		EventID:   id,
		SessionID: "session_1",
		RunID:     runID,
		Type:      typ,
		Payload:   json.RawMessage(payload),
	}
}

func completedRunFixture() (domain.RunState, []domain.Event) {
	run := domain.RunState{
		RunID:          "run_1",
		SessionID:      "session_1",
		Status:         domain.RunCompleted,
		SelectedSkills: []domain.SkillRef{{ID: "tool-calling-policy"}},
	}
	events := []domain.Event{
		runEvent("evt_user", domain.EventUserMessageReceived, "run_1", `{"text":"add 1 and 2 with math.add"}`),
		// Another run's event must be ignored.
		runEvent("evt_other", domain.EventToolCallSucceeded, "run_2", `{"tool_name":"echo"}`),
		runEvent("evt_fail", domain.EventToolCallFailed, "run_1", `{"tool_name":"math.add"}`),
		runEvent("evt_ok1", domain.EventToolCallSucceeded, "run_1", `{"tool_name":"math.add"}`),
		runEvent("evt_ok2", domain.EventToolCallSucceeded, "run_1", `{"tool_name":"math.add"}`),
		runEvent("evt_done", domain.EventRunCompleted, "run_1", `{}`),
	}
	return run, events
}

func TestBuildRunFactsFromEvents(t *testing.T) {
	run, events := completedRunFixture()
	facts := BuildRunFacts(run, events)

	if !facts.Completed {
		t.Fatal("Completed = false, want true")
	}
	if facts.UserGoal != "add 1 and 2 with math.add" {
		t.Fatalf("UserGoal = %q, want the run's user message", facts.UserGoal)
	}
	if len(facts.ToolsUsed) != 2 || facts.ToolsUsed[0] != "math.add" {
		t.Fatalf("ToolsUsed = %v, want two math.add successes", facts.ToolsUsed)
	}
	if facts.ToolFailures != 1 || !facts.RecoveredFailure {
		t.Fatalf("failures = %d recovered = %t, want 1/true", facts.ToolFailures, facts.RecoveredFailure)
	}
	if len(facts.SkillsUsed) != 1 || facts.SkillsUsed[0].ID != "tool-calling-policy" {
		t.Fatalf("SkillsUsed = %v, want the run snapshot's skill", facts.SkillsUsed)
	}
	// user + fail + ok1 + ok2 + done; the other run's event is excluded.
	if len(facts.SourceEventIDs) != 5 {
		t.Fatalf("SourceEventIDs = %v, want 5 run_1 events", facts.SourceEventIDs)
	}
}

func TestDetectCandidateTable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		facts  RunFacts
		want   bool
		reason string
	}{
		{"not completed", RunFacts{Completed: false, ToolsUsed: []string{"echo"}}, false, ""},
		{"tools used", RunFacts{Completed: true, ToolsUsed: []string{"echo"}}, true, reasonUsedTools},
		{"skills used", RunFacts{Completed: true, SkillsUsed: []domain.SkillRef{{ID: "x"}}}, true, reasonUsedSkills},
		{"recovered failure", RunFacts{Completed: true, ToolsUsed: []string{"echo"}, RecoveredFailure: true}, true, reasonRecoveredFailure},
		{"repeated tool", RunFacts{Completed: true, ToolsUsed: []string{"echo", "echo"}}, true, reasonRepeatedTool},
		{"explicit request only", RunFacts{Completed: true, ExplicitRequest: true}, true, reasonExplicitRequest},
		{"plain completed run", RunFacts{Completed: true}, false, ""},
	} {
		got, reason := DetectCandidate(tc.facts)
		if got != tc.want {
			t.Fatalf("%s: DetectCandidate = %t, want %t (%s)", tc.name, got, tc.want, reason)
		}
		if tc.want && !strings.Contains(reason, tc.reason) {
			t.Fatalf("%s: reason = %q, want it to contain %q", tc.name, reason, tc.reason)
		}
	}
}

func TestNewProposalFromRunIsDeterministic(t *testing.T) {
	run, events := completedRunFixture()
	facts := BuildRunFacts(run, events)
	facts.ExplicitRequest = true
	_, reason := DetectCandidate(facts)
	now := time.Unix(1700000000, 0).UTC()

	first := NewProposalFromRun("prop_run_1", facts, reason, now)
	second := NewProposalFromRun("prop_run_1", facts, reason, now)
	if first.Body != second.Body || first.ContentHash != second.ContentHash {
		t.Fatal("NewProposalFromRun is not deterministic for identical inputs")
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if first.Status != ProposalPending {
		t.Fatalf("Status = %q, want pending", first.Status)
	}
	if !strings.HasPrefix(first.SkillID, "learned-") {
		t.Fatalf("SkillID = %q, want learned- prefix", first.SkillID)
	}
	if !strings.Contains(first.Body, "## Procedure") || !strings.Contains(first.Body, "math.add") {
		t.Fatalf("Body missing procedure/tool sections:\n%s", first.Body)
	}
	if !strings.Contains(first.Body, "never execute side effects") {
		t.Fatalf("Body missing the no-side-effects pitfall:\n%s", first.Body)
	}
	if len(first.RelatedTools) != 1 || first.RelatedTools[0] != "math.add" {
		t.Fatalf("RelatedTools = %v, want deduplicated [math.add]", first.RelatedTools)
	}
	if !strings.Contains(first.RationaleSummary, reasonUsedTools) {
		t.Fatalf("RationaleSummary = %q, want detection reasons", first.RationaleSummary)
	}
}

func TestProposedSkillIDIsASCIISafe(t *testing.T) {
	facts := RunFacts{
		RunID:     "run_kr",
		Completed: true,
		UserGoal:  "웹소켓으로 런타임 테스트 방법 알려줘",
		ToolsUsed: []string{"echo"},
	}
	p := NewProposalFromRun("prop_run_kr", facts, reasonUsedTools, time.Unix(1700000000, 0).UTC())
	if !strings.HasPrefix(p.SkillID, "learned-") {
		t.Fatalf("SkillID = %q, want learned- prefix", p.SkillID)
	}
	for _, r := range p.SkillID {
		if r > 127 {
			t.Fatalf("SkillID %q contains non-ASCII rune %q", p.SkillID, r)
		}
	}
}
