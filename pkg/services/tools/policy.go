package tools

import (
	"context"
	"strings"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/stores"
)

// 工具的授权归属类别，既是判定输入也是日志字段。
const (
	SourceBuiltinPublic = "builtin-public" // 内置公开工具，始终允许
	SourceBuiltinKeeper = "builtin-keeper" // 内置受限工具，仅 keeper 允许
	SourceMCPGlobal     = "mcp-global"     // 全局 MCP 工具，始终允许
	SourceMCPChannel    = "mcp-channel"    // 频道专属 MCP 工具，仅归属频道允许
)

// EffectiveToolPolicy 是当前上下文下的工具授权判定结果。
// 可见面（ToolsFor）与执行面（Invoke）共用同一份判定：看不见的工具也无法按名调用。
type EffectiveToolPolicy struct {
	keeper  bool
	channel string

	// registered 是全局工具名（小写）的集合。
	registered map[string]struct{}
	// categories 是全局工具名（小写）到授权归属类别的映射。
	categories map[string]string
	// channelOwners 是频道工具名（小写）到归属频道的映射。
	channelOwners map[string]string
}

// PolicyFor 计算当前上下文下的工具授权策略。
// 归属信息在注册期记录，判定只读该表，开销与工具数量无关。
func (r *Registry) PolicyFor(ctx context.Context) EffectiveToolPolicy {
	r.toolsMu.RLock()
	registered := make(map[string]struct{}, len(r.invokers)+len(r.tools))
	for name := range r.invokers {
		registered[strings.ToLower(name)] = struct{}{}
	}
	for _, desc := range r.tools {
		registered[strings.ToLower(desc.Name)] = struct{}{}
	}
	categories := make(map[string]string, len(r.categories))
	for name, src := range r.categories {
		categories[name] = src
	}
	r.toolsMu.RUnlock()

	r.channelMu.RLock()
	type channelSet struct {
		name string
		cs   *channelToolSet
	}
	sets := make([]channelSet, 0, len(r.channelTools))
	for name, cs := range r.channelTools {
		sets = append(sets, channelSet{name: name, cs: cs})
	}
	r.channelMu.RUnlock()

	channelOwners := make(map[string]string)
	for _, s := range sets {
		s.cs.mu.RLock()
		for name := range s.cs.invokers {
			channelOwners[strings.ToLower(name)] = s.name
		}
		for _, desc := range s.cs.tools {
			channelOwners[strings.ToLower(desc.Name)] = s.name
		}
		s.cs.mu.RUnlock()
	}

	return EffectiveToolPolicy{
		keeper:        stores.IsKeeper(ctx),
		channel:       mcps.ChannelFromContext(ctx),
		registered:    registered,
		categories:    categories,
		channelOwners: channelOwners,
	}
}

// Source 返回工具的授权来源，未注册时返回空串。
func (p EffectiveToolPolicy) Source(name string) string {
	key := strings.ToLower(name)
	if src, ok := p.categories[key]; ok {
		return src
	}
	if _, ok := p.channelOwners[key]; ok {
		return SourceMCPChannel
	}
	return ""
}

// Registered 判断工具是否已注册，用于区分"未找到"与"无权限"。
func (p EffectiveToolPolicy) Registered(name string) bool {
	key := strings.ToLower(name)
	if _, ok := p.registered[key]; ok {
		return true
	}
	_, ok := p.channelOwners[key]
	return ok
}

// Allows 判断工具在当前策略下是否可执行。
func (p EffectiveToolPolicy) Allows(name string) bool {
	key := strings.ToLower(name)
	switch p.Source(name) {
	case SourceBuiltinPublic, SourceMCPGlobal:
		return true
	case SourceBuiltinKeeper:
		return p.keeper
	case SourceMCPChannel:
		return p.channelOwners[key] == p.channel
	default:
		return false
	}
}
