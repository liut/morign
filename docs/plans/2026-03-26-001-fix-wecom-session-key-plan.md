---
title: "fix: 平台会话与 Conversation 映射及历史保持"
type: fix
status: completed
date: 2026-03-26
origin: docs/plans/2026-03-25-001-feat-platform-adapter-layer-plan.md
---

# fix: 平台会话与 Conversation 映射及历史保持

## Overview

将平台会话（sessionKey = `{platform}:{chatID}:{userID}`）与 Conversation (OID) 建立持久映射，解决：
1. WeCom WebSocket 当前使用 `reqID` 导致每条消息都是独立会话
2. 群聊/单聊应绑定为一个固定 Conversation，历史累积
3. 重连后可恢复之前的会话上下文

## Problem Statement

### 根因分析

**WeCom 当前问题** (`pkg/platform/wecom/websocket.go:329`):
```go
sessionKey := reqID  // reqID 是每次消息都不同的 UUID
```

**期望语义**: 一个聊天模式（单聊或群聊）= 一个固定 Conversation，历史累积

**问题**:
- `reqID` 每次消息都变化，导致每次创建新 Conversation
- `sessionKey` 格式 `wecom:{chatID}:{userID}` 与 `ReconstructReplyCtx` 期望不一致

### 平台会话语义

| 平台 | sessionKey 格式 | 语义 |
|------|-----------------|------|
| Feishu | `feishu:{chatID}:{userID}` | ✅ 正确 |
| WeCom (当前) | `wecom:{reqID}` | ❌ 每消息新会话 |
| WeCom (修复后) | `wecom:{chatID}:{userID}` | ✅ 同 Feishu |

## Proposed Solution

### 核心设计

**Redis 映射表**:
```
Key:   platform:csid:{platform}:{chatID}:{userID}
Value: conversation OID 字符串 (如 "cs-12345")
TTL:   30 天（可调整）
```

**查找顺序**:
1. 先查 Redis：`GET platform:csid:wecom:{chatID}:{userID}`
2. 找到 → 复用该 Conversation OID
3. 未找到 → 创建新 Conversation，写入 Redis 映射

### 实现位置

**`stores/conversation.go`**：新增 `GetOrCreateConversationBySessionKey` 函数

### 修改点

#### 1. stores/conversation.go - 新增映射函数

```go
const sessionKeyCSIDPrefix = "platform:csid:"

func sessionKeyToCSIDKey(sessionKey string) string {
    return sessionKeyCSIDPrefix + sessionKey
}

// GetOrCreateConversationBySessionKey 从 Redis 查找或创建 Conversation
func GetOrCreateConversationBySessionKey(ctx context.Context, sessionKey string) Conversation {
    // 1. 尝试从 Redis 获取已映射的 OID
    key := sessionKeyToCSIDKey(sessionKey)
    oidStr, err := SgtRC().Get(ctx, key).Result()
    if err == nil && oidStr != "" {
        // 2a. 找到映射，直接使用该 OID 创建 Conversation
        return NewConversation(ctx, oidStr)
    }

    // 2b. 未找到，创建新 Conversation
    cs := NewConversation(ctx, nil)  // 会生成新 OID
    csid := cs.GetID()

    // 3. 写入 Redis 映射，TTL 30 天
    SgtRC().Set(ctx, key, csid, 30*24*time.Hour)

    return cs
}
```

**关键点**：找到 OID 后直接 `NewConversation(ctx, oidStr)`，不管数据库是否有记录。`NewConversation` 内部会处理查找或创建。

#### 2. handle_platform.go - 使用新的映射函数

```go
// 修改前
csid := extractPlatformCSID(msg.SessionKey, msg.SessionKey)
cs := stores.NewConversation(ctx, csid)

// 修改后
cs := stores.GetOrCreateConversationBySessionKey(ctx, msg.SessionKey)
```

#### 3. wecom/websocket.go - 修复 sessionKey 格式

```go
// 修改前
sessionKey := reqID

// 修改后
sessionKey := fmt.Sprintf("wecom:%s:%s", chatID, body.From.UserID)
```

### 重连后上下文恢复

`ReconstructReplyCtx` 依赖 `sessionKey` 格式 `wecom:{chatID}:{userID}`，而该格式在重连后不变，所以：
1. 平台适配器仍用相同 sessionKey 构造 Message
2. `GetOrCreateConversationBySessionKey` 查找 Redis 映射，找到则复用
3. 找到对应 Conversation，加载历史消息

### 单聊新会话指令（可选扩展）

未来可支持 `/new` 或 `/clear` 指令来主动创建新 Conversation：
- 检测到指令时，删除 Redis 中的映射 Key
- 下次消息将创建新的 Conversation

## Acceptance Criteria

- [ ] WeCom WebSocket sessionKey 格式改为 `wecom:{chatID}:{userID}`
- [ ] 同一用户/群的消息共享会话历史（Redis 映射生效）
- [ ] 重连后能恢复之前的会话上下文
- [ ] Feishu 平台不受影响（使用相同逻辑）
- [ ] `make vet lint` 通过
- [ ] 不破坏现有 Reply/Send 功能

## Technical Considerations

### Redis 映射 TTL

- 设为 30 天，与会话历史 TTL（24 小时）分离
- 30 天无活动后，映射自动清除，下次消息创建新会话

### 去重机制

- 当前通过 `MsgID` 去重，不依赖 sessionKey
- 修复后同一用户发送的相同内容会被正确识别为重复

### 并发处理

- 同一 sessionKey 可能同时收到多条消息
- 需要考虑 Redis SetNX 或类似机制避免重复创建

### 平台兼容性

| 平台 | sessionKey | 映射支持 |
|------|------------|----------|
| Feishu WS | `feishu:{chatID}:{userID}` | ✅ 直接复用 |
| Feishu HTTP | `feishu:{chatID}:{userID}` | ✅ 直接复用 |
| WeCom WS | `wecom:{chatID}:{userID}` | ✅ 修复后复用 |
| WeCom HTTP | `wecom:{userID}` | ✅ 直接复用 |

## Files to Change

| File | Change |
|------|--------|
| `pkg/services/stores/conversation.go` | 新增 `GetOrCreateConversationBySessionKey` |
| `pkg/web/api/handle_platform.go` | 使用新的映射函数 |
| `pkg/services/channels/wecom/websocket.go:329` | `sessionKey := fmt.Sprintf("wecom:%s:%s", chatID, body.From.UserID)` |

## Related

- **Origin**: `docs/plans/2026-03-25-001-feat-platform-adapter-layer-plan.md`
- **Issue**: WebSocket 重连后会话历史断裂
- **Log Sample**:
  ```
  subReqID=ev-557gu7xyc9aj  # 正常
  subReqID=ev-557gwnxqblla  # 重连后变化
  subReqID=ev-557gz3ws309d  # 再次重连
  ```
