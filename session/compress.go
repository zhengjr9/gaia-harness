package session

import (
	"context"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

// TokenCompressor keeps the newest transcript that fits the model window.
// It uses a conservative character/token estimate and never drops the newest turn.
type TokenCompressor struct {
	ReserveOutput int
	SummaryPrefix string
}

func (c TokenCompressor) Compress(_ context.Context, r Record) ([]provider.Message, error) {
	limit := r.Model.ContextWindow - c.ReserveOutput
	if limit <= 0 {
		return r.Messages, nil
	}
	used := 0
	kept := make([]provider.Message, 0, len(r.Messages))
	for i := len(r.Messages) - 1; i >= 0; i-- {
		cost := messageTokens(r.Messages[i])
		if used+cost > limit && len(kept) > 0 {
			break
		}
		kept = append(kept, r.Messages[i])
		used += cost
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	if len(kept) < len(r.Messages) && c.SummaryPrefix != "" {
		kept = append([]provider.Message{{Role: provider.RoleSystem, Content: []provider.Content{{Text: c.SummaryPrefix}}}}, kept...)
	}
	return kept, nil
}
func messageTokens(m provider.Message) int {
	n := len(string(m.Role)) + len(m.ToolCallID)
	for _, c := range m.Content {
		n += len(c.Text) + len(c.Thinking)
		if c.ToolCall != nil {
			n += len(c.ToolCall.Name) + len(c.ToolCall.Arguments)
		}
		if c.ToolResult != nil {
			n += len(c.ToolResult.Content)
		}
	}
	return (n + 3) / 4
}
