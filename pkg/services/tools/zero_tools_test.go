package tools

import (
	"context"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/liut/morign/pkg/models/mcps"
)

// --- genToolKey ---

func TestGenToolKey(t *testing.T) {
	tests := []struct {
		sn, tn, ch string
		want       string
	}{
		{"srv", "tool", "", "srv__tool"},
		{"srv", "tool", "wecom", "wecom__srv__tool"},
		{"a", "b", "", "a__b"},
		{"a", "b", "feishu", "feishu__a__b"},
	}

	for _, tt := range tests {
		got := genToolKey(tt.sn, tt.tn, tt.ch)
		if got != tt.want {
			t.Errorf("genToolKey(%q, %q, %q) = %q, want %q", tt.sn, tt.tn, tt.ch, got, tt.want)
		}
	}
}

// --- newMCPConnectionFromServer ---

func TestNewMCPConnectionFromServer(t *testing.T) {
	sb := &mcps.ServerBasic{
		Name:      "test-srv",
		URL:       "http://localhost:8080/sse",
		TransType: mcps.TransTypeSSE,
		Channel:   "wecom",
	}
	mcpc := newMCPConnectionFromServer(sb)
	if mcpc.Name != "test-srv" {
		t.Errorf("Name = %q, want %q", mcpc.Name, "test-srv")
	}
	if mcpc.URL != "http://localhost:8080/sse" {
		t.Errorf("URL = %q, want %q", mcpc.URL, "http://localhost:8080/sse")
	}
	if mcpc.Channel != "wecom" {
		t.Errorf("Channel = %q, want %q", mcpc.Channel, "wecom")
	}
	if mcpc.client != nil {
		t.Error("client should be nil")
	}
}

// --- convertInputSchema ---

func TestConvertInputSchema(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"key": map[string]any{"type": "string"},
		},
		Required: []string{"key"},
	}
	result := convertInputSchema(schema)
	if result["type"] != "object" {
		t.Errorf("type = %v", result["type"])
	}
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not map[string]any")
	}
	if len(props) != 1 {
		t.Errorf("properties len = %d, want 1", len(props))
	}
	req, ok := result["required"].([]string)
	if !ok {
		t.Fatal("required is not []string")
	}
	if len(req) != 1 || req[0] != "key" {
		t.Errorf("required = %v", req)
	}
}

func TestConvertInputSchemaNilFields(t *testing.T) {
	schema := mcp.ToolInputSchema{Type: "object"}
	result := convertInputSchema(schema)
	if _, ok := result["properties"].(map[string]any); !ok {
		t.Error("properties should be empty map when nil")
	}
	if _, ok := result["required"].([]string); !ok {
		t.Error("required should be empty slice when nil")
	}
}

// --- convertMCPToolResult ---

func TestConvertMCPToolResult(t *testing.T) {
	t.Run("empty content", func(t *testing.T) {
		result := convertMCPToolResult(&mcp.CallToolResult{})
		content, ok := result["content"].([]map[string]any)
		if !ok || len(content) == 0 || content[0]["text"] != "ok" {
			t.Errorf("empty content should return default ok, got %v", result)
		}
	})

	t.Run("text success", func(t *testing.T) {
		result := convertMCPToolResult(&mcp.CallToolResult{
			Content: []mcp.Content{mcp.TextContent{Text: "hello"}},
		})
		sc, ok := result["structuredContent"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent, got %v", result)
		}
		if sc["text"] != "hello" {
			t.Errorf("text = %q", sc["text"])
		}
	})

	t.Run("text error", func(t *testing.T) {
		result := convertMCPToolResult(&mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{mcp.TextContent{Text: "something went wrong"}},
		})
		if result["isError"] != true {
			t.Error("should be error")
		}
		content, _ := result["content"].([]map[string]any)
		if len(content) == 0 || content[0]["text"] != "something went wrong" {
			t.Error("error content not preserved")
		}
	})

	t.Run("non-text content", func(t *testing.T) {
		result := convertMCPToolResult(&mcp.CallToolResult{
			Content: []mcp.Content{mcp.EmbeddedResource{}},
		})
		sc, _ := result["structuredContent"].(map[string]any)
		if _, ok := sc["content"]; !ok {
			t.Error("should have content field for non-text type")
		}
	})
}

// --- Registry basics ---

func newTestRegistry() *Registry {
	return NewRegistry(nil)
}

