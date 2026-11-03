package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	auth "github.com/liut/simpauth"
	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/settings"
)

const (
	keeperToolName  = "kb_create"
	channelToolName = "wecom__srv__search"
)

func keeperCtx() context.Context {
	return auth.ContextWithUser(context.Background(), &auth.User{
		OID:   "01H0000000000000000000001",
		UID:   "keeper",
		Roles: auth.Names{settings.Current.KeeperRole},
	})
}

func userCtx() context.Context {
	return auth.ContextWithUser(context.Background(), &auth.User{
		OID: "01H0000000000000000000002",
		UID: "user",
	})
}

func channelCtx(ctx context.Context, channel string) context.Context {
	return mcps.ContextWithChannel(ctx, channel)
}

// newPolicyRegistry 构造带受限工具与频道工具的注册表，calls 记录实际执行过的工具名。
func newPolicyRegistry(calls *[]string) *Registry {
	reg := NewRegistry(nil)

	// 受限工具：仅 keeper 可见可执行
	reg.privTools = append(reg.privTools, mcps.ToolDescriptor{Name: keeperToolName})
	reg.invokers[keeperToolName] = func(ctx context.Context, params map[string]any) (map[string]any, error) {
		*calls = append(*calls, keeperToolName)
		return mcps.BuildToolSuccessResult("created"), nil
	}
	reg.categories[keeperToolName] = SourceBuiltinKeeper

	// 频道工具：仅 wecom 频道可见可执行
	cs := &channelToolSet{
		tools:    []mcps.ToolDescriptor{{Name: channelToolName}},
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	cs.invokers[channelToolName] = func(ctx context.Context, params map[string]any) (map[string]any, error) {
		*calls = append(*calls, channelToolName)
		return mcps.BuildToolSuccessResult("searched"), nil
	}
	reg.channelTools["wecom"] = cs

	return reg
}

func visibleNames(reg *Registry, ctx context.Context) []string {
	descs := reg.ToolsFor(ctx)
	names := make([]string, 0, len(descs))
	for _, d := range descs {
		names = append(names, d.Name)
	}
	return names
}

// isPermissionDenied 判断结果是否为门控层拒绝。
func isPermissionDenied(result map[string]any) bool {
	return strings.Contains(resultText(result), "permission denied")
}

func TestToolsForVisibility(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	tests := []struct {
		name    string
		ctx     context.Context
		want    []string
		notWant []string
	}{
		{
			name: "普通用户看不到受限工具",
			ctx:  userCtx(),
			want: []string{ToolNameFetch, ToolNameCurrentTime},
			notWant: []string{
				keeperToolName,
			},
		},
		{
			name: "keeper 可见受限工具",
			ctx:  keeperCtx(),
			want: []string{ToolNameFetch, ToolNameCurrentTime, keeperToolName},
		},
		{
			name: "频道上下文可见本频道工具",
			ctx:  channelCtx(userCtx(), "wecom"),
			want: []string{ToolNameFetch, ToolNameCurrentTime, channelToolName},
			notWant: []string{
				keeperToolName,
			},
		},
		{
			name: "他频道上下文看不到本频道工具",
			ctx:  channelCtx(userCtx(), "feishu"),
			want: []string{ToolNameFetch, ToolNameCurrentTime},
			notWant: []string{
				channelToolName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := visibleNames(reg, tt.ctx)
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("visible tools = %v, want to contain %q", got, want)
				}
			}
			for _, notWant := range tt.notWant {
				if slices.Contains(got, notWant) {
					t.Errorf("visible tools = %v, want NOT to contain %q", got, notWant)
				}
			}
		})
	}
}

func TestInvokeDeniesRestrictedTool(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	result, err := reg.Invoke(userCtx(), keeperToolName, nil)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !isPermissionDenied(result) {
		t.Errorf("result = %v, want permission denied", resultText(result))
	}
	if len(calls) != 0 {
		t.Errorf("invoker executed for a denied call: %v", calls)
	}

	if _, err := reg.Invoke(keeperCtx(), keeperToolName, nil); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !slices.Contains(calls, keeperToolName) {
		t.Errorf("keeper call did not reach the invoker: %v", calls)
	}
}

func TestInvokeDeniesOtherChannelTool(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	for _, ctx := range []context.Context{userCtx(), channelCtx(userCtx(), "feishu")} {
		result, err := reg.Invoke(ctx, channelToolName, nil)
		if err != nil {
			t.Fatalf("Invoke() error = %v", err)
		}
		if !isPermissionDenied(result) {
			t.Errorf("result = %v, want permission denied", resultText(result))
		}
	}
	if len(calls) != 0 {
		t.Errorf("invoker executed for a denied call: %v", calls)
	}

	if _, err := reg.Invoke(channelCtx(userCtx(), "wecom"), channelToolName, nil); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !slices.Contains(calls, channelToolName) {
		t.Errorf("own-channel call did not reach the invoker: %v", calls)
	}
}

