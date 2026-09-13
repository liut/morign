package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
)

func TestStableMessageOrdersBlocks(t *testing.T) {
	tests := []struct {
		name     string
		parts    SystemPromptParts
		hasTools bool
		want     string
	}{
		{
			name: "all blocks in declared order",
			parts: SystemPromptParts{
				Base: "base", Tools: "tools", Channel: "channel",
				Memory: "memory guidance", Skills: "# Available Skills",
			},
			hasTools: true,
			want:     "base\ntools\nchannel\nmemory guidance\n# Available Skills",
		},
		{
			name: "empty base falls back to default",
			parts: SystemPromptParts{
				Memory: "memory guidance",
			},
			want: DefaultSystemMsg + "\nmemory guidance",
		},
		{
			name:     "empty tools prompt falls back to default",
			parts:    SystemPromptParts{Base: "base"},
			hasTools: true,
			want:     "base\n" + DefaultToolsMsg,
		},
		{
			name:     "no tools drops the tools block",
			parts:    SystemPromptParts{Base: "base", Tools: "tools", Channel: "channel"},
			hasTools: false,
			want:     "base\nchannel",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.parts.Normalize(tt.hasTools).StableMessage()
			if got.Role != llm.RoleSystem {
				t.Errorf("role = %q, want %q", got.Role, llm.RoleSystem)
			}
			if got.Content != tt.want {
				t.Errorf("content = %q, want %q", got.Content, tt.want)
			}
		})
	}
}

func TestStableMessageKeepsActivationOut(t *testing.T) {
	parts := SystemPromptParts{Base: "base", Activation: "## Skill: invoice\ninvoice body"}

	stable := parts.Normalize(true).StableMessage()
	if strings.Contains(stable.Content, "invoice body") {
		t.Errorf("stable message leaked the activation block: %q", stable.Content)
	}
	if stable.Content != parts.Normalize(true).StableMessage().Content {
		t.Error("stable message is not deterministic")
	}
}

func TestActivatedMessage(t *testing.T) {
	parts := SystemPromptParts{Base: "base", Activation: "\n## Skill: invoice\ninvoice body\n"}

	msg := parts.ActivatedMessage()
	if msg == nil {
		t.Fatal("expected an activation message")
	}
	if msg.Role != llm.RoleUser {
		t.Errorf("role = %q, want %q", msg.Role, llm.RoleUser)
	}
	want := ActivationHeader + "\n## Skill: invoice\ninvoice body"
	if msg.Content != want {
		t.Errorf("content = %q, want %q", msg.Content, want)
	}

	for _, empty := range []string{"", "   ", "\n"} {
		if got := (SystemPromptParts{Activation: empty}).ActivatedMessage(); got != nil {
			t.Errorf("activation %q should produce no message, got %q", empty, got.Content)
		}
	}
}

func TestBuildSystemMessage(t *testing.T) {
	reg := tools.NewRegistry(nil) // built-in tools without a store
	sysMsg, toolDefs := BuildSystemMessage(context.Background(), reg, SystemPromptParts{Base: "base"})

	if len(toolDefs) == 0 {
		t.Fatal("expected built-in tool definitions")
	}
	if sysMsg.Content != "base\n"+DefaultToolsMsg {
		t.Errorf("system content = %q", sysMsg.Content)
	}
	// R2: time and other standing context never enter the prompt.
	if strings.Contains(sysMsg.Content, "当前时辰") || strings.Contains(sysMsg.Content, "SessionID") {
		t.Errorf("system message leaked standing context: %q", sysMsg.Content)
	}
}
