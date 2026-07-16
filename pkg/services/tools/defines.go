package tools

import (
	"fmt"
	"strings"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/utils/words"
)

const (
	ToolNameKBSearch = "kb_search" // 知识库搜索工具
	ToolNameKBCreate = "kb_create" // 知识库创建工具
	ToolNameFetch    = "fetch"     // 网页抓取工具

	ToolNameMemoryList   = "memory_list"   // 记忆列表工具
	ToolNameMemoryRecall = "memory_recall" // 记忆召回工具
	ToolNameMemoryStore  = "memory_store"  // 记忆存储工具
	ToolNameMemoryForget = "memory_forget" // 记忆删除工具

	ToolNameCapabilityMatch  = "capability_match"  // API 能力匹配工具
	ToolNameCapabilityInvoke = "capability_invoke" // API 能力调用工具

	ToolNameStrataExec = "strata_exec"
)

// ToolDescriptor 变量定义
var (
	// kbSearchDescriptor 知识库搜索工具描述
	kbSearchDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameKBSearch,
		Description: "Search documents in knowledge base with subject. \nWhen faced with unknown or uncertain issues, prioritize consulting the knowledge base.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject": map[string]any{
					"type":        "string",
					"description": "text of keywords or subject",
				},
			},
			"required": []string{"subject"},
		},
	}

	// kbCreateDescriptor 知识库创建工具描述（需要 keeper 角色）
	kbCreateDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameKBCreate,
		Description: "Create new document of knowledge base, all parameters are required. Note: Unless the user explicitly requests supplementary content, do not invoke it. Before invoking, always perform a kb_search to confirm there is no corresponding subject or content. If similar content already exists, do not invoke even if requested by the user!",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "title of document, like a main name or topic",
				},
				"heading": map[string]any{
					"type":        "string",
					"description": "heading of document, like a sub name or property",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "long text of content of document",
				},
			},
			"required": []string{"title", "heading", "content"},
		},
	}

	// fetchDescriptor 网页抓取工具描述
	fetchDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameFetch,
		Description: "Fetches a URL from the internet and optionally extracts its contents as markdown",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "URL to fetch",
				},
				"max_length": map[string]any{
					"type":        "number",
					"description": "Maximum number of characters to return, default 5000",
					"default":     5000,
					"minimum":     0,
					"maximum":     1000000,
				},
				"start_index": map[string]any{
					"type":        "number",
					"description": "On return output starting at this character index, default 0",
					"default":     0,
					"minimum":     0,
				},
				"raw": map[string]any{
					"type":        "boolean",
					"description": "Get the actual HTML content without simplification, default false",
					"default":     false,
				},
			},
			"required": []string{"url"},
		},
	}

	// memoryListDescriptor 记忆列表工具描述
	memoryListDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameMemoryList,
		Description: "List memory entries in recency order. Use for requests like 'show first N memory records' without shell/sqlite access.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max entries to return (default: 5, max: 100)",
					"default":     5,
					"minimum":     1,
					"maximum":     100,
				},
				"category": map[string]any{
					"type":        "string",
					"description": "Optional category filter (core|daily|conversation|custom)",
				},
				"session_id": map[string]any{
					"type":        "string",
					"description": "Optional session filter",
				},
				"include_content": map[string]any{
					"type":        "boolean",
					"description": "Include content preview (default: true)",
					"default":     true,
				},
			},
		},
	}

	// memoryRecallDescriptor 记忆召回工具描述
	memoryRecallDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameMemoryRecall,
		Description: "Search long-term memory for relevant facts, preferences, or context.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Keywords or phrase to search for in memory",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max results to return (default: 5)",
					"default":     5,
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{"query"},
		},
	}

	// memoryStoreDescriptor 记忆存储工具描述
	memoryStoreDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameMemoryStore,
		Description: "Store durable user facts, preferences, and decisions in long-term memory. Use category 'core' for stable facts, 'daily' for session notes, 'conversation' for important context only. Do not store routine greetings or every chat message.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Unique key for this memory",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The information to remember",
				},
				"category": map[string]any{
					"type":        "string",
					"description": "Memory category",
					"enum":        []string{"core", "daily", "conversation"},
				},
			},
			"required": []string{"key", "content"},
		},
	}

	// memoryForgetDescriptor 记忆删除工具描述
	memoryForgetDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameMemoryForget,
		Description: "Remove a memory by key. Use to delete outdated facts or sensitive data.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "The key of the memory to forget",
				},
			},
			"required": []string{"key"},
		},
	}

	// capabilityMatchDescriptor API 能力匹配工具描述
	capabilityMatchDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameCapabilityMatch,
		Description: "Match API capabilities by user intent. Most APIs are RESTful, so each intent should target a single resource for best matching. Returns 3-5 relevant APIs based on semantic similarity.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"intent": map[string]any{
					"type":        "string",
					"description": "User's intent targeting a single resource (e.g., '查询我的订单', '创建文章')",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max results to return (default: 6)",
					"default":     6,
					"minimum":     1,
					"maximum":     10,
				},
			},
			"required": []string{"intent"},
		},
	}

	// capabilityInvokeDescriptor API 能力调用工具描述
	capabilityInvokeDescriptor = mcps.ToolDescriptor{
		Name:        ToolNameCapabilityInvoke,
		Description: "Invoke a specific API capability. Returns the API response from Bus. Use the capability info from capability_match to construct the request. The method, endpoint are provided for LLM to construct the full URI. Note: Database fields use snake_case naming, not camelCase. For example, joiningAt in the model is joining_at in the database. This is especially useful when sorting.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method (GET, POST, PUT, DELETE, etc.)",
				},
				"endpoint": map[string]any{
					"type":        "string",
					"description": "API endpoint path (may contain path variables like /api/accounts/{id})",
				},
				"params": map[string]any{
					"type":        "object",
					"description": "API parameters values to fill in (path variables, query params, body, etc.)",
				},
			},
			"required": []string{"method", "endpoint"},
		},
	}

	// strataExecDescriptor = mcps.ToolDescriptor{
	// 	Name:        ToolNameStrataExec,
	// 	Description: "Execute Shell command",
	// 	InputSchema: map[string]any{
	// 		"type": "object",
	// 		"properties": map[string]any{
	// 			"command":    map[string]string{"type": "string", "description": "Shell 命令"},
	// 			"timeout_ms": map[string]any{"type": "number", "description": "超时毫秒", "default": float64(30000)},
	// 		},
	// 		"required": []string{"command"},
	// 	},
	// }
)

