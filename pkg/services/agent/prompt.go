package agent

import (
	"context"
	"strings"
	"time"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
)

const (
	// DefaultSystemMsg is the fallback system prompt when no preset is configured.
	DefaultSystemMsg = "You are a helpful assistant. If you cannot find relevant information in the provided context to answer the user's question, please honestly state that you don't know rather than making up an answer."

	// DefaultToolsMsg is the fallback tool-usage prompt when no preset is configured.
	DefaultToolsMsg = "You will select the appropriate tool based on the user's question and call the tool to solve the problem. If the tool returns no relevant information, honestly state that you don't know rather than making up an answer. If the tool requires parameters, you must extract them from the user's question. Note that it is important to clearly distinguish between read and write operations. If a write operation is required by the tool, it must be explicitly stated in the user's question for writing purposes (such as adding, creating, appending, modifying, etc.), and all necessary parameters for the tool must be included in the user's question before calling; otherwise, treat it as a regular read operation or Q&A."
)

// ThisMoment returns a timestamp string in Chinese 时辰 format.
func ThisMoment() string {
	now := time.Now()
	hour := now.Hour()

	var shichen string
	switch {
	case hour >= 23 || hour < 1:
		shichen = "子时"
	case hour >= 1 && hour < 3:
		shichen = "丑时"
	case hour >= 3 && hour < 5:
		shichen = "寅时"
	case hour >= 5 && hour < 7:
		shichen = "卯时"
	case hour >= 7 && hour < 9:
		shichen = "辰时"
	case hour >= 9 && hour < 11:
		shichen = "巳时"
	case hour >= 11 && hour < 13:
		shichen = "午时"
	case hour >= 13 && hour < 15:
		shichen = "未时"
	case hour >= 15 && hour < 17:
		shichen = "申时"
	case hour >= 17 && hour < 19:
		shichen = "酉时"
	case hour >= 19 && hour < 21:
		shichen = "戌时"
	case hour >= 21 && hour < 23:
		shichen = "亥时"
	}

	return "当前时辰: " + now.Format("2006-01-02") + " " + shichen
}

// ConvertMCPToolsToLLMTools converts MCP tool descriptors to LLM tool definitions.
func ConvertMCPToolsToLLMTools(tools []mcps.ToolDescriptor) []llm.ToolDefinition {
	result := make([]llm.ToolDefinition, 0, len(tools))
	for _, td := range tools {
		result = append(result, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDefinition{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.InputSchema,
			},
		})
	}
	return result
}

// BuildSystemMessage constructs a system message from the tool registry and prompts.
// Returns the system message and the resolved tool definitions.
func BuildSystemMessage(ctx context.Context, toolreg *tools.Registry, sysPrompt, toolsPrompt string) (llm.Message, []llm.ToolDefinition) {
	var sb strings.Builder

	if sysPrompt == "" {
		sysPrompt = DefaultSystemMsg
	}
	sb.WriteString(sysPrompt)

	if settings.Current.DateInContext {
		sb.WriteString("\n")
		sb.WriteString(ThisMoment())
	}

	toolDefs := ConvertMCPToolsToLLMTools(toolreg.ToolsFor(ctx))
	if len(toolDefs) > 0 {
		if toolsPrompt == "" {
			toolsPrompt = DefaultToolsMsg
		}
		sb.WriteString("\n")
		sb.WriteString(toolsPrompt)
	}

	return llm.Message{Role: llm.RoleSystem, Content: sb.String()}, toolDefs
}
