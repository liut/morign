package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/settings"
)

type Invoker = mcps.Invoker
type HeaderFunc = transport.HTTPHeaderFunc

type Registry struct {
	tools    []mcps.ToolDescriptor
	invokers map[string]Invoker

	// 受限工具列表（需要 keeper 角色）
	privTools []mcps.ToolDescriptor

	clientInfo mcp.Implementation // MCP 客户端信息
	headerFunc HeaderFunc

	// MCP Servers 连接容器（name -> connection）
	servers   map[string]*MCPConnection
	serversMu sync.RWMutex
}

// RegistryOption 用于配置 Registry 的可选参数
type RegistryOption func(*Registry)

// WithClientInfo 设置 MCP 客户端信息
func WithClientInfo(name, version string) RegistryOption {
	return func(r *Registry) {
		r.clientInfo = mcp.Implementation{Name: name, Version: version}
	}
}

func WithHeaderFunc(hf HeaderFunc) RegistryOption {
	return func(r *Registry) {
		r.headerFunc = hf
	}
}

// NewRegistry 创建工具注册表
func NewRegistry(sto stores.Storage, opts ...RegistryOption) *Registry {
	r := &Registry{
		tools:    make([]mcps.ToolDescriptor, 0),
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	r.initTools(sto)

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// AddInvoker 添加自定义工具 invoker
// name: 工具名称
// fn: 工具调用函数
// desc: 工具描述
// inputSchema: 输入参数 schema
func (r *Registry) AddInvoker(name string, fn Invoker, desc string, inputSchema map[string]any) error {
	// 检查工具名是否冲突
	if err := r.checkToolNameConflict(name); err != nil {
		return err
	}

	// 注册 invoker
	r.invokers[name] = fn

	// 注册 ToolDescriptor
	r.tools = append(r.tools, mcps.ToolDescriptor{
		Name:        name,
		Description: desc,
		InputSchema: inputSchema,
	})

	logger().Infow("custom invoker added", "name", name)
	return nil
}

// Invoke 调用指定名称的工具
func (r *Registry) Invoke(ctx context.Context, name string, params map[string]any) (map[string]any, error) {
	if name == "" {
		return mcps.BuildToolErrorResult("tool name is empty"), nil
	}

	logger().Debugw("invoking", "toolName", name, "params", params)
	for key, invoker := range r.invokers {
		if strings.EqualFold(key, name) {
			return invoker(ctx, params)
		}
	}
	return mcps.BuildToolErrorResult("tool not found"), nil
}

func (r *Registry) initTools(sto stores.Storage) {
	// Add KB tools
	if sto != nil {
		// 公开工具：KBSearch
		r.tools = append(r.tools, kbSearchDescriptor)
		r.invokers[ToolNameKBSearch] = sto.Corpus().InvokerForSearch()

		// 受限工具：KBCreate (需要 keeper 角色)
		r.privTools = append(r.privTools, kbCreateDescriptor)
		r.invokers[ToolNameKBCreate] = sto.Corpus().InvokerForCreate()

		r.tools = append(r.tools,
			memoryListDescriptor, memoryRecallDescriptor,
			memoryStoreDescriptor, memoryForgetDescriptor,
		)
		r.invokers[ToolNameMemoryList] = sto.Convo().InvokerForMemoryList()
		r.invokers[ToolNameMemoryRecall] = sto.Convo().InvokerForMemoryRecall()
		r.invokers[ToolNameMemoryStore] = sto.Convo().InvokerForMemoryStore()
		r.invokers[ToolNameMemoryForget] = sto.Convo().InvokerForMemoryForget()

		// Capability tools - type assert to get X interface
		ctx := context.Background()
		if count, err := sto.Capability().CountCapability(ctx); err == nil && count > 10 {
			r.tools = append(r.tools, capabilityMatchDescriptor, capabilityInvokeDescriptor)
			r.invokers[ToolNameCapabilityMatch] = sto.Capability().InvokerForMatch()
			r.invokers[ToolNameCapabilityInvoke] = sto.Capability().InvokerForInvoke(stores.NewCapabilityInvoker(settings.Current.BusPrefix))
		}
	}

	// 公开工具：Fetch
	r.tools = append(r.tools, fetchDescriptor)
	r.invokers[ToolNameFetch] = r.callFetch

	logger().Debugw("init tools", "tools", mcps.ToolNames(r.tools), "priv", len(r.privTools))
}

// ApplyToolDescriptions 应用 preset 中的自定义工具描述
// descriptions: toolName -> description
func (r *Registry) ApplyToolDescriptions(descriptions map[string]string) {
	if len(descriptions) == 0 {
		return
	}

	// 更新内置工具描述
	for i := range r.tools {
		if desc, ok := descriptions[r.tools[i].Name]; ok && len(desc) > len(r.tools[i].Name) {
			r.tools[i].Description = desc
		}
	}
	for i := range r.privTools {
		if desc, ok := descriptions[r.privTools[i].Name]; ok && len(desc) > len(r.privTools[i].Name) {
			r.privTools[i].Description = desc
		}
	}

	logger().Infow("applied custom tool descriptions", "count", len(descriptions))
}

// ToolsFor 返回适合当前上下文的工具列表
// 如果用户有 keeper 角色，返回所有工具；否则只返回公开工具
func (r *Registry) ToolsFor(ctx context.Context) []mcps.ToolDescriptor {
	// TODO: 工具过滤器

	if stores.IsKeeper(ctx) {
		// 合并公开工具和受限工具
		return append(r.tools, r.privTools...)
	}
	return r.tools
}

// convertInputSchema 将 ToolInputSchema 转换为 map[string]any
func convertInputSchema(schema mcp.ToolInputSchema) map[string]any {
	properties := schema.Properties
	if properties == nil {
		properties = make(map[string]any)
	}
	required := schema.Required
	if required == nil {
		required = make([]string, 0)
	}
	return map[string]any{
		"type":       schema.Type,
		"properties": properties,
		"required":   required,
	}
}

// convertMCPToolResult 将 MCP 工具结果转换为本地格式
func convertMCPToolResult(result *mcp.CallToolResult) map[string]any {
	if len(result.Content) == 0 {
		return mcps.BuildToolSuccessResult(nil)
	}

	// 取第一个 content
	content := result.Content[0]
	if textContent, ok := content.(mcp.TextContent); ok {
		if result.IsError {
			return mcps.BuildToolErrorResult(textContent.Text)
		}
		return mcps.BuildToolSuccessResult(map[string]any{
			"text": textContent.Text,
		})
	}

	// 其他类型直接返回
	return mcps.BuildToolSuccessResult(map[string]any{
		"content": content,
	})
}

// AddServer 添加一个 MCP Server 并初始化连接
// 仅支持远程传输类型（SSE 或 Streamable）
func (r *Registry) AddServer(ctx context.Context, server *mcps.ServerBasic) error {
	// 验证传输类型
	if !server.TransType.IsRemote() {
		return fmt.Errorf("unsupported transport type: %v (only SSE and Streamable are supported)", server.TransType)
	}

	// 验证 URL
	if server.URL == "" {
		return fmt.Errorf("URL is required")
	}

	// 检查名称冲突
	if err := r.checkToolNameConflict(server.Name); err != nil {
		return err
	}

	hf := HeaderFunc(server.HeaderFunc)
	if hf == nil {
		hf = r.headerFunc
	}

	// 创建 transport（使用接口类型）
	var tp transport.Interface
	var err error
	switch server.TransType {
	case mcps.TransTypeSSE:
		tp, err = transport.NewSSE(server.URL,
			transport.WithHeaderFunc(hf))
	case mcps.TransTypeStreamable:
		tp, err = transport.NewStreamableHTTP(server.URL,
			transport.WithHTTPHeaderFunc(hf))
	default:
		return fmt.Errorf("unsupported transport type: %v", server.TransType)
	}
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// 创建并启动 client
	c := client.NewClient(tp)
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	logger().Debugw("MCP initializing", "name", server.Name, "uri", server.URL, "type", server.TransType)
	// 初始化 MCP 协议
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      r.clientInfo,
		},
	}); err != nil {
		_ = c.Close()
		return fmt.Errorf("failed to initialize MCP: %w", err)
	}

	// 获取工具列表
	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// 检查新工具名是否冲突
	for _, tool := range result.Tools {
		if err := r.checkToolNameConflict(tool.Name); err != nil {
			_ = c.Close()
			return err
		}
	}

	// 注册工具
	r.serversMu.Lock()
	mcpc := &MCPConnection{
		Name:      server.Name,
		URL:       server.URL,
		TransType: server.TransType,
		client:    c,
	}
	toolNames := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		toolKey := mcpc.getToolKey(tool.Name)
		inputSchema := convertInputSchema(tool.InputSchema)
		r.tools = append(r.tools, mcps.ToolDescriptor{
			Name:        toolKey,
			Description: tool.Description,
			InputSchema: inputSchema,
		})
		r.invokers[toolKey] = func(ctx context.Context, params map[string]any) (map[string]any, error) {
			return r.callServerTool(ctx, server.Name, tool.Name, params)
		}
		toolNames = append(toolNames, toolKey)
		logger().Infow("MCP tool registered", "server", server.Name, "tool", tool.Name)
	}
	mcpc.toolNames = toolNames
	r.servers[server.Name] = mcpc
	r.serversMu.Unlock()

	logger().Debugw("MCP server added", "name", server.Name, "url", server.URL, "tools", len(result.Tools))
	return nil
}

