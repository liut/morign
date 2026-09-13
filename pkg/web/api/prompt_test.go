package api

import (
	"context"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	auth "github.com/liut/simpauth"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/services/agent"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
)

type fakePromptStore struct {
	preset aigc.Preset
	skill  agent.SkillStore
}

func newFakePromptStore() *fakePromptStore {
	return &fakePromptStore{
		preset: aigc.Preset{
			SystemPrompt:  "preset base",
			ToolsPrompt:   "preset tools",
			ChannelPrompt: "channel rules",
		},
		skill: &fakeSkillStore{byName: map[string]*skills.Skill{}},
	}
}

func (f *fakePromptStore) Preset() aigc.Preset { return f.preset }

func (f *fakePromptStore) Skill() agent.SkillStore { return f.skill }

type fakeConversation struct {
	id      string
	channel string
	tools   []string
}

func (f *fakeConversation) GetID() string                    { return f.id }
func (f *fakeConversation) GetOID() oid.OID                  { return oid.Cast(f.id) }
func (f *fakeConversation) GetChannel() string               { return f.channel }
func (f *fakeConversation) SetTools(names ...string)         { f.tools = names }
func (f *fakeConversation) Save(context.Context) error       { return nil }
func (f *fakeConversation) CountHistory(context.Context) int { return 0 }
func (f *fakeConversation) AddHistory(context.Context, *aigc.HistoryItem) error {
	return nil
}
func (f *fakeConversation) ListHistory(context.Context) (aigc.HistoryItems, error) {
	return nil, nil
}
func (f *fakeConversation) ClearHistory(context.Context) error { return nil }

func skillEntry(name, desc, content string) *skills.Skill {
	return &skills.Skill{SkillBasic: skills.SkillBasic{Name: name, Description: desc, Content: content}}
}

func allTools() map[string]bool {
	return map[string]bool{
		tools.ToolNameMemoryList: true,
		tools.ToolNameSkillRead:  true,
	}
}

func userCtx(oidStr, name string) context.Context {
	return auth.ContextWithUser(context.Background(),
		&auth.User{OID: oidStr, UID: "uid-" + name, Name: name})
}

func TestPromptPartsStableAcrossTurns(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{
		byName: map[string]*skills.Skill{"invoice": skillEntry("invoice", "开发票", "INVOICE_BODY")},
		recent: []skills.Skill{{SkillBasic: skills.SkillBasic{Name: "invoice", Description: "开发票"}}},
	}
	cs := &fakeConversation{id: "s1", channel: "wecom"}

	ctx := userCtx("1001", "alice")
	first := promptParts(ctx, sto, allTools(), nil, cs).StableMessage().Content
	second := promptParts(ctx, sto, allTools(), nil, cs).StableMessage().Content

	if first != second {
		t.Errorf("stable message changed between turns:\n%q\n%q", first, second)
	}
}

func TestPromptPartsCarriesSessionConstantsLast(t *testing.T) {
	sto := newFakePromptStore()
	cs := &fakeConversation{id: "s1", channel: "wecom"}

	parts := promptParts(userCtx("1001", "alice"), sto, allTools(), nil, cs)

	want := "Current user: alice"
	if parts.SessionConstants != want {
		t.Errorf("session constants = %q, want %q", parts.SessionConstants, want)
	}
	content := parts.StableMessage().Content
	if !strings.HasSuffix(content, want) {
		t.Errorf("session constants must close the system message: %q", content)
	}
	// Only the display name is injected; identifiers are not the model's business.
	for _, unwanted := range []string{"uid-alice", "1001", "s1"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("prompt leaked %q: %q", unwanted, content)
		}
	}
}

func TestPromptPartsResolvesUserName(t *testing.T) {
	tests := []struct {
		name string
		user *auth.User
		want string
	}{
		{"display name wins", &auth.User{OID: "1001", UID: "liutao", Name: "林涛"}, "Current user: 林涛"},
		{"login name stands in when no display name", &auth.User{OID: "1002", UID: "wecom-zhangsan"}, "Current user: wecom-zhangsan"},
		{"display name equal to login name renders once", &auth.User{OID: "1003", UID: "zhangsan", Name: "zhangsan"}, "Current user: zhangsan"},
		{"no user, no constants", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = auth.ContextWithUser(ctx, tt.user)
			}
			parts := promptParts(ctx, newFakePromptStore(), allTools(), nil, &fakeConversation{id: "s1"})
			if parts.SessionConstants != tt.want {
				t.Errorf("session constants = %q, want %q", parts.SessionConstants, tt.want)
			}
		})
	}
}