func TestNewRegistry(t *testing.T) {
	r := newTestRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.tools) == 0 {
		t.Error("should have at least fetch tool")
	}
	if len(r.invokers) == 0 {
		t.Error("should have at least fetch invoker")
	}
	if r.channelTools == nil {
		t.Error("channelTools map should be initialized")
	}
}

func TestWithClientInfo(t *testing.T) {
	r := NewRegistry(nil, WithClientInfo("test-app", "1.0"))
	if r.clientInfo.Name != "test-app" {
		t.Errorf("clientInfo.Name = %q", r.clientInfo.Name)
	}
	if r.clientInfo.Version != "1.0" {
		t.Errorf("clientInfo.Version = %q", r.clientInfo.Version)
	}
}

// --- ToolsFor ---

func TestToolsForReturnsPublicTools(t *testing.T) {
	r := newTestRegistry()
	ctx := context.Background()
	tools := r.ToolsFor(ctx)
	if len(tools) == 0 {
		t.Error("should have at least fetch tool for non-keeper")
	}
	found := false
	for _, td := range tools {
		if td.Name == ToolNameFetch {
			found = true
			break
		}
	}
	if !found {
		t.Error("fetch tool not in public tools")
	}
}

func TestToolsForWithChannel(t *testing.T) {
	r := newTestRegistry()
	chTool := mcps.ToolDescriptor{Name: "wecom__msg__send", Description: "send message"}
	r.channelMu.Lock()
	cs := &channelToolSet{
		tools:    []mcps.ToolDescriptor{chTool},
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	r.channelTools["wecom"] = cs
	r.channelMu.Unlock()

	ctx := mcps.ContextWithChannel(context.Background(), "wecom")
	tools := r.ToolsFor(ctx)
	found := false
	for _, td := range tools {
		if td.Name == "wecom__msg__send" {
			found = true
			break
		}
	}
	if !found {
		t.Error("channel tools should be included")
	}
}

func TestToolsForWithoutChannel(t *testing.T) {
	r := newTestRegistry()
	chTool := mcps.ToolDescriptor{Name: "wecom__msg__send", Description: "send message"}
	r.channelMu.Lock()
	cs := &channelToolSet{
		tools:    []mcps.ToolDescriptor{chTool},
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	r.channelTools["wecom"] = cs
	r.channelMu.Unlock()

	ctx := context.Background()
	tools := r.ToolsFor(ctx)
	for _, td := range tools {
		if td.Name == "wecom__msg__send" {
			t.Error("channel tools should NOT be included without channel context")
		}
	}
}

// --- Invoke ---

func TestInvokeEmptyName(t *testing.T) {
	r := newTestRegistry()
	result, err := r.Invoke(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["isError"] != true {
		t.Error("empty name should return error result")
	}
}

func TestInvokeNotFound(t *testing.T) {
	r := newTestRegistry()
	result, err := r.Invoke(context.Background(), "nonexistent_tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["isError"] != true {
		t.Error("unknown tool should return error result")
	}
}

func TestInvokeChannelTool(t *testing.T) {
	r := newTestRegistry()

	called := false
	invoker := func(ctx context.Context, params map[string]any) (map[string]any, error) {
		called = true
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}, nil
	}

	r.channelMu.Lock()
	cs := &channelToolSet{
		tools:    []mcps.ToolDescriptor{{Name: "wecom__srv__hello"}},
		invokers: map[string]Invoker{"wecom__srv__hello": invoker},
		servers:  make(map[string]*MCPConnection),
	}
	r.channelTools["wecom"] = cs
	r.channelMu.Unlock()

	ctx := mcps.ContextWithChannel(context.Background(), "wecom")
	result, err := r.Invoke(ctx, "wecom__srv__hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("channel invoker should have been called")
	}
	if result["isError"] == true {
		t.Error("should be success")
	}
}

func TestInvokeChannelToolNotFoundFallsBackToGlobal(t *testing.T) {
	r := newTestRegistry()

	r.channelMu.Lock()
	cs := &channelToolSet{
		tools:    []mcps.ToolDescriptor{{Name: "wecom__srv__other"}},
		invokers: map[string]Invoker{},
		servers:  make(map[string]*MCPConnection),
	}
	r.channelTools["wecom"] = cs
	r.channelMu.Unlock()

	ctx := mcps.ContextWithChannel(context.Background(), "wecom")
	result, _ := r.Invoke(ctx, "wecom__srv__missing", nil)
	if result["isError"] != true {
		t.Error("should fall through to global and return error")
	}
}

// --- ApplyToolDescriptions ---

func TestApplyToolDescriptions(t *testing.T) {
	r := newTestRegistry()
	for i := range r.tools {
		if r.tools[i].Name == ToolNameFetch {
			r.tools[i].Description = "old"
			break
		}
	}

	desc := map[string]string{ToolNameFetch: "new longer description for fetch tool"}
	r.ApplyToolDescriptions(desc)

	for _, td := range r.tools {
		if td.Name == ToolNameFetch && td.Description != "new longer description for fetch tool" {
			t.Errorf("description not updated: %q", td.Description)
		}
	}
}

func TestApplyToolDescriptionsEmpty(t *testing.T) {
	r := newTestRegistry()
	r.ApplyToolDescriptions(nil)
	r.ApplyToolDescriptions(map[string]string{})
}

func TestApplyToolDescriptionsTooShort(t *testing.T) {
	r := newTestRegistry()
	for i := range r.tools {
		if r.tools[i].Name == ToolNameFetch {
			r.tools[i].Description = "a long description"
			break
		}
	}

	desc := map[string]string{ToolNameFetch: "short"}
	r.ApplyToolDescriptions(desc)

	for _, td := range r.tools {
		if td.Name == ToolNameFetch && td.Description != "a long description" {
			t.Error("short description should not overwrite")
		}
	}
}

// --- checkServerNameConflict ---

func TestCheckServerNameConflict(t *testing.T) {
	r := newTestRegistry()

	t.Run("built-in tool name", func(t *testing.T) {
		err := r.checkServerNameConflict("kb_search")
		if err == nil {
			t.Error("kb_search as server name should conflict with built-in tool")
		}
	})

	t.Run("ok name", func(t *testing.T) {
		err := r.checkServerNameConflict("my-custom-server")
		if err != nil {
			t.Errorf("unexpected conflict: %v", err)
		}
	})

	t.Run("duplicate server", func(t *testing.T) {
		r.serversMu.Lock()
		r.servers["existing"] = &MCPConnection{Name: "existing"}
		r.serversMu.Unlock()

		err := r.checkServerNameConflict("existing")
		if err == nil {
			t.Error("duplicate server name should conflict")
		}
	})
}

// --- checkToolNameConflict ---

func TestCheckToolNameConflict(t *testing.T) {
	r := newTestRegistry()

	t.Run("built-in tool name", func(t *testing.T) {
		err := r.checkToolNameConflict("kb_search")
		if err == nil {
			t.Error("kb_search should conflict with built-in tool")
		}
	})

	t.Run("existing tool", func(t *testing.T) {
		err := r.checkToolNameConflict(ToolNameFetch)
		if err == nil {
			t.Error("fetch should conflict with existing tool")
		}
	})

	t.Run("priv tool", func(t *testing.T) {
		r.toolsMu.Lock()
		r.privTools = append(r.privTools, mcps.ToolDescriptor{Name: "secret_tool"})
		r.toolsMu.Unlock()

		err := r.checkToolNameConflict("secret_tool")
		if err == nil {
			t.Error("priv tool name should conflict")
		}
	})

	t.Run("ok name", func(t *testing.T) {
		err := r.checkToolNameConflict("brand_new_tool")
		if err != nil {
			t.Errorf("unexpected conflict: %v", err)
		}
	})
}

// --- RemoveServer ---

func TestRemoveServer(t *testing.T) {
	r := newTestRegistry()

	r.serversMu.Lock()
	r.servers["dummy"] = &MCPConnection{
		Name:      "dummy",
		toolNames: []string{"dummy__tool"},
	}
	r.serversMu.Unlock()
	r.toolsMu.Lock()
	r.tools = append(r.tools, mcps.ToolDescriptor{Name: "dummy__tool"})
	r.invokers["dummy__tool"] = func(ctx context.Context, p map[string]any) (map[string]any, error) {
		return nil, nil
	}
	r.toolsMu.Unlock()

	err := r.RemoveServer("dummy")
	if err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}

	r.serversMu.RLock()
	_, ok := r.servers["dummy"]
	r.serversMu.RUnlock()
	if ok {
		t.Error("server should be removed")
	}

	r.toolsMu.RLock()
	_, ok = r.invokers["dummy__tool"]
	for _, td := range r.tools {
		if td.Name == "dummy__tool" {
			ok = true
		}
	}
	r.toolsMu.RUnlock()
	if ok {
		t.Error("tool should be removed")
	}
}

func TestRemoveServerNotFound(t *testing.T) {
	r := newTestRegistry()
	err := r.RemoveServer("nonexistent")
	if err == nil {
		t.Error("should return error for unknown server")
	}
}

// --- RemoveChannelTools ---

func TestRemoveChannelTools(t *testing.T) {
	r := newTestRegistry()

	chTool := mcps.ToolDescriptor{Name: "wecom__srv__hello"}
	r.channelMu.Lock()
	r.channelTools["wecom"] = &channelToolSet{
		tools:    []mcps.ToolDescriptor{chTool},
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	r.channelMu.Unlock()

	r.RemoveChannelTools("wecom")

	r.channelMu.RLock()
	cs := r.channelTools["wecom"]
	r.channelMu.RUnlock()
	if cs != nil {
		t.Error("channel tools should be removed")
	}
}

func TestRemoveChannelToolsNotExist(t *testing.T) {
	r := newTestRegistry()
	r.RemoveChannelTools("nonexistent")
}

// --- ChannelServerStatuses ---

func TestChannelServerStatuses(t *testing.T) {
	r := newTestRegistry()

	r.channelMu.Lock()
	r.channelTools["wecom"] = &channelToolSet{
		tools:    make([]mcps.ToolDescriptor, 0),
		invokers: make(map[string]Invoker),
		servers: map[string]*MCPConnection{
			"msg": {Name: "msg", URL: "http://msg/sse", toolNames: []string{"send", "recv"}},
		},
	}
	r.channelMu.Unlock()

	statuses := r.ChannelServerStatuses("wecom")
	if len(statuses) != 1 {
		t.Fatalf("len = %d, want 1", len(statuses))
	}
	if statuses[0].Name != "msg" {
		t.Errorf("Name = %q", statuses[0].Name)
	}
	if len(statuses[0].Tools) != 2 {
		t.Errorf("Tools len = %d", len(statuses[0].Tools))
	}
}

func TestChannelServerStatusesNotExist(t *testing.T) {
	r := newTestRegistry()
	statuses := r.ChannelServerStatuses("nonexistent")
	if statuses != nil {
		t.Error("should return nil for unknown channel")
	}
}

// --- AddServer validation ---

func TestAddServerNonRemoteTransType(t *testing.T) {
	r := newTestRegistry()
	err := r.AddServer(context.Background(), &mcps.ServerBasic{
		TransType: mcps.TransTypeStdIO,
	})
	if err == nil {
		t.Error("StdIO trans type should be rejected")
	}
}

func TestAddServerEmptyURL(t *testing.T) {
	r := newTestRegistry()
	err := r.AddServer(context.Background(), &mcps.ServerBasic{
		TransType: mcps.TransTypeSSE,
	})
	if err == nil {
		t.Error("empty URL should be rejected")
	}
}

// --- ResultLogs ---

func TestResultLogsString(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var rl ResultLogs
		if rl.String() != "" {
			t.Errorf("nil should return empty string, got %q", rl.String())
		}
	})

	t.Run("empty", func(t *testing.T) {
		rl := ResultLogs{}
		if rl.String() != "{}" {
			t.Errorf("empty should return {}, got %q", rl.String())
		}
	})

	t.Run("with content", func(t *testing.T) {
		rl := ResultLogs{"key": "value"}
		s := rl.String()
		if len(s) == 0 || s[0] != '{' {
			t.Errorf("should start with {, got %q", s)
		}
	})
}

// --- Concurrent access safety ---

func TestConcurrentToolsFor(t *testing.T) {
	r := newTestRegistry()
	ctx := context.Background()
	done := make(chan struct{})

	for range 10 {
		go func() {
			for range 100 {
				_ = r.ToolsFor(ctx)
			}
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}
}

func TestConcurrentInvoke(t *testing.T) {
	r := newTestRegistry()
	ctx := context.Background()
	done := make(chan struct{})

	for range 10 {
		go func() {
			for range 100 {
				_, _ = r.Invoke(ctx, ToolNameFetch, nil)
			}
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}
}