// checkToolNameConflict 检查工具名是否冲突
func (r *Registry) checkToolNameConflict(name string) error {
	// 检查是否与内置工具冲突
	switch name {
	case ToolNameKBSearch, ToolNameKBCreate, ToolNameFetch,
		ToolNameMemoryList, ToolNameMemoryRecall, ToolNameMemoryStore, ToolNameMemoryForget:
		return fmt.Errorf("tool name %q conflicts with built-in tool", name)
	}

	// 检查是否与已注册的工具冲突
	for _, t := range r.tools {
		if t.Name == name {
			return fmt.Errorf("tool name %q already exists", name)
		}
	}
	for _, t := range r.privTools {
		if t.Name == name {
			return fmt.Errorf("tool name %q already exists", name)
		}
	}

	// 检查是否与已注册的 server 冲突
	r.serversMu.RLock()
	for _, s := range r.servers {
		if s.Name == name {
			r.serversMu.RUnlock()
			return fmt.Errorf("server %q already exists", name)
		}
	}
	r.serversMu.RUnlock()

	return nil
}

// callServerTool 调用 MCP Server 工具
func (r *Registry) callServerTool(ctx context.Context, serverName, toolName string, params map[string]any) (map[string]any, error) {
	r.serversMu.RLock()
	server, ok := r.servers[serverName]
	r.serversMu.RUnlock()

	if !ok {
		return mcps.BuildToolErrorResult("server not found"), nil
	}

	// 确保 params 不为空
	if params == nil {
		params = make(map[string]any)
	}

	result, err := server.client.CallTool(mcps.ContextWithServerName(ctx, serverName),
		mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      toolName,
				Arguments: params,
			},
		})
	if err != nil {
		logger().Errorw("MCP server tool call failed", "server", serverName, "tool", toolName, "err", err)
		return mcps.BuildToolErrorResult(err.Error()), nil
	}

	return convertMCPToolResult(result), nil
}