func TestPromptPartsSessionConstantsAreTheOnlyPerUserDifference(t *testing.T) {
	sto := newFakePromptStore()

	alice := promptParts(userCtx("1001", "alice"), sto, allTools(), nil, &fakeConversation{id: "s1", channel: "wecom"})
	bob := promptParts(userCtx("1002", "bob"), sto, allTools(), nil, &fakeConversation{id: "s2", channel: "wecom"})

	alice.SessionConstants, bob.SessionConstants = "", ""
	if alice != bob {
		t.Errorf("session constants must be the only per-user difference:\n%+v\n%+v", alice, bob)
	}
}

func TestPromptPartsCarriesOnlyStableContent(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{
		byName: map[string]*skills.Skill{
			"invoice": skillEntry("invoice", "开发票", "INVOICE_BODY"),
		},
		recent: []skills.Skill{{SkillBasic: skills.SkillBasic{Name: "invoice", Description: "开发票"}}},
	}
	cs := &fakeConversation{id: "s1", channel: "wecom"}

	parts := promptParts(context.Background(), sto, allTools(), nil, cs)
	stable := parts.StableMessage()

	if stable.Role != llm.RoleSystem {
		t.Errorf("stable role = %q", stable.Role)
	}
	for _, want := range []string{"preset base", "preset tools", "channel rules", agent.MemoryGuidance, "# Available Skills", "- invoice: "} {
		if !strings.Contains(stable.Content, want) {
			t.Errorf("stable message missing %q: %q", want, stable.Content)
		}
	}
	// R2: per-turn context never enters the prompt. Identity is covered by
	// TestPromptPartsCarriesSessionConstantsLast.
	for _, unwanted := range []string{
		"INVOICE_BODY", "当前时辰", "Current user", "memory related to user ID",
	} {
		if strings.Contains(stable.Content, unwanted) {
			t.Errorf("stable message leaked %q: %q", unwanted, stable.Content)
		}
	}
	if parts.ActivatedMessage() != nil {
		t.Error("no activation expected without an explicit /skill command")
	}
}

func TestPromptPartsWithoutTools(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{
		byName: map[string]*skills.Skill{"invoice": skillEntry("invoice", "开发票", "INVOICE_BODY")},
		recent: []skills.Skill{{SkillBasic: skills.SkillBasic{Name: "invoice", Description: "开发票"}}},
	}

	parts := promptParts(context.Background(), sto, nil, nil, &fakeConversation{id: "s1"})
	content := parts.StableMessage().Content

	if content != "preset base" {
		t.Errorf("tool-less turn should carry the base prompt only, got %q", content)
	}
	if strings.Contains(content, agent.MemoryGuidance) || strings.Contains(content, "# Available Skills") {
		t.Errorf("memory guidance and skills index need their tools: %q", content)
	}
}

func TestPromptPartsGatesGuidancePerTool(t *testing.T) {
	sto := newFakePromptStore()
	cs := &fakeConversation{id: "s1"}

	onlySkill := promptParts(context.Background(), sto, map[string]bool{tools.ToolNameSkillRead: true}, nil, cs)
	if strings.Contains(onlySkill.StableMessage().Content, agent.MemoryGuidance) {
		t.Error("memory guidance injected without memory tools")
	}

	onlyMemory := promptParts(context.Background(), sto, map[string]bool{tools.ToolNameMemoryList: true}, nil, cs)
	if !strings.Contains(onlyMemory.StableMessage().Content, agent.MemoryGuidance) {
		t.Error("memory guidance missing with memory tools present")
	}
}

