package agent

import (
	"context"
	"strings"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
)

const (
	// DefaultSystemMsg is the fallback system prompt when no preset is configured.
	DefaultSystemMsg = "You are a helpful assistant. If you cannot find relevant information in the provided context to answer the user's question, please honestly state that you don't know rather than making up an answer."

	// DefaultToolsMsg is the fallback tool-usage prompt when no preset is configured.
	DefaultToolsMsg = "You will select the appropriate tool based on the user's question and call the tool to solve the problem. If the tool returns no relevant information, honestly state that you don't know rather than making up an answer. If the tool requires parameters, you must extract them from the user's question. Note that it is important to clearly distinguish between read and write operations. If a write operation is required by the tool, it must be explicitly stated in the user's question for writing purposes (such as adding, creating, appending, modifying, etc.), and all necessary parameters for the tool must be included in the user's question before calling; otherwise, treat it as a regular read operation or Q&A."

	// MemoryGuidance is the stable instruction that tells the model its
	// long-term memory exists and how to reach it. The memory index is not
	// injected into the prompt, so this block is what makes the model look.
	MemoryGuidance = "Long-term memory about the current user lives behind tools, not in this prompt. Call memory_list to see what is stored, memory_recall to search it by keyword, and memory_store to save durable facts the user shares. Check memory before answering questions about the user's preferences, history, or personal context."

	// ActivationHeader opens the message carrying an explicitly activated
	// skill, so it cannot be mistaken for the user's own words.
	ActivationHeader = "# Activated Skill"
)

// SystemPromptParts carries the stable system blocks, the trailing session
// constants and the optional explicit activation block.
//
// Everything ahead of SessionConstants is byte-identical for requests sharing
// a preset, a channel, a tool set and a visible-skill set, so it forms a prefix
// worth reusing. Values fixed for the life of one conversation — today only the
// user's display name — render last, so a new conversation or a different user
// only costs the tail, never the shared blocks.
//
// Data that changes per turn or per request stays out of the prompt entirely:
// time, the memory index and retrieved documents reach the model through tools.
// The only per-turn injection is an explicitly activated skill, which renders
// as a user message placed after the history and before the current question.
type SystemPromptParts struct {
	Base    string // preset system prompt
	Tools   string // tool-usage prompt
	Channel string // channel prompt
	Memory  string // memory usage guidance
	Skills  string // skills index, names and descriptions only

	// SessionConstants holds values fixed for the life of a conversation; today
	// that is the user's display name. It renders last, so it never breaks the
	// shared prefix ahead of it.
	SessionConstants string

	// Activation holds the full text of a skill the caller explicitly
	// activated (the /skill command). It is per-turn content, so it renders as
	// a user message rather than living in the system prompt.
	Activation string
}

// Normalize applies the defaults that depend on whether tools are available.
// Tool prompts are only rendered when there is at least one tool.
func (p SystemPromptParts) Normalize(hasTools bool) SystemPromptParts {
	if p.Base == "" {
		p.Base = DefaultSystemMsg
	}
	if !hasTools {
		p.Tools = ""
	} else if p.Tools == "" {
		p.Tools = DefaultToolsMsg
	}
	return p
}

// StableMessage renders the system message. Consecutive turns with the same
// preset, channel, tool set and visible skills produce byte-identical content.
func (p SystemPromptParts) StableMessage() llm.Message {
	return llm.Message{
		Role:    llm.RoleSystem,
		Content: joinBlocks([]string{p.Base, p.Tools, p.Channel, p.Memory, p.Skills, p.SessionConstants}),
	}
}

// ActivatedMessage renders an explicitly activated skill as a user message, or
// nil when nothing was activated.
func (p SystemPromptParts) ActivatedMessage() *llm.Message {
	content := joinBlocks([]string{ActivationHeader, p.Activation})
	if content == ActivationHeader {
		return nil
	}
	return &llm.Message{Role: llm.RoleUser, Content: content}
}

// joinBlocks trims each block and joins the non-empty ones with a newline.
func joinBlocks(blocks []string) string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return strings.Join(out, "\n")
}

// ConvertMCPToolsToLLMTools converts MCP tool descriptors to LLM tool definitions.
func ConvertMCPToolsToLLMTools(tools []mcps.ToolDescriptor) []llm.ToolDefinition {
	result := make([]llm.ToolDefinition, 0, len(tools))
	for _, td := range tools {
		result = append(result, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDefinition{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.InputSchema,
			},
		})
	}
	return result
}

// BuildSystemMessage resolves the tool registry against the prompt parts and
// returns the stable system message and the resolved tool definitions. Callers
// assemble [stable][history][activation][question].
func BuildSystemMessage(ctx context.Context, toolreg *tools.Registry, parts SystemPromptParts) (llm.Message, []llm.ToolDefinition) {
	toolDefs := ConvertMCPToolsToLLMTools(toolreg.ToolsFor(ctx))
	parts = parts.Normalize(len(toolDefs) > 0)

	return parts.StableMessage(), toolDefs
}