// LoadServers 加载所有 Running 状态的 MCP Server
func (r *Registry) LoadServers(ctx context.Context, sto stores.Storage) error {
	if sto == nil {
		logger().Warnw("no storage configured, skipping MCP server load")
		return nil
	}

	spec := &stores.MCPServerSpec{
		IsActive: "true",
	}
	spec.Limit = 2
	spec.Sort = "created DESC"
	servers, _, err := sto.MCP().ListServer(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to list MCP servers: %w", err)
	}

	for i := range servers {
		if !servers[i].TransType.IsRemote() {
			logger().Infow("skipping non-remote MCP server", "name", servers[i].Name, "type", servers[i].TransType)
			continue
		}
		if err := r.AddServer(ctx, &servers[i].ServerBasic); err != nil {
			logger().Warnw("failed to load MCP server", "name", servers[i].Name, "err", err)
			continue
		}
		logger().Infow("loaded MCP server", "name", servers[i].Name)
	}

	logger().Info("MCP servers loaded", "count", len(servers))
	return nil
}

// RemoveServer 移除 MCP Server 连接
func (r *Registry) RemoveServer(name string) error {
	r.serversMu.Lock()
	defer r.serversMu.Unlock()

	conn, ok := r.servers[name]
	if !ok {
		return fmt.Errorf("server %q not found", name)
	}

	// 关闭 client 连接
	if conn.client != nil {
		_ = conn.client.Close()
	}

	// 使用 toolNames 移除工具
	for _, toolName := range conn.toolNames {
		delete(r.invokers, toolName)
	}

	// 过滤掉该 server 的工具
	newTools := make([]mcps.ToolDescriptor, 0, len(r.tools))
	for _, tool := range r.tools {
		if !slices.Contains(conn.toolNames, tool.Name) {
			newTools = append(newTools, tool)
		}
	}
	r.tools = newTools
	delete(r.servers, name)
	logger().Infow("MCP server removed", "name", name)
	return nil
}
