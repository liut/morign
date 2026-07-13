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

// channelToolSet 存储单个频道的专属工具集
type channelToolSet struct {
	tools    []mcps.ToolDescriptor
	invokers map[string]Invoker
	servers  map[string]*MCPConnection
	mu       sync.RWMutex
}

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

	toolsMu sync.RWMutex // 保护 tools、privTools、invokers 的并发访问

	// 频道专属工具集（channel name -> tool set）
	channelTools map[string]*channelToolSet
	channelMu    sync.RWMutex
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
		tools:        make([]mcps.ToolDescriptor, 0),
		invokers:     make(map[string]Invoker),
		servers:      make(map[string]*MCPConnection),
		channelTools: make(map[string]*channelToolSet),
	}
	r.initTools(sto)

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RegisterInvoker 注册一个工具及其调用函数。name 为空或 inv 为 nil 时静默忽略。
func (r *Registry) RegisterInvoker(name string, inv Invoker) {
	if name == "" || inv == nil {
		return
	}
	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()
	r.tools = append(r.tools, mcps.ToolDescriptor{Name: name})
	r.invokers[name] = inv
}

// Invoke 调用指定名称的工具，频道工具优先
func (r *Registry) Invoke(ctx context.Context, name string, params map[string]any) (map[string]any, error) {
	if name == "" {
		return mcps.BuildToolErrorResult("tool name is empty"), nil
	}

	logger().Debugw("invoking", "toolName", name, "params", params)

	// 先查频道专属 invokers
	if ch := mcps.ChannelFromContext(ctx); ch != "" {
		r.channelMu.RLock()
		cs := r.channelTools[ch]
		r.channelMu.RUnlock()
		if cs != nil {
			cs.mu.RLock()
			for key, invoker := range cs.invokers {
				if strings.EqualFold(key, name) {
					cs.mu.RUnlock()
					return invoker(ctx, params)
				}
			}
			cs.mu.RUnlock()
		}
	}

	// 再查全局 invokers
	r.toolsMu.RLock()
	defer r.toolsMu.RUnlock()
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

	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()

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
// 当 context 中有频道信息时，额外合并该频道的专属工具（始终公开）
func (r *Registry) ToolsFor(ctx context.Context) []mcps.ToolDescriptor {
	r.toolsMu.RLock()
	defer r.toolsMu.RUnlock()

	base := make([]mcps.ToolDescriptor, len(r.tools))
	copy(base, r.tools)
	if stores.IsKeeper(ctx) {
		base = append(base, r.privTools...)
	}

	// 合并频道专属工具（始终公开）
	if ch := mcps.ChannelFromContext(ctx); ch != "" {
		r.channelMu.RLock()
		cs := r.channelTools[ch]
		r.channelMu.RUnlock()
		if cs != nil {
			cs.mu.RLock()
			base = append(base, cs.tools...)
			cs.mu.RUnlock()
		}
	}

	return base
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

// AddServer 添加 MCP Server 并初始化连接。server.Channel 非空时注册为频道专属工具。
func (r *Registry) AddServer(ctx context.Context, server *mcps.ServerBasic) error {
	if !server.TransType.IsRemote() {
		return fmt.Errorf("unsupported transport type: %v", server.TransType)
	}
	if server.URL == "" {
		return fmt.Errorf("URL is required")
	}

	// 全局 server 检查名称冲突（快速失败，网络 I/O 前）
	if server.Channel == "" {
		if err := r.checkServerNameConflict(server.Name); err != nil {
			return err
		}
	}

	hf := HeaderFunc(server.HeaderFunc)
	if hf == nil {
		hf = r.headerFunc
	}

	var tp transport.Interface
	var err error
	switch server.TransType {
	case mcps.TransTypeSSE:
		tp, err = transport.NewSSE(server.URL, transport.WithHeaderFunc(hf))
	case mcps.TransTypeStreamable:
		tp, err = transport.NewStreamableHTTP(server.URL, transport.WithHTTPHeaderFunc(hf))
	default:
		return fmt.Errorf("unsupported transport type: %v", server.TransType)
	}
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

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

	// 注册工具 —— 锁覆盖冲突检测 + 注册，不跨越网络 I/O
	if server.Channel == "" {
		r.toolsMu.Lock()
		r.serversMu.Lock()
		defer r.toolsMu.Unlock()
		defer r.serversMu.Unlock()
		return r.registerToolsLocked(&r.tools, r.invokers, r.servers, server, c, result)
	}

	r.channelMu.Lock()
	cs := r.channelTools[server.Channel]
	if cs == nil {
		cs = &channelToolSet{
			tools:    make([]mcps.ToolDescriptor, 0),
			invokers: make(map[string]Invoker),
			servers:  make(map[string]*MCPConnection),
		}
		r.channelTools[server.Channel] = cs
	}
	r.channelMu.Unlock()

	cs.mu.Lock()
	defer cs.mu.Unlock()
	r.toolsMu.RLock()
	defer r.toolsMu.RUnlock()
	return r.registerToolsLocked(&cs.tools, cs.invokers, cs.servers, server, c, result)
}

// registerToolsLocked 注册 MCP Server 工具到指定容器（调用方负责加锁）
func (r *Registry) registerToolsLocked(
	tools *[]mcps.ToolDescriptor, invokers map[string]Invoker, servers map[string]*MCPConnection,
	server *mcps.ServerBasic, c *client.Client, result *mcp.ListToolsResult,
) error {
	// 校验工具名冲突（处理并发 AddServer 的 TOCTOU）
	for _, tool := range result.Tools {
		toolKey := genToolKey(server.Name, tool.Name, server.Channel)
		if err := r.checkToolNameConflict(toolKey); err != nil {
			_ = c.Close()
			return err
		}
	}

	mcpc := newMCPConnectionFromServer(server)
	mcpc.client = c
	tNames := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		toolKey := genToolKey(server.Name, tool.Name, server.Channel)
		inputSchema := convertInputSchema(tool.InputSchema)
		*tools = append(*tools, mcps.ToolDescriptor{
			Name: toolKey, Description: tool.Description, InputSchema: inputSchema})
		client := c
		invokers[toolKey] = func(ctx context.Context, params map[string]any) (map[string]any, error) {
			return callServerToolWithClient(ctx, client, tool.Name, params)
		}
		tNames = append(tNames, toolKey)
		logger().Infow("MCP tool registered", "toolKey", toolKey)
	}
	mcpc.toolNames = tNames
	servers[server.Name] = mcpc

	logger().Debugw("MCP server added", "server", server.Name, "url", server.URL, "tools", len(result.Tools))
	return nil
}

// RemoveChannelTools 移除指定频道的所有专属工具
func (r *Registry) RemoveChannelTools(channel string) {
	r.channelMu.Lock()
	cs := r.channelTools[channel]
	if cs == nil {
		r.channelMu.Unlock()
		return
	}
	delete(r.channelTools, channel)
	r.channelMu.Unlock()

	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, s := range cs.servers {
		if s.client != nil {
			_ = s.client.Close()
		}
	}
	logger().Infow("channel MCP tools removed", "channel", channel)
}

// checkServerNameConflict 检查 server 名是否冲突（server 名独立于工具名）
func (r *Registry) checkServerNameConflict(name string) error {
	switch name {
	case ToolNameKBSearch, ToolNameKBCreate, ToolNameFetch,
		ToolNameMemoryList, ToolNameMemoryRecall, ToolNameMemoryStore, ToolNameMemoryForget:
		return fmt.Errorf("server name %q conflicts with built-in tool", name)
	}

	r.serversMu.RLock()
	defer r.serversMu.RUnlock()
	for _, s := range r.servers {
		if s.Name == name {
			return fmt.Errorf("server %q already exists", name)
		}
	}
	return nil
}

// checkToolNameConflict 检查工具名是否冲突（工具名独立于 server 名）
func (r *Registry) checkToolNameConflict(name string) error {
	switch name {
	case ToolNameKBSearch, ToolNameKBCreate, ToolNameFetch,
		ToolNameMemoryList, ToolNameMemoryRecall, ToolNameMemoryStore, ToolNameMemoryForget:
		return fmt.Errorf("tool name %q conflicts with built-in tool", name)
	}

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
	return nil
}

// ChannelServerStatus 表示频道 MCP Server 的状态
type ChannelServerStatus struct {
	Name  string   `json:"name"`
	URL   string   `json:"url"`
	Tools []string `json:"tools"`
}

// ChannelServerStatuses 返回指定频道的 MCP Server 状态列表
func (r *Registry) ChannelServerStatuses(channel string) []ChannelServerStatus {
	r.channelMu.RLock()
	cs := r.channelTools[channel]
	r.channelMu.RUnlock()
	if cs == nil {
		return nil
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()
	result := make([]ChannelServerStatus, 0, len(cs.servers))
	for _, s := range cs.servers {
		result = append(result, ChannelServerStatus{
			Name:  s.Name,
			URL:   s.URL,
			Tools: s.toolNames,
		})
	}
	return result
}

// callServerToolWithClient 使用已捕获的 client 调用 MCP 工具，无需查 servers map
func callServerToolWithClient(ctx context.Context, c *client.Client, toolName string, params map[string]any) (map[string]any, error) {
	if params == nil {
		params = make(map[string]any)
	}

	result, err := c.CallTool(ctx,
		mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      toolName,
				Arguments: params,
			},
		})
	if err != nil {
		logger().Errorw("MCP server tool call failed", "tool", toolName, "err", err)
		return mcps.BuildToolErrorResult(fmt.Sprintf("tool '%s' call failed: %s", toolName, err)), nil
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
	r.toolsMu.Lock()
	r.serversMu.Lock()

	conn, ok := r.servers[name]
	if !ok {
		r.serversMu.Unlock()
		r.toolsMu.Unlock()
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
	r.serversMu.Unlock()
	r.toolsMu.Unlock()

	logger().Infow("MCP server removed", "name", name)
	return nil
}
