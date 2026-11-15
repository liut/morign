package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/settings"
)

func setMCPToolTimeout(t *testing.T, secs int) {
	t.Helper()
	orig := settings.Current.MCPToolTimeoutSeconds
	settings.Current.MCPToolTimeoutSeconds = secs
	t.Cleanup(func() { settings.Current.MCPToolTimeoutSeconds = orig })
}

func streamableServerBasic(name, url string) *mcps.ServerBasic {
	return &mcps.ServerBasic{Name: name, URL: url, TransType: mcps.TransTypeStreamable}
}

func TestAddServerStreamableHappyPath(t *testing.T) {
	// Happy path: 正常 MCP 服务，加超时选项后工具调用结果不变。
	setMCPToolTimeout(t, 5)
	mcpSrv := mcpserver.NewMCPServer("test", "1.0.0")
	mcpSrv.AddTools(mcpserver.ServerTool{
		Tool: mcp.NewTool("echo"),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	})
	httpSrv := httptest.NewServer(mcpserver.NewStreamableHTTPServer(mcpSrv))
	defer httpSrv.Close()

	reg := NewRegistry(nil)
	if err := reg.AddServer(context.Background(), streamableServerBasic("test", httpSrv.URL)); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	res, err := reg.Invoke(context.Background(), "test__echo", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if mcps.IsToolError(res) {
		t.Fatalf("unexpected tool error: %v", res)
	}
	if got := toolResultTextOf(res); got != "pong" {
		t.Errorf("result text = %q, want pong", got)
	}
}

// hangTCPServer 起一个接受连接但永不响应的假 MCP 服务器（模拟传输挂起），
// 返回 server URL 与 cleanup；cleanup 关闭 listener 与所有已接受的连接。
func hangTCPServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	return "http://" + ln.Addr().String(), func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	}
}

func TestAddServerStreamableInitializeTimeout(t *testing.T) {
	// Edge case: 服务器接受连接但永不响应 —— 超时后 AddServer 报错返回。
	setMCPToolTimeout(t, 1)
	url, cleanup := hangTCPServer(t)
	defer cleanup()

	reg := NewRegistry(nil)
	start := time.Now()
	err := reg.AddServer(context.Background(), streamableServerBasic("hang", url))
	if err == nil {
		t.Fatal("expected timeout error from AddServer")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("AddServer took %v, want fast timeout failure", elapsed)
	}
}

func TestCallToolTimeoutYieldsToolError(t *testing.T) {
	// Edge case + R8: call tool 挂起 —— 传输超时后按工具失败处理（error=nil + isError=true），
	// 与客户端取消路径区分，不向上传播 error。
	setMCPToolTimeout(t, 1)
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.HasPrefix(req.Method, "notifications/"):
			w.WriteHeader(http.StatusAccepted)
		case req.Method == "initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"hang","version":"1"}}}`, req.ID)
		case req.Method == "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":{"tools":[{"name":"slow","description":"","inputSchema":{"type":"object","properties":{}}}]}}`, req.ID)
		default:
			<-release // call tool 挂起
		}
	}))
	defer hang.Close()
	defer close(release)

	reg := NewRegistry(nil)
	if err := reg.AddServer(context.Background(), streamableServerBasic("hang", hang.URL)); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	res, err := reg.Invoke(context.Background(), "hang__slow", nil)
	if err != nil {
		t.Fatalf("timeout should be swallowed as tool error, got error: %v", err)
	}
	if !mcps.IsToolError(res) {
		t.Fatalf("expected tool error result on timeout, got: %v", res)
	}
}

// toolResultTextOf 取工具结果中的 text，与 agent 包 toolResultText 的提取逻辑对齐。
func toolResultTextOf(result map[string]any) string {
	if content, ok := result["content"].([]any); ok {
		for _, item := range content {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					return text
				}
			}
		}
	}
	if sc, ok := result["structuredContent"].(map[string]any); ok {
		if text, ok := sc["text"].(string); ok {
			return text
		}
	}
	return ""
}
