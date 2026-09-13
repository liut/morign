package api

import (
	"context"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/services/agent"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
)

// promptStore is the storage surface prompt assembly needs. Narrowing it keeps
// the slot mapping testable: the generated sub-store interfaces are far too
// large to fake.
type promptStore interface {
	Preset() aigc.Preset
	Skill() agent.SkillStore
}

// storagePromptStore forwards to the real store.
type storagePromptStore struct{ sto stores.Storage }

func (s storagePromptStore) Preset() aigc.Preset { return s.sto.Preset() }

func (s storagePromptStore) Skill() agent.SkillStore { return s.sto.Skill() }

// promptParts fills the stable blocks and the trailing session constants.
// Per-turn data — time, memories, retrieved documents — deliberately has no
// slot: it reaches the model through tools instead of the prompt.
func promptParts(ctx context.Context, sto promptStore, toolNames map[string]bool,
	skills []string, cs stores.Conversation) agent.SystemPromptParts {
	preset := sto.Preset()
	parts := agent.SystemPromptParts{
		Base:  preset.SystemPrompt,
		Tools: preset.ToolsPrompt,
	}.Normalize(len(toolNames) > 0)

	if len(cs.GetChannel()) > 0 {
		parts.Channel = preset.ChannelPrompt
	}

	if toolNames[tools.ToolNameMemoryList] {
		parts.Memory = agent.MemoryGuidance
	}
	if toolNames[tools.ToolNameSkillRead] {
		parts.Skills = skillIndex(ctx, sto, skills)
	}
	parts.SessionConstants = sessionConstants(ctx)

	return parts
}

// sessionConstants renders the values fixed for the life of a conversation. It
// carries the user's display name only: the session id, the uid and the OID all
// stay out — the model has no use for identifiers. Rendering last keeps a new
// conversation or another user from invalidating the shared prefix ahead of it.
func sessionConstants(ctx context.Context) string {
	if user, ok := stores.UserFromContext(ctx); ok && user.Name != "" {
		return "Current user: " + user.Name
	}
	return ""
}

// prepareSystemMessage resolves tool definitions and renders the stable system
// message.
func prepareSystemMessage(ctx context.Context, sto promptStore, toolreg *tools.Registry,
	skills []string, cs stores.Conversation) (llm.Message, []llm.ToolDefinition) {
	toolDefs := agent.ConvertMCPToolsToLLMTools(toolreg.ToolsFor(ctx))
	names := llm.Tools(toolDefs).Names()
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}

	parts := promptParts(ctx, sto, set, skills, cs)
	if len(toolDefs) > 0 {
		cs.SetTools(names...)
	}
	return parts.StableMessage(), toolDefs
}

// skillIndex renders the skills index: names and descriptions only. Full text
// is loaded by the skill_read tool or injected by an explicit activation.
func skillIndex(ctx context.Context, sto promptStore, requested []string) string {
	block, err := agent.BuildSkillIndex(ctx, sto.Skill(), requested)
	if err != nil {
		logger().Warn("skill prompt fail", "err", err)
		return ""
	}
	return block
}

// activatedSkill renders the full text of an explicitly activated skill as a
// user message, or nil when the lookup fails or yields nothing.
func activatedSkill(ctx context.Context, sk agent.SkillStore, name string) *llm.Message {
	if name == "" {
		return nil
	}
	block, err := agent.SkillPromptByCommand(ctx, sk, name)
	if err != nil {
		logger().Warn("activate skill fail", "name", name, "err", err)
		return nil
	}
	return agent.SystemPromptParts{Activation: block}.ActivatedMessage()
}

// chatMessages assembles the wire sequence: the stable system message, the
// conversation history, the optional activation message and the current
// question. Keeping the activation after the history lets the system message
// and the history form a prefix that consecutive turns can reuse.
func chatMessages(sysMsg llm.Message, activation *llm.Message, history aigc.HistoryItems,
	prompt string, skipLast bool) []llm.Message {
	messages := make([]llm.Message, 0, len(history)*2+3)
	messages = append(messages, sysMsg)
	messages = append(messages, historyMessages(history, skipLast)...)
	if activation != nil {
		messages = append(messages, *activation)
	}
	return append(messages, llm.Message{Role: llm.RoleUser, Content: prompt})
}

// historyMessages renders stored exchanges as messages, optionally skipping the
// most recent one on retry or regenerate.
func historyMessages(data aigc.HistoryItems, skipLast bool) []llm.Message {
	messages := make([]llm.Message, 0, len(data)*2)
	for i, hi := range data {
		if hi.ChatItem == nil {
			continue
		}
		if skipLast && i == len(data)-1 {
			break
		}
		if len(hi.ChatItem.User) > 0 {
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: hi.ChatItem.User})
		}
		if len(hi.ChatItem.Assistant) > 0 {
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: hi.ChatItem.Assistant})
		}
	}
	return messages
}

// conversationMsgCount counts the conversation messages in a wire sequence. The
// activation message is derived prompt plumbing, not conversation content, so
// it stays out of the count.
func conversationMsgCount(messages []llm.Message, activation *llm.Message) int {
	if activation == nil {
		return len(messages)
	}
	return len(messages) - 1
}
