package tools

import (
	"fmt"

	"github.com/mark3labs/mcp-go/client"

	"github.com/liut/morign/pkg/models/mcps"
)

// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	Channel   string
	Name      string
	URL       string
	TransType mcps.TransType
	client    *client.Client
	toolNames []string // 注册的工具名列表
}

func newMCPConnectionFromServer(sb *mcps.ServerBasic) *MCPConnection {
	return &MCPConnection{
		Channel:   sb.Channel,
		Name:      sb.Name,
		URL:       sb.URL,
		TransType: sb.TransType,
	}
}

func genToolKey(sn, tn string, ch string) string {
	if len(ch) > 0 {
		return fmt.Sprintf("%s__%s__%s", ch, sn, tn)
	}
	return fmt.Sprintf("%s__%s", sn, tn)
}
