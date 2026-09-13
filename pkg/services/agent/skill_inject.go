package agent

import (
	"context"
	"strings"

	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/settings"
)

// SkillStore 注入核心所需的最小存储接口
type SkillStore interface {
	TopRecent(ctx context.Context, limit int) (skills.Skills, error)
	LoadForName(ctx context.Context, name string) (*skills.Skill, error)
}

// BuildSkillIndex 组装注入 system prompt 的技能索引：只有 name 与
// description 元数据，技能正文经 skill_read 工具或显式激活取得。
// 无可见技能时返回空字符串。
func BuildSkillIndex(ctx context.Context, sk SkillStore, requested []string) (string, error) {
	names, err := resolveNames(ctx, sk, requested)
	if err != nil || len(names) == 0 {
		return "", err
	}
	var objs []*skills.Skill
	for _, name := range names {
		obj, err := sk.LoadForName(ctx, name)
		if err != nil {
			continue // 不可见或不存在，跳过
		}
		objs = append(objs, obj)
	}
	if len(objs) == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("\n\n# Available Skills\n")
	for _, obj := range objs {
		sb.WriteString("- ")
		sb.WriteString(obj.Name)
		sb.WriteString(": ")
		sb.WriteString(obj.Description)
		sb.WriteString("\n")
	}
	sb.WriteString("可通过 skill_read 工具加载技能全文。\n")
	return sb.String(), nil
}

// SkillPromptByCommand 指令命中时加载单个技能全文（等效 skill_read）。
func SkillPromptByCommand(ctx context.Context, sk SkillStore, name string) (string, error) {
	obj, err := sk.LoadForName(ctx, name)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("\n## Skill: ")
	sb.WriteString(obj.Name)
	sb.WriteString("\n")
	sb.WriteString(obj.Content)
	return sb.String(), nil
}

func resolveNames(ctx context.Context, sk SkillStore, requested []string) ([]string, error) {
	if len(requested) > 0 {
		return dedupe(requested), nil
	}
	data, err := sk.TopRecent(ctx, settings.Current.SkillDefaultCount)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(data))
	for _, obj := range data {
		names = append(names, obj.Name)
	}
	return names, nil
}

func dedupe(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}
