---
title: "企微 HTTP 回调路径未接入消息去重"
category: logic-errors
date: 2026-10-29
tags: [wecom, channel, dedup, idempotency, bug]
status: open
related:
  - docs/plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md
  - docs/reviews/2026-11-02-whip-plan-review-revision-log.md
issues:
  - "同一企业微信消息被重复投递时，模型调用与副作用工具会重复执行"
  - "重复投递会产生两条相同的频道回复"
---

# 企微 HTTP 回调路径未接入消息去重

## Problem

企业微信的 HTTP 回调路径（`pkg/services/channels/wecom/wecom_http.go`）收到消息后直接异步投递到 handler，**全程没有任何幂等检查**。

`Dedup.IsDuplicate` 在全仓只有三个调用点：

| 文件 | 位置 | 通道 |
| --- | --- | --- |
| `pkg/services/channels/wecom/websocket.go` | :334 | 企微 WebSocket |
| `pkg/services/channels/feishu/websocket.go` | :242 | 飞书 WebSocket |
| `pkg/services/channels/feishu/webhook.go` | :281 | 飞书 HTTP 回调 |
| `pkg/services/channels/wecom/wecom_http.go` | — | **企微 HTTP 回调：缺失** |

对比可见，企微 HTTP 是四条频道路径中唯一没有去重的一条，且它是 HTTP 回调——恰好是最容易发生重投的接入方式。

## Root Cause

`handleMessage` 的处理顺序是：解密 → `w.WriteHeader(http.StatusOK)` → `go handler(...)`。

企微要求回调在 5 秒内响应，因此代码提前 ack、把实际处理放到 goroutine。这个设计本身是对的，但 ack 之后到 handler 执行之间没有任何"这条消息处理过没有"的判定。

三条已有去重的路径都在解密/解析后、`handler` 调用前插入了一次 `IsDuplicate` 检查，企微 HTTP 路径在重构时（`docs/plans/2026-03-25-001-feat-channel-adapter-layer-plan.md` 引入通道适配层）漏掉了这一步。

## Impact

企微官方策略：回调未在 5 秒内响应会重试最多 3 次，间隔递增。每次重投都会完整重跑一遍模型调用与工具执行。

工具的重复调用代价不均匀：

| 工具 | 重复调用后果 |
| --- | --- |
| `memory_store` | 幂等——同 key 走 update 分支（`pkg/services/stores/convo_x.go:517`） |
| `memory_list` / `memory_recall` | 只读 |
| `KBCreate` | **非幂等**——每次新建文档，重复投递产生重复知识库条目 |
| 外部 MCP 工具 | **非幂等且副作用未知**——发消息、下单、改配置都在此列 |
| 频道回复 | 用户收到两条相同回复 |

## Detection

日志中可观察到同一条 `msg_id` 触发了多次 `channel: message received`：

```
slog.Info("channel: message received", "channel", ..., "session", ..., "conversation", ...)
```

企微 HTTP 路径的 `wecom-http: message received` 日志（`wecom_http.go:214`）在重复投递时会出现多次，且其后没有对应的 `duplicate message skipped` 日志——其它三条路径都有。

## Fix

见 [计划 003](../../plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md)：企微 HTTP 的重复投递由 U3 在 `handle_platform.go` 的 `MessageHandler` 处接入 PG 侧 `ChatTurn` 幂等键（`wecom:<msg_id>`）统一覆盖——`MessageHandler` 是频道侧的汇合点，企微 HTTP 的 `go handler(...)` 最终也会走到那里。

**不再单独为企微 HTTP 补 Redis 前置闸门。** Redis `SetNX` 命中即丢弃的语义正是本文件"反方向的问题"一节记录的静默丢失根因：进程在 `SetNX` 之后、轮次落库之前崩溃时，平台重投会被直接跳过——既没有执行，也没有轮次记录。计划 003 因此把 Redis 去重降级为"只记录不阻断"的提示层，判定权归 PG 状态机；补 Redis 闸门等于把同一缺陷复制到唯一还没有它的路径上。

代价是 PG 幂等上线前，企微 HTTP 仍然裸奔。这是本计划的取舍窗口，不是修复的终点。

## Prevention

1. **新增通道接入时逐条核对能力清单**——去重、身份注入、频道工具、超时，缺一项就是一条通道与其它通道行为不一致。企微 HTTP 与其它三条通道的差异正是在这个环节引入的。
2. **`Dedup` 的使用应在通道适配层统一收口**，而不是由每个 adapter 自行决定要不要调用。让"忘了调用"变成编译期或构造期可见的问题，而不是运行时行为差异。
3. **同一能力的多条实现路径要有对照测试**——四条频道路径应共享同一组"收到消息"的契约测试。
