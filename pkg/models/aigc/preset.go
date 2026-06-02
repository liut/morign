package aigc

import "github.com/liut/morign/pkg/models/mcps"

// MCPServerConfig 定义 preset 中频道专属 MCP Server 的配置结构
type MCPServerConfig struct {
	Name       string          `json:"name" yaml:"name"`
	URL        string          `json:"url" yaml:"url"`
	TransType  mcps.TransType  `json:"trans_type" yaml:"trans_type"`
	HeaderCate mcps.HeaderCate `json:"header_cate,omitempty" yaml:"header_cate,omitempty"`
}

// Preset is the preset configuration including welcome message, system prompt and tool descriptions
type Preset struct {
	Welcome       string `json:"welcome,omitempty" yaml:"welcome,omitempty"`
	SystemPrompt  string `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`
	ToolsPrompt   string `json:"toolsPrompt,omitempty" yaml:"toolsPrompt,omitempty"`
	ChannelPrompt string `json:"channelPrompt,omitempty" yaml:"channelPrompt,omitempty"`

	KeywordTpl string `json:"keywordTpl,omitempty" yaml:"keywordTpl,omitempty"`
	TitleTpl   string `json:"titleTpl,omitempty" yaml:"titleTpl,omitempty"`

	// toolName -> description
	Tools map[string]string `json:"tools,omitempty" yaml:"tools,omitempty"`

	// Channels holds channel adapter configurations
	Channels map[string]ChannelConfig `json:"channels,omitempty" yaml:"channels,omitempty"`
}

// ChannelConfig holds configuration for a single channel adapter.
type ChannelConfig struct {
	Enable     bool               `json:"enable,omitempty" yaml:"enable,omitempty"`
	Mode       string             `json:"mode,omitempty" yaml:"mode,omitempty"` // "websocket", "webhook"
	Config     map[string]any     `json:"config,omitempty" yaml:"config,omitempty"`
	MCPServers []MCPServerConfig  `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
}
