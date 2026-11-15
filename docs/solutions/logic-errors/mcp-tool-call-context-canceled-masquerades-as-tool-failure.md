---
title: "客户端取消被伪装成工具故障，导致模型反复重调工具"
category: logic-errors
date: 2026-11-13
tags: [mcp, tool-calls, regen, context-canceled, abort, bug]
status: fixed
issues:
  - "regen 之后工具被反复执行，模型不断重新规划"
  - "工具记录里出现 tool 'exec' call failed: transport error: failed to send request: ... context canceled"
  - "SSE 发完 tool_calls 后没有任何后续反馈，轮次一直停在「执行中」"
---

# 客户端取消被伪装成工具故障，导致模型反复重调工具

排查「regen 时会忽略最后一次历史并重新执行 tools」这一行为时发现的缺陷。regen 本身是正常设计，问题出在它触发的一个下游效应。

**修复**：见 `docs/plans/2026-11-13-001-fix-mcp-tool-call-cancel-and-timeout-plan.md`。取消时 `callTool` 返回 nil 且整批丢弃，`agent loop` 在取消态不再发起 LLM 请求，轮次落 `interrupted`；MCP 传输层新增 `MORIGN_MCP_TOOL_TIMEOUT_SECONDS`（默认 900s）兜底。

## 现象

前端点「重新生成」后：

- 旧的那一轮里，工具调用全部报 `context canceled`
- 新的一轮里，模型像是没看见工具失败，反复调用同一个工具
- 有时整条 SSE 流在 `tool_calls` 之后就没有后续了，轮次状态永远停在 `status=2`（执行中）

## 根因

### 一、取消被当成了工具失败

取消的传播链是自洽的：

1. 前端 regen 会先 `chatStore.abortByCsid(startedCsid)` 中止同一会话的所有旧流
2. 旧 HTTP 请求断开 → 后端 `r.Context()` 取消
3. 正在执行的 MCP 调用随之取消 → `Post ...: context canceled`
4. `agent/tool_executor.go` 的 `callTool` 捕获该 error，构造 `toolErrorResult(...)`
5. 这条「工具执行失败」的结果作为正常的 tool message 喂回模型

第 4 步是问题所在：**「客户端主动取消」与「工具真的坏了」被归成了同一类**。于是模型收到一条假的工具故障，自然会重新规划、再次调用工具。

修法见 `docs/plans/2026-11-13-001-fix-mcp-tool-call-cancel-and-timeout-plan.md` 的 U1。

### 二、MCP 传输层没有超时

`pkg/services/tools/registry.go` 用 `transport.NewStreamableHTTP` 建连，http client 没有 timeout；`client.CallTool` 的 ctx 直接来自 HTTP 请求。

而 Web 链路的 ctx 是 `r.Context()` —— 只有客户端断开才取消，**没有 deadline**。所以「ctx 取消即超时」的免费午餐在 Web 链路上不成立。

后果：某次 MCP 响应读取出岔子时，轮次永久停在 `status=2`，`finishTurn` 永不调用。

> 注：`docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md` 曾决策「不引入独立的工具超时配置，ctx 取消与 deadline 已覆盖」。该决策的前提是调用方 ctx 带 deadline，Web 链路不满足，已由上面这份计划修正。

## 排查手法（可复用）

### 用连接队列判断「谁没读」

`netstat -anv -p tcp | grep <pid>` 的 `RecvQ` / `SendQ` 是关键：

- `RecvQ=0, SendQ=<几百字节>`：请求已发出，服务端还没回 → 服务端侧问题
- `RecvQ=<几千字节>, SendQ=0`：**响应数据已在内核缓冲区，但应用层没读** → 客户端侧卡住

本次排查中正是后者排除了「MCP 服务端不可用」的误判 —— 同一时刻用 MCP 协议直接 curl 服务端，0.15 秒就返回了。

### 区分「服务端没回」与「客户端没读」

直接按 MCP 协议手工调服务端（`initialize` → `notifications/initialized` → `tools/call`），能立刻区分两种情况。注意服务端可能要求 `X-Owner-Id` / `X-Session-Id` 头，缺失时会返回 `owner_id and session_id are required`。

### 抓 goroutine 栈

`kill -QUIT <pid>` 会让 Go runtime 把所有 goroutine 栈打到 stderr。注意两点：

- 二进制若用 `-trimpath` 构建，栈里只有函数名没有文件路径，仍可读
- **必须抓「收到请求的那个进程」**。另起一个实例监听别的端口是收不到请求的，栈会缺关键帧

### 从数据库反推链路

`convo_chat_turn`（轮次状态）、`convo_usage_record`（LLM 调用次数）、`convo_tool_record`（工具轨迹）三张表能还原一次请求的完整时间线。本次靠它们确认了：第一轮 LLM 正常结束，卡点在工具执行阶段。

顺带一个坑：`convo_tool_record` 只在工具结果**超限**时才写（`spillToolResult`），短结果不落库。**查不到记录不等于工具没跑。**

## regen 语义备忘（这些是正常设计，不用查）

| 现象 | 依据 |
|------|------|
| 忽略最后一次历史 | `handle_convo.go` 设 `skipLast`；`prompt.go` 的 `historyMessages` 用 `break` 丢弃最后一轮 |
| 重新发起 LLM | `webTurnKey` 在 `retry` 时追加 nonce，绕过幂等新建一轮 |
| 重新执行工具 | 工具调用由模型每轮重新决定；轨迹按 `turn_id` 分列，旧轮次不被覆盖 |
| 历史不堆积 | `AddHistory` 发现「最后一条 User == 本轮 prompt」时 `RPop` 再 push，替换而非追加 |
