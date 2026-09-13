package api

import (
	"fmt"
	"os"
	"testing"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/skills"
)

func dumpStore() *fakePromptStore {
	sto := newFakePromptStore()
	sto.preset = aigc.Preset{
		SystemPrompt:  "<preset 正文>",
		ToolsPrompt:   "<preset 工具说明>",
		ChannelPrompt: "<preset 频道说明>",
	}
	sto.skill = &fakeSkillStore{
		byName: map[string]*skills.Skill{
			"invoice": {SkillBasic: skills.SkillBasic{
				Name: "invoice", Description: "开发票", Content: "<技能正文>",
			}},
		},
		recent: []skills.Skill{{SkillBasic: skills.SkillBasic{Name: "invoice", Description: "开发票"}}},
	}
	return sto
}

func TestDumpPromptContent(t *testing.T) {
	if os.Getenv("MORRIGAN_DUMP_PROMPT") == "" {
		t.Skip("set MORRIGAN_DUMP_PROMPT=1 to dump the assembled prompt")
	}
	sto := dumpStore()
	ctx := userCtx("1001", "林涛")
	question := "帮我把上周的差旅报销算一下"
	history := aigc.HistoryItems{
		{ChatItem: &aigc.HistoryChatItem{User: "在吗", Assistant: "在的，请讲。"}},
		{ChatItem: &aigc.HistoryChatItem{User: "上周去北京出差了三天", Assistant: "收到。"}},
	}

	dumpCase := func(label string, cs *fakeConversation, toolNames map[string]bool, activate string) {
		parts := promptParts(ctx, sto, toolNames, nil, cs)
		activation := activatedSkill(ctx, sto.Skill(), activate)
		messages := chatMessages(parts.StableMessage(), activation, history, question, false)

		fmt.Printf("\n================ %s ================\n", label)
		for i, m := range messages {
			fmt.Printf("--- [%d] role=%s (%d 字符) ---\n%s\n", i, m.Role, len([]rune(m.Content)), m.Content)
		}
		fmt.Printf("--- 用量计数 MsgCount=%d（消息数组共 %d 条）---\n",
			conversationMsgCount(messages, activation), len(messages))
	}

	dumpCase("A. 记忆与技能工具在场 / 频道=wecom（无显式激活）",
		&fakeConversation{id: "6a1f0c3d2b4e5f608192a3b4", channel: "wecom"}, allTools(), "")
	dumpCase("B. 无工具（仅稳定 base）",
		&fakeConversation{id: "6a1f0c3d2b4e5f608192a3b4"}, nil, "")
	dumpCase("C. 显式激活 /skill invoice",
		&fakeConversation{id: "6a1f0c3d2b4e5f608192a3b4"}, allTools(), "invoice")
}
