package skill

// SkillEventPayload is the payload for per-skill lifecycle events
// (SkillSelected, SkillLoaded, SkillSkipped). Fields are omitted when empty so
// each event only carries the data relevant to its stage.
type SkillEventPayload struct {
	SkillID      string `json:"skill_id"`
	SkillName    string `json:"skill_name,omitempty"`
	SkillVersion string `json:"skill_version,omitempty"`
	Score        int    `json:"score,omitempty"`
	Reason       string `json:"reason,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	Chars        int    `json:"chars,omitempty"`
}

// SkillAppliedPayload summarizes the set of skills applied to a run's LLM
// context. It is emitted once per run when skills are applied.
type SkillAppliedPayload struct {
	SkillIDs   []string `json:"skill_ids"`
	Count      int      `json:"count"`
	TotalChars int      `json:"total_chars"`
}