func TestPrepareSystemMessageSetsConversationTools(t *testing.T) {
	sto := newFakePromptStore()
	cs := &fakeConversation{id: "s1"}

	sysMsg, toolDefs := prepareSystemMessage(context.Background(), sto, tools.NewRegistry(nil), nil, cs)

	if len(toolDefs) == 0 {
		t.Fatal("expected built-in tool definitions")
	}
	if len(cs.tools) != len(toolDefs) {
		t.Errorf("conversation tools = %v, want %d entries", cs.tools, len(toolDefs))
	}
	if sysMsg.Role != llm.RoleSystem {
		t.Errorf("system role = %q", sysMsg.Role)
	}
}

func TestActivatedSkill(t *testing.T) {
	sk := &fakeSkillStore{byName: map[string]*skills.Skill{
		"invoice": skillEntry("invoice", "开发票", "INVOICE_BODY"),
	}}

	msg := activatedSkill(context.Background(), sk, "invoice")
	if msg == nil {
		t.Fatal("expected an activation message")
	}
	if msg.Role != llm.RoleUser || !strings.Contains(msg.Content, "INVOICE_BODY") {
		t.Errorf("activation message = %+v", msg)
	}
	if !strings.HasPrefix(msg.Content, agent.ActivationHeader) {
		t.Errorf("activation should be labelled: %q", msg.Content)
	}

	if got := activatedSkill(context.Background(), sk, ""); got != nil {
		t.Errorf("empty name should not activate, got %q", got.Content)
	}
	if got := activatedSkill(context.Background(), sk, "nope"); got != nil {
		t.Errorf("invisible skill should not activate, got %q", got.Content)
	}
}

func TestChatMessagesSequence(t *testing.T) {
	sysMsg := llm.Message{Role: llm.RoleSystem, Content: "S"}
	activation := &llm.Message{Role: llm.RoleUser, Content: "A"}
	history := aigc.HistoryItems{
		{ChatItem: &aigc.HistoryChatItem{User: "h1", Assistant: "a1"}},
		{ChatItem: &aigc.HistoryChatItem{User: "h2", Assistant: "a2"}},
	}

	got := chatMessages(sysMsg, activation, history, "q", false)
	want := []llm.Message{
		{Role: llm.RoleSystem, Content: "S"},
		{Role: llm.RoleUser, Content: "h1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "h2"},
		{Role: llm.RoleAssistant, Content: "a2"},
		{Role: llm.RoleUser, Content: "A"},
		{Role: llm.RoleUser, Content: "q"},
	}
	assertMessages(t, got, want)

	if n := conversationMsgCount(want, activation); n != len(want)-1 {
		t.Errorf("conversation msg count = %d, want %d", n, len(want)-1)
	}
	if n := conversationMsgCount(want, nil); n != len(want) {
		t.Errorf("conversation msg count = %d, want %d", n, len(want))
	}
}

func TestChatMessagesWithoutActivation(t *testing.T) {
	sysMsg := llm.Message{Role: llm.RoleSystem, Content: "S"}
	history := aigc.HistoryItems{{ChatItem: &aigc.HistoryChatItem{User: "h1", Assistant: "a1"}}}

	assertMessages(t, chatMessages(sysMsg, nil, history, "q", false), []llm.Message{
		{Role: llm.RoleSystem, Content: "S"},
		{Role: llm.RoleUser, Content: "h1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "q"},
	})
}

func TestChatMessagesSkipLastOnRetry(t *testing.T) {
	sysMsg := llm.Message{Role: llm.RoleSystem, Content: "S"}
	history := aigc.HistoryItems{
		{ChatItem: &aigc.HistoryChatItem{User: "h1", Assistant: "a1"}},
		{ChatItem: &aigc.HistoryChatItem{User: "h2", Assistant: "a2"}},
	}

	assertMessages(t, chatMessages(sysMsg, nil, history, "h2", true), []llm.Message{
		{Role: llm.RoleSystem, Content: "S"},
		{Role: llm.RoleUser, Content: "h1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "h2"},
	})
}

func assertMessages(t *testing.T, got, want []llm.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("messages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Errorf("message %d = %s/%q, want %s/%q",
				i, got[i].Role, got[i].Content, want[i].Role, want[i].Content)
		}
	}
}
