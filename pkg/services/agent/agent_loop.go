package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/liut/morign/pkg/services/llm"
)

// AgentLoopOption is a functional option for AgentLoopConfig.
type AgentLoopOption func(*AgentLoopConfig)

// WithMaxLoop sets the maximum number of loop iterations before safety-net termination.
func WithMaxLoop(n int) AgentLoopOption {
	return func(c *AgentLoopConfig) {
		c.MaxLoop = n
	}
}

// AgentLoopConfig holds configuration for AgentLoop.
type AgentLoopConfig struct {
	LLM      llm.Client    // LLM client for Chat and StreamChat
	ToolExec *ToolExecutor // tool executor (same package)
	MaxLoop  int           // safety net: max loop iterations, default 5
}

// AgentLoop encapsulates the full agent loop: LLM call + tool execution + iteration.
type AgentLoop struct {
	cfg AgentLoopConfig
}

// NewAgentLoop creates a new AgentLoop with the given config and options.
// Defaults MaxLoop to 5 when unset or <= 0.
func NewAgentLoop(cfg AgentLoopConfig, opts ...AgentLoopOption) *AgentLoop {
	if cfg.MaxLoop <= 0 {
		cfg.MaxLoop = 5
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &AgentLoop{cfg: cfg}
}

// Run executes the streaming agent loop, returning an iter.Seq2 that yields
// events from StreamChat and tool execution. The caller consumes events via
// `for event, err := range loop.Run(ctx, messages, tools)`.
//
// The iterator runs synchronously within the caller's goroutine. It stops when:
//   - LLM returns no tool calls (final answer)
//   - all executed tools signal terminate=true
//   - maxLoop iterations are exceeded (yields error event)
//   - StreamChat returns an error (yields error event)
//   - the caller breaks out of the range loop
func (al *AgentLoop) Run(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return func(yield func(*llm.Event, error) bool) {
		for iter := 0; iter < al.cfg.MaxLoop; iter++ {
			var roundThink strings.Builder
			var toolCalls []llm.ToolCall

			// Consume stream events from LLM
			for event, err := range al.cfg.LLM.StreamChat(ctx, messages, tools) {
				if err != nil {
					yield(nil, fmt.Errorf("stream chat: %w", err))
					return
				}
				roundThink.WriteString(event.Think)

				if !yield(event, nil) {
					return // caller stopped consuming
				}

				if event.Done {
					toolCalls = event.ToolCalls
				}
			}

			// No tool calls → final answer delivered, we're done
			if len(toolCalls) == 0 {
				return
			}

			// Execute tool calls and yield tool result events
			events, updatedMsgs, allTerminate := al.cfg.ToolExec.ExecuteToolCalls(
				ctx, messages, toolCalls, roundThink.String(),
			)
			messages = updatedMsgs
			for _, ev := range events {
				if !yield(ev, nil) {
					return // caller stopped consuming
				}
			}

			// All tools requested termination → no more LLM calls needed
			if allTerminate {
				return
			}
		}

		// Safety net: max loop iterations exceeded
		yield(nil, fmt.Errorf("max loop iterations (%d) exceeded", al.cfg.MaxLoop))
	}
}

// RunNonStreaming executes the non-streaming agent loop, returning the final
// answer string. It calls Chat, collects tool calls, executes tools, and loops
// until no tool calls remain or all tools signal terminate.
func (al *AgentLoop) RunNonStreaming(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, error) {
	for iter := 0; iter < al.cfg.MaxLoop; iter++ {
		result, err := al.cfg.LLM.Chat(ctx, messages, tools)
		if err != nil {
			return "", fmt.Errorf("chat: %w", err)
		}

		// No tool calls → return the content as final answer
		if len(result.ToolCalls) == 0 {
			return result.Content, nil
		}

		// Execute tool calls
		_, updatedMsgs, allTerminate := al.cfg.ToolExec.ExecuteToolCalls(
			ctx, messages, result.ToolCalls, result.Thinking,
		)
		messages = updatedMsgs

		if allTerminate {
			return result.Content, nil
		}
	}

	return "", fmt.Errorf("max loop iterations (%d) exceeded", al.cfg.MaxLoop)
}
