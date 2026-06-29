package skill

import "strings"

// ContextTag is the delimiter wrapping selected skill content in the LLM
// request instructions, so the model can clearly distinguish procedural skill
// knowledge from the conversation.
const ContextTag = "orbis_skills"

// BuildContext renders the given skill bodies into a single delimited block for
// the LLM request Instructions. Empty bodies are skipped, and it returns "" when
// nothing remains so the caller can leave Instructions unset.
func BuildContext(bodies []string) string {
	nonEmpty := make([]string, 0, len(bodies))
	for _, b := range bodies {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<" + ContextTag + ">\n")
	sb.WriteString(strings.Join(nonEmpty, "\n\n"))
	sb.WriteString("\n</" + ContextTag + ">")
	return sb.String()
}
