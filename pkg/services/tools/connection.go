package tools

import (
	"fmt"

	"github.com/mark3labs/mcp-go/client"

	"github.com/liut/morign/pkg/models/mcps"
)

// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	Name      string
	URL       string
	TransType mcps.TransType
	client    *client.Client
	toolNames []string // 注册的工具名列表
}

// getToolKey returns the tool key with server prefix
func (mcpc *MCPConnection) getToolKey(name string) string {
	return fmt.Sprintf("%s__%s", mcpc.Name, name)
}
