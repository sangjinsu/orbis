package runtime

import (
	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/worker"
)

// BuildLLMMessages converts persisted session history into provider-neutral LLM
// messages so a real provider can reconstruct the conversation, including
// assistant tool-call turns and tool outputs. It is a pure function: the
// reducer calls it to embed context into a DispatchLLMCall action payload
// without performing any side effects.
func BuildLLMMessages(state domain.SessionState) []worker.LLMMessage {
	messages := make([]worker.LLMMessage, 0, len(state.MessageHistory))
	for _, m := range state.MessageHistory {
		messages = append(messages, worker.LLMMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			ToolArgs:   m.ToolArgs,
		})
	}
	return messages
}
