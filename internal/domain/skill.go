package domain

// SkillRef is a stable reference to a skill that was selected for a run. It is
// recorded on the run snapshot for prompt stability and later inspection, and
// carries enough metadata to identify the exact skill body version that was
// applied without embedding the body text itself.
type SkillRef struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Chars       int    `json:"chars,omitempty"`
}
