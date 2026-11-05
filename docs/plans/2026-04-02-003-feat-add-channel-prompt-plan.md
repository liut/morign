---
title: 添加 channelPrompt 字段到 Preset
type: feat
status: completed
date: 2026-04-02
---

# 添加 channelPrompt 字段到 Preset

## Overview

在 Preset 配置中添加 `channelPrompt` 字段，用于外部平台（如企业微信wecom等）连接进来的聊天提示。当工具调用出现 401 令牌问题时，提示用户需要重新在 Web 端浏览器登录并打开助手页面。

## Proposed Solution

### 1. 修改 Preset 结构体

**文件:** `pkg/models/aigc/preset.go`

在 `Preset` 结构体中添加 `ChannelPrompt` 字段：

```go
type Preset struct {
    Welcome      string `json:"welcome,omitempty" yaml:"welcome,omitempty"`
    SystemPrompt string `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`
    ToolsPrompt  string `json:"toolsPrompt,omitempty" yaml:"toolsPrompt,omitempty"`
    ChannelPrompt string `json:"channelPrompt,omitempty" yaml:"channelPrompt,omitempty"` // 新增

    KeywordTpl string `json:"keywordTpl,omitempty" yaml:"keywordTpl,omitempty"`
    TitleTpl   string `json:"titleTpl,omitempty" yaml:"titleTpl,omitempty"`

    // toolName -> description
    Tools map[string]string `json:"tools,omitempty" yaml:"tools,omitempty"`

    // Channels holds channel adapter configurations
    Channels map[string]ChannelConfig `json:"channels,omitempty" yaml:"channels,omitempty"`
}
```

### 2. 修改 prepareSystemMessage 函数

**文件:** `pkg/web/api/handle_convo.go`

在 `prepareSystemMessage` 函数中，当 `ChannelPrompt` 存在时，将其追加到系统消息末尾（独占一行）：

```go
// 在函数末尾，返回之前添加
if len(sto.Preset().ChannelPrompt) > 0 {
    sb.WriteString("\n")
    sb.WriteString(sto.Preset().ChannelPrompt)
}
```

### 3. 更新示例配置

**文件:** `data/preset.example.yaml`

添加默认的 channelPrompt 配置：

```yaml
channelPrompt: "提示：当工具调用出现 401 令牌问题（如令牌过期、无效等）时，请尝试重新在 Web 端浏览器登录并打开助手页面一次。"
```

## Implementation Steps

- [ ] 在 `pkg/models/aigc/preset.go` 的 `Preset` 结构体中添加 `ChannelPrompt string` 字段
- [ ] 在 `pkg/web/api/handle_convo.go` 的 `prepareSystemMessage` 函数中追加 `ChannelPrompt`（独占一行）
- [ ] 在 `data/preset.example.yaml` 中添加示例 `channelPrompt` 配置

## Acceptance Criteria

- [ ] `Preset` 结构体包含 `ChannelPrompt` 字段
- [ ] 当配置了 `channelPrompt` 时，系统消息末尾会独占一行追加该内容
- [ ] 示例配置文件包含带默认提示语的 `channelPrompt`
- [ ] 运行 `make lint` 通过

## Files

- `pkg/models/aigc/preset.go` — 添加字段
- `pkg/web/api/handle_convo.go` — 追加到系统消息
- `data/preset.example.yaml` — 添加示例