func TestInvokeUnknownTool(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	result, err := reg.Invoke(userCtx(), "no_such_tool", nil)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !strings.Contains(resultText(result), "tool not found") {
		t.Errorf("result = %v, want tool not found", resultText(result))
	}
}

func TestInvokeCaseVariantKeepsPermission(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	result, err := reg.Invoke(userCtx(), "KB_CREATE", nil)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !isPermissionDenied(result) {
		t.Errorf("result = %v, want permission denied", resultText(result))
	}
	if _, err := reg.Invoke(keeperCtx(), "KB_CREATE", nil); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !slices.Contains(calls, keeperToolName) {
		t.Errorf("case variant did not reach the invoker: %v", calls)
	}
}

func TestVisibilityMatchesExecution(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)

	universe := []string{ToolNameFetch, ToolNameCurrentTime, keeperToolName, channelToolName}
	contexts := map[string]context.Context{
		"user":       userCtx(),
		"keeper":     keeperCtx(),
		"wecom":      channelCtx(userCtx(), "wecom"),
		"feishu":     channelCtx(userCtx(), "feishu"),
		"wecom-user": channelCtx(userCtx(), "wecom"),
	}

	for name, ctx := range contexts {
		t.Run(name, func(t *testing.T) {
			visible := visibleNames(reg, ctx)
			for _, tool := range universe {
				result, err := reg.Invoke(ctx, tool, nil)
				if err != nil {
					t.Fatalf("Invoke(%q) error = %v", tool, err)
				}
				denied := isPermissionDenied(result)
				seen := slices.Contains(visible, tool)
				if seen && denied {
					t.Errorf("tool %q is visible but invocation is denied", tool)
				}
				if !seen && !denied {
					t.Errorf("tool %q is hidden but invocation is allowed", tool)
				}
			}
		})
	}
}

func TestRegisterInvokerIsBuiltinPublic(t *testing.T) {
	var calls []string
	reg := newPolicyRegistry(&calls)
	reg.RegisterInvoker("weather", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return mcps.BuildToolSuccessResult("sunny"), nil
	})

	if src := reg.PolicyFor(userCtx()).Source("weather"); src != SourceBuiltinPublic {
		t.Errorf("source = %q, want %q", src, SourceBuiltinPublic)
	}
	if !slices.Contains(visibleNames(reg, userCtx()), "weather") {
		t.Error("runtime-registered tool should be visible to a normal user")
	}
	result, err := reg.Invoke(userCtx(), "weather", nil)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if isPermissionDenied(result) {
		t.Errorf("result = %v, want allowed", resultText(result))
	}
}

func TestMCPToolCategories(t *testing.T) {
	reg := NewRegistry(nil)
	result := &mcp.ListToolsResult{Tools: []mcp.Tool{{Name: "search"}}}

	global := &mcps.ServerBasic{Name: "srv"}
	if err := reg.registerToolsLocked(&reg.tools, reg.invokers, reg.servers, reg.categories, global, nil, result); err != nil {
		t.Fatalf("registerToolsLocked() error = %v", err)
	}
	if src := reg.categories["srv__search"]; src != SourceMCPGlobal {
		t.Errorf("global MCP source = %q, want %q", src, SourceMCPGlobal)
	}

	cs := &channelToolSet{
		tools:    make([]mcps.ToolDescriptor, 0),
		invokers: make(map[string]Invoker),
		servers:  make(map[string]*MCPConnection),
	}
	reg.channelTools["wecom"] = cs
	chServer := &mcps.ServerBasic{Name: "srv", Channel: "wecom"}
	if err := reg.registerToolsLocked(&cs.tools, cs.invokers, cs.servers, nil, chServer, nil, result); err != nil {
		t.Fatalf("registerToolsLocked() error = %v", err)
	}
	// 频道工具的归属类别由容器派生，无需逐工具登记
	if src := reg.PolicyFor(channelCtx(userCtx(), "wecom")).Source("wecom__srv__search"); src != SourceMCPChannel {
		t.Errorf("channel MCP source = %q, want %q", src, SourceMCPChannel)
	}
	if reg.PolicyFor(channelCtx(userCtx(), "feishu")).Allows("wecom__srv__search") {
		t.Error("channel MCP tool should not be allowed in another channel")
	}
}
