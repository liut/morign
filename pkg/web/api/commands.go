package api

import (
	"context"
	"strings"
	"unicode"

	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/services/stores"
)

type Command struct {
	Name    string
	Aliases []string
	Desc    string
	Action  func(ctx context.Context, msg *channel.Message) (bool, error)
}

var commandRegistry = []Command{
	{
		Name:    "reset",
		Aliases: []string{"/reset", "/new", "/clear"},
		Desc:    "重置会话，创建新的 csid",
		Action:  handleResetCommand,
	},
	{
		Name:    "skill",
		Aliases: []string{"/skill"},
		Desc:    "激活技能全文注入",
		Action:  handleSkillCommand,
	},
}

func DetectCommand(content string) Command {
	trimmed := strings.TrimSpace(content)
	for _, cmd := range commandRegistry {
		for _, alias := range cmd.Aliases {
			if isCommandMatch(trimmed, alias) {
				return cmd
			}
		}
	}
	return Command{}
}

// isCommandMatch 指令需为完整单词：别名后紧跟空白（空格/回车/tab 等）或消息结束。
func isCommandMatch(content, alias string) bool {
	if !strings.HasPrefix(content, alias) {
		return false
	}
	rest := content[len(alias):]
	return rest == "" || unicode.IsSpace(rune(rest[0]))
}

func handleResetCommand(ctx context.Context, msg *channel.Message) (bool, error) {
	if err := stores.ResetSessionBySessionKey(ctx, msg.SessionKey); err != nil {
		return false, err
	}
	logger().Info("command: session reset", "sessionKey", msg.SessionKey)
	return true, nil
}

// handleSkillCommand 解析 /skill <name> 后的技能名并剥离指令文本，继续正常对话流程。
func handleSkillCommand(ctx context.Context, msg *channel.Message) (bool, error) {
	content := strings.TrimSpace(msg.Content)
	rest, ok := strings.CutPrefix(content, "/skill")
	if !ok {
		return false, nil
	}
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return false, nil
	}
	rest = strings.TrimSpace(rest)
	name, tail, _ := strings.Cut(rest, " ")
	name = strings.TrimSpace(name)
	if !skills.ValidName(name) {
		return false, nil // 非法指令，按普通消息继续
	}
	msg.SkillName = name
	msg.Content = strings.TrimSpace(tail)
	return false, nil
}
