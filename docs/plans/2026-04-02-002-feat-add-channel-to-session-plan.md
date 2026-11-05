---
title: NewConversation 添加 channel 参数并从 sessionKey 提取
type: feat
status: completed
date: 2026-04-02
---

# NewConversation 添加 channel 参数并从 sessionKey 提取

## Overview

Session 表已添加 `Channel` 字段，现需在创建 Conversation 时从 `sessionKey` 提取 channel 标识并存储。

## Background

- `sessionKey` 格式：`{channel}:{chatID}:{userID}` 或 `{channel}:{userID}`
- `GetOrCreateConversationBySessionKey` 目前未提取 channel 和 chatID
- `convo.Session` 已有 `Channel` 字段，chatID 存入 Meta

## Proposed Solution

### 修改 `GetOrCreateConversationBySessionKey`

**文件:** `pkg/services/stores/conversation.go`

从 sessionKey 提取 channel 和 chatID 并设置到 Session：

```go
// GetOrCreateConversationBySessionKey 根据渠道 sessionKey 查找或创建 Conversation
// sessionKey 格式: "{channel}:{chatID}:{userID}" 或 "{channel}:{userID}"
// 查找 Redis 映射获取已绑定的 Conversation OID，若无则创建新 Conversation 并写入映射
func GetOrCreateConversationBySessionKey(ctx context.Context, sessionKey string) Conversation {
    key := sessionKeyCSIDPrefix + sessionKey
    oidStr, _ := SgtRC().Get(ctx, key).Result()

    cs := NewConversation(ctx, oidStr)
    if oidStr == "" {
        SgtRC().Set(ctx, key, cs.GetID(), 30*24*time.Hour)
    } else {
        SgtRC().Expire(ctx, key, 30*24*time.Hour)
    }

    // 从 sessionKey 提取 channel 和 chatID
    parts := strings.SplitN(sessionKey, ":", 3)
    if len(parts) >= 2 {
        cs.sess.SetWith(convo.SessionSet{Channel: &parts[0]})
        if len(parts) >= 3 {
            cs.sess.MetaSet("chatID", parts[1])
        }
    }
    return cs
}
```

## Implementation Steps

- [ ] 修改 `GetOrCreateConversationBySessionKey`，用 `strings.SplitN` 提取 channel 和 chatID
- [ ] 设置 `cs.sess.Channel` 和 `cs.sess.MetaSet("chatID", ...)`
- [ ] 运行 `make lint` 通过

## Files

- `pkg/services/stores/conversation.go`

## Acceptance Criteria

- [ ] 从 sessionKey 正确提取 channel 和 chatID
- [ ] Session.Channel 和 Meta("chatID") 被正确设置
- [ ] `make lint` 通过
