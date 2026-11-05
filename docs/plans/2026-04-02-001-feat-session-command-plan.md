---
title: feat: 添加会话指令系统 (/new, /clear)
type: feat
status: completed
date: 2026-04-02
---

# 添加会话指令系统 (/new, /clear)

## 概述

在消息处理流程中检测特殊指令（如 `/new`），执行对应操作（如重置会话）后再进入正常 LLM 处理流程。指令在内容中居首位的才有效。

## 动机

- 用户需要一个快捷方式来重置会话、开始新的对话上下文
- 当前没有机制来区分普通用户输入和系统指令
- 未来可能扩展更多指令（/help, /reset 等）

## 解决方案

### 1. 设计指令检测与执行机制

在 `pkg/web/api/handle_platform.go` 的 `MessageHandler` 中，在构建 `cs` (Conversation) **之前** 检测消息内容是否以指令开头。

#### 指令注册表设计

```go
// pkg/web/api/commands.go

type Command struct {
    Name    string   // 指令名，如 "new", "clear"
    Aliases []string // 别名列表，如 []string{"/new", "/clear"}
    Desc    string   // 指令描述
    Action  func(ctx context.Context, msg *channel.Message) (bool, error)
    // 返回 (handled, error): handled=true 表示指令已处理，不再继续 LLM 调用
}

var commandRegistry = []Command{
    {
        Name:    "reset",
        Aliases: []string{"/reset", "/new", "/clear"},
        Desc:    "重置会话，创建新的 csid",
        Action:  handleResetCommand,
    },
}

func handleResetCommand(ctx context.Context, msg *channel.Message) (bool, error) {
    // 调用 stores 包的导出函数删除 sessionKey -> csid 映射
    if err := stores.ResetSessionBySessionKey(ctx, msg.SessionKey); err != nil {
        return false, err
    }
    logger().Infow("command: session reset", "sessionKey", msg.SessionKey)
    return true, nil // 指令已处理，不继续 LLM 调用
}
```

#### 修改 MessageHandler 流程

```
MessageHandler
  └─> 检测指令 ──> 执行指令 Action
            └─> handled=true? ─┬─> true: 发送确认回复，结束
                               └─> false: 继续正常 LLM 流程
```

在 `handle_platform.go:MessageHandler` 的第 112 行 `cs := stores.GetOrCreateConversationBySessionKey(...)` 之前插入指令检测：

```go
// Check for commands at the beginning of content
if cmd := DetectCommand(msg.Content); cmd != nil {
    handled, err := cmd.Action(ctx, msg)
    if err != nil {
        logger().Warnw("command execution failed", "cmd", cmd.Name, "err", err)
    }
    if handled {
        // Send acknowledgment to user
        if err := p.Reply(ctx, msg.ReplyCtx, "会话已重置，开始新对话"); err != nil {
            logger().Warnw("reply after command failed", "err", err)
        }
        return
    }
    // Fall through to normal processing with trimmed content
}
```

### 2. 实现 DetectCommand 函数

```go
// pkg/web/api/commands.go

func DetectCommand(content string) *Command {
    trimmed := strings.TrimSpace(content)
    for _, cmd := range commandRegistry {
        for _, alias := range cmd.Aliases {
            if strings.HasPrefix(trimmed, alias) {
                return &cmd
            }
        }
    }
    return nil
}
```

### 3. 扩展指令注册表（未来）

在 `commands.go` 的 `commandRegistry` 中添加新条目即可：

```go
{
    Name:    "help",
    Aliases: []string{"/help", "/h", "?"},
    Desc:    "显示可用指令列表",
    Action:  handleHelpCommand,
},
```

## 系统影响

### 交互图

```
MessageHandler
  └─> DetectCommand() ──> Command.Action()
        ├─> new/clear: Del Redis key
        └─> (future) help: Reply with command list
```

### 错误传播

- 指令执行失败：记录日志，继续正常 LLM 处理流程（不阻塞用户）
- Redis 删除失败：返回 false 让流程继续（容错设计）

### 状态生命周期

- `/new` 删除 Redis 中的 `platform:csid:{sessionKey}` 映射
- 后续 `GetOrCreateConversationBySessionKey` 会创建新的 csid
- 旧的 Redis history key (`convs-{old_csid}`) 保留直到 TTL 过期

## 验收标准

- [ ] 发送 `/new` 或 `/clear` 时，系统生成新的 csid，后续对话与之前隔离
- [ ] 发送普通消息时，不触发指令检测，正常进入 LLM 处理
- [ ] 指令处理后向用户发送确认消息
- [ ] 指令可扩展，添加新指令只需在 registry 中添加条目

## 技术细节

### 关键文件

- `pkg/services/stores/conversation.go` — 添加 `ResetSessionBySessionKey()` 导出函数
- `pkg/web/api/handle_platform.go:112` — 插入指令检测逻辑
- `pkg/web/api/commands.go` (新建) — 指令注册与执行逻辑

### 别名实现

别名在 `Command.Aliases` 中定义，检测时遍历所有别名匹配。指令匹配以空格或消息结束为边界，防止误匹配（如 `/newuser` 不会匹配 `/new`）。

## 依赖与风险

- 无新依赖
- 风险：指令检测应在 LLM 处理之前，但不影响现有消息流

## 实现步骤

1. 在 `stores/conversation.go` 添加 `ResetSessionBySessionKey(ctx, sessionKey)` 导出函数
2. 创建 `pkg/web/api/commands.go`，定义 `Command` 结构体和 `commandRegistry`
3. 实现 `DetectCommand()` 函数
4. 在 `MessageHandler` 第 112 行前插入指令检测
5. 实现 `handleNewCommand`，调用 `stores.ResetSessionBySessionKey` 后发送确认
6. 添加单元测试（检测指令解析、别名匹配）