// ResultLogs 是工具调用结果的日志类型别名
type ResultLogs map[string]any

// formatValue 格式化单个值
// string 取前 30 字符，slice 遍历处理，map 递归处理，其他返回 ""
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		maxLen := len(val)
		if maxLen > 50 {
			maxLen = 50
		}
		return words.TakeHead(val, maxLen, "..")
	case []any:
		if len(val) == 0 {
			return ""
		}
		var sb strings.Builder
		sb.WriteString("[")
		for i, item := range val {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(formatValue(item))
		}
		sb.WriteString("]")
		return sb.String()
	case map[string]any:
		if len(val) == 0 {
			return ""
		}
		var sb strings.Builder
		sb.WriteString("{")
		first := true
		for k, v := range val {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			fmt.Fprintf(&sb, "%s: %s", k, formatValue(v))
		}
		sb.WriteString("}")
		return sb.String()
	}
	return ""
}

// String 实现 fmt.Stringer 接口，返回格式化的结果日志
// 格式为 {key: val[0:min(30,len(val)}，如果值不是 string/slice/map，则为 key: ""
func (rl ResultLogs) String() string {
	if rl == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("{")
	first := true
	for k, v := range rl {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&sb, "%s: %s", k, formatValue(v))
	}
	sb.WriteString("}")
	return sb.String()
}
