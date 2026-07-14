package agent

import (
	"context"
	"fmt"

	"github.com/liut/morign/pkg/services/llm"
)

// StreamCallbacks holds callbacks for streaming agent responses.
type StreamCallbacks struct {
	OnDelta func(delta string)
	OnThink func(think string)
}

// StreamChat runs the agent loop in streaming mode, invoking callbacks for each
// delta and think event. Returns the full accumulated answer text.
func StreamChat(ctx context.Context, loop *AgentLoop, messages []llm.Message, tools []llm.ToolDefinition, cb StreamCallbacks) (string, error) {
	var fullAnswer string
	for event, err := range loop.Run(ctx, messages, tools) {
		if err != nil {
			return fullAnswer, fmt.Errorf("stream chat: %w", err)
		}
		fullAnswer += event.Delta
		if event.Delta != "" && cb.OnDelta != nil {
			cb.OnDelta(event.Delta)
		}
		if event.Think != "" && cb.OnThink != nil {
			cb.OnThink(event.Think)
		}
		if event.StopReason == llm.FinishReasonToolCalls {
			cb.OnThink("\n")
		}
	}
	return fullAnswer, nil
}
