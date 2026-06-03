package api

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/channels"
	"github.com/liut/morign/pkg/services/channels/feishu"
	"github.com/liut/morign/pkg/services/channels/wecom"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/runner"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
)

// channelHandler holds dependencies for handling channel messages.
type channelHandler struct {
	sto      stores.Storage
	llm      llm.Client
	toolreg  *tools.Registry
	toolExec *ToolExecutor
	rnr      *runner.Runner
}

// InitChannels initializes channel adapters from preset configuration.
func InitChannels(r chi.Router, preset *aigc.Preset, sto stores.Storage, llmClient llm.Client, toolreg *tools.Registry) error {

	// init Channels
	channels.RegisterChannel("feishu", feishu.New)
	channels.RegisterChannel("wecom", wecom.New)

	if preset == nil || len(preset.Channels) == 0 {
		slog.Info("channel: no platforms configured")
		return nil
	}

	chandler := &channelHandler{
		sto:      sto,
		llm:      llmClient,
		toolreg:  toolreg,
		toolExec: NewToolExecutor(toolreg),
		rnr:      runner.New(stores.NewSessionStore(sto), stores.NewHistoryStore(sto)),
	}

	for name, cfg := range preset.Channels {
		if !cfg.Enable {
			slog.Debug("channel: skipping disabled channel", "name", name)
			continue
		}

		// Inject mode into config for channel factory
		channelConfig := cfg.Config
		if channelConfig == nil {
			channelConfig = make(map[string]any)
		}
		channelConfig["mode"] = cfg.Mode

		p, err := channels.NewChannel(name, channelConfig)
		if err != nil {
			slog.Warn("channel: create failed", "name", name, "error", err)
			continue
		}

		if err := p.Start(chandler.MessageHandler); err != nil {
			slog.Warn("channel: start failed", "name", name, "error", err)
			continue
		}

		// Register HTTP routes if channel supports webhook callback
		if httpRouter, ok := p.(channels.HTTPRouter); ok {
			callbackPath, _ := channelConfig["callback_path"].(string)
			if callbackPath == "" {
				callbackPath = "/" + name + "/callback"
			}
			httpRouter.RegisterHTTPRoutes(r, callbackPath, chandler.MessageHandler)
			slog.Info("channel: HTTP routes registered", "name", name, "path", callbackPath)
		}

		// 注册频道专属 MCP Server
		for _, mcpCfg := range cfg.MCPServers {
			sb := mcps.ServerBasic{
				Name:       mcpCfg.Name,
				URL:        mcpCfg.URL,
				TransType:  mcpCfg.TransType,
				HeaderCate: mcpCfg.HeaderCate,
				Channel:    p.Name(),
			}
			if err := chandler.toolreg.AddServer(context.Background(), &sb); err != nil {
				slog.Warn("channel: MCP server init failed", "channel", p.Name(), "server", mcpCfg.Name, "err", err)
			}
		}

		// Use name + mode as unique key to support multiple instances of same channel type
		key := name
		if cfg.Mode != "" {
			key = name + "-" + cfg.Mode
		}
		channels.TrackChannel(key, p, func() {
			chandler.toolreg.RemoveChannelTools(p.Name())
		})
		slog.Info("channel: started", "name", name, "mode", cfg.Mode, "key", key)
	}

	slog.Info("channel: manager initialized")
	return nil
}

// StopChannels stops all channel adapters.
func StopChannels() {
	channels.StopAll()
}

// MessageHandler processes incoming messages from channel adapters.
func (chh *channelHandler) MessageHandler(p channel.Channel, msg *channel.Message) {
	if chh == nil {
		slog.Error("channel: handler not initialized")
		return
	}

	ctx := context.Background()
	user, err := chh.sto.Convo().GetUserWith(ctx, msg.UserID)
	if err == nil {
		logger().Debugw("found user", "id", user.ID, "userID", msg.UserID)
		ctx = ContextWithUser(ctx, user)
		if token, err := stores.LoadTokenWithUser(ctx, user.StringID()); err == nil {
			ctx = stores.OAuthContextWithToken(ctx, token)
		}
	} else {
		logger().Infow("not found user", "userID", msg.UserID, "err", err)
	}

	ctx = mcps.ContextWithChannel(ctx, p.Name())

	// Check for commands at the beginning of content
	if cmd := DetectCommand(msg.Content); cmd.Name != "" {
		handled, err := cmd.Action(ctx, msg)
		if handled {
			replyMsg := "会话已重置，开始新对话"
			if err != nil {
				replyMsg = "指令执行失败，请重试"
			}
			if err := p.Reply(ctx, msg.ReplyCtx, replyMsg); err != nil {
				logger().Warnw("reply after command failed", "err", err)
			}
			return
		}
		if err != nil {
			logger().Warnw("command execution failed", "cmd", cmd.Name, "err", err)
		}
	}

	// Build the chat request
	cs := stores.GetOrCreateConversationBySessionKey(ctx, msg.SessionKey)

	slog.Info("channel: message received",
		"channel", p.Name(),
		"session", msg.SessionKey,
		"conversation", cs.GetID(),
		"user", msg.UserID,
		"content_len", len(msg.Content),
	)

	// Detect streaming support
	if sr, ok := p.(channel.StreamReplier); ok {
		chh.handleStreamingReply(ctx, p, msg, sr, cs)
	} else {
		chh.handleRegularReply(ctx, p, msg, cs)
	}
}

// handleStreamingReply handles reply with streaming support (e.g., WeCom WebSocket).
// It uses a loop similar to chatStreamResponseLoop to handle tool calls.
func (chh *channelHandler) handleStreamingReply(ctx context.Context, p channel.Channel, msg *channel.Message, sr channel.StreamReplier, cs stores.Conversation) {
	// Build messages and get tools
	messages, tools := chh.buildChatMessagesAndTools(ctx, msg, cs)

	// MaxLoopIterations limits tool call chain depth to prevent infinite loops (default: 5)
	maxLoopIterations := settings.Current.MaxLoopIterations
	iter := 0
	var fullAnswer string
	var streamID string

	for {
		iter++
		if iter > maxLoopIterations {
			slog.Info("channel: streaming loop iteration limit reached", "maxIter", maxLoopIterations)
			break
		}

		// First iteration: start stream immediately to notify platform we're processing
		if streamID == "" {
			var err error
			streamID, err = sr.StartStream(ctx, msg.ReplyCtx, "正在思考...")
			if err != nil {
				slog.Error("channel: start stream failed", "err", err)
				channelReplyError(p, msg, "AI processing failed")
				return
			}
		}

		// Do one streaming round (stream lifecycle managed by this function)
		answer, toolCalls, err := chh.doChannelStream(ctx, p, msg, sr, streamID, messages, tools)
		if err != nil {
			slog.Error("channel: stream round failed", "iter", iter, "err", err)
			finishErr := sr.FinishStream(ctx, msg.ReplyCtx, streamID, translateLLMErrorToUser(err))
			if finishErr != nil {
				slog.Warn("channel: finish stream after error failed, falling back to Reply", "err", finishErr)
				channelReplyError(p, msg, "AI processing failed")
			}
			return
		}
		slog.Info("channel: stream round done",
			"iter", iter,
			"answer_len", len(answer),
			"toolCalls_len", len(toolCalls),
			"streamID", streamID)

		// Only update fullAnswer if we got actual content
		if answer != "" {
			fullAnswer = answer
		}

		// No more tool calls, we're done
		if len(toolCalls) == 0 {
			break
		}

		// Add assistant response to messages (with full answer content)
		if fullAnswer != "" {
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: fullAnswer})
		}

		// Execute tool calls and update messages with results
		evs, msgs := chh.toolExec.ExecuteToolCalls(ctx, messages, toolCalls, "")
		messages = msgs
		if len(evs) == 0 {
			// 没有成功执行任何工具，跳出循环
			break
		}
	}

	slog.Info("channel: streaming reply finishing",
		"streamID", streamID,
		"fullAnswer_len", len(fullAnswer))

	// Finish the stream
	if streamID != "" {
		if err := sr.FinishStream(ctx, msg.ReplyCtx, streamID, fullAnswer); err != nil {
			slog.Warn("channel: finish stream failed", "err", err)
		}
	}

	// Save to history
	if fullAnswer != "" {
		if err := chh.rnr.Persist(ctx, cs.GetID(), &llm.Event{
			Author:     "assistant",
			Delta:      fullAnswer,
			UserID:     msg.UserID,
			UserPrompt: msg.Content,
		}); err != nil {
			slog.Warn("channel: persist history failed", "err", err)
		}
	}
}

// doChannelStream performs one streaming chat round, returns answer, tool calls, and streamID.
// The stream lifecycle (Start/Finish) is managed by the caller (handleStreamingReply).
func (chh *channelHandler) doChannelStream(ctx context.Context, p channel.Channel, msg *channel.Message, sr channel.StreamReplier, streamID string, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, error) {
	var contentBuilder strings.Builder
	var currentToolCalls []llm.ToolCall
	chunkCount := 0

	for result, err := range chh.llm.StreamChat(ctx, messages, tools) {
		chunkCount++
		if err != nil {
			slog.Warn("channel: stream error", "err", err)
			break
		}

		// Only send to channel when we have content; accumulate locally for WeCom overwrite semantics
		if result.Delta != "" {
			contentBuilder.WriteString(result.Delta)
			content := contentBuilder.String()
			if err := sr.AppendStream(ctx, msg.ReplyCtx, streamID, content); err != nil {
				slog.Warn("channel: append stream failed", "err", err)
			}
		}

		// Capture tool calls when Done=true (LLM signaled end with tool calls)
		if result.Done {
			currentToolCalls = result.ToolCalls
			slog.Debug("channel: stream Done received", "toolCalls_len", len(result.ToolCalls))
		}
	}

	slog.Info("channel: doChannelStream result",
		"chunkCount", chunkCount,
		"content_len", contentBuilder.Len(),
		"toolCalls_len", len(currentToolCalls),
		"streamID", streamID)

	return contentBuilder.String(), currentToolCalls, nil
}

// handleRegularReply handles reply without streaming (non-WebSocket channels).
func (chh *channelHandler) handleRegularReply(ctx context.Context, p channel.Channel, msg *channel.Message, cs stores.Conversation) {
	messages, tools := chh.buildChatMessagesAndTools(ctx, msg, cs)

	// Execute the chat with tool call loop
	exec := func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, *llm.Usage, error) {
		result, err := chh.llm.Chat(ctx, messages, tools)
		if err != nil {
			return "", nil, nil, err
		}
		return result.Content, result.ToolCalls, result.Usage, nil
	}
	answer, _, _, err := chh.executeToolCallLoop(ctx, messages, tools, exec)
	if err != nil {
		slog.Error("channel: chat execution failed",
			"channel", p.Name(), "error", err)
		channelReplyError(p, msg, "AI processing failed")
		return
	}

	// Save to history (only final answer, not tool call content)
	if len(answer) > 0 {
		if err := chh.rnr.Persist(ctx, cs.GetID(), &llm.Event{
			Author:     "assistant",
			Delta:      answer,
			UserID:     msg.UserID,
			UserPrompt: msg.Content,
		}); err != nil {
			slog.Warn("channel: persist history failed", "err", err)
		}
	}

	// Send reply to channel
	if err := p.Reply(ctx, msg.ReplyCtx, answer); err != nil {
		slog.Error("channel: reply failed",
			"channel", p.Name(), "error", err)
	}
}

// executeToolCallLoop executes tool calls in a loop until no more tool calls
func (chh *channelHandler) executeToolCallLoop(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, exec chatExecutor) (string, []llm.ToolCall, *llm.Usage, error) {
	return chh.toolExec.ExecuteToolCallLoop(ctx, messages, tools, exec)
}

// channelReplyError sends an error message back to the channel.
func channelReplyError(p channel.Channel, msg *channel.Message, errorText string) {
	ctx := context.Background()
	if err := p.Reply(ctx, msg.ReplyCtx, errorText); err != nil {
		slog.Error("channel: send error reply failed",
			"channel", p.Name(), "error", err)
	}
}

// translateLLMErrorToUser converts LLM errors to user-friendly messages.
func translateLLMErrorToUser(err error) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "context deadline exceeded"):
		return "请求超时，请稍后重试"
	case strings.Contains(errStr, "rate limit"):
		return "请求过于频繁，请稍后重试"
	case strings.Contains(errStr, "model not found"):
		return "AI 服务暂时不可用"
	default:
		return "抱歉，发生了错误，请稍后重试"
	}
}

// buildChatMessagesAndTools builds the message list and returns tools for the chat.
func (chh *channelHandler) buildChatMessagesAndTools(ctx context.Context, msg *channel.Message, cs stores.Conversation) ([]llm.Message, []llm.ToolDefinition) {
	sysMsg, tools := prepareSystemMessage(ctx, chh.sto, chh.toolreg, msg.Content, cs)

	content := msg.Content
	if len(msg.Images) > 0 {
		content += "\n[User sent an image]"
	}
	if msg.Audio != nil {
		content += "\n[User sent a voice message]"
	}

	messages := []llm.Message{sysMsg}
	history, _ := cs.ListHistory(ctx)
	for _, hi := range history {
		if hi.ChatItem != nil {
			if hi.ChatItem.User != "" {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: hi.ChatItem.User})
			}
			if hi.ChatItem.Assistant != "" {
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: hi.ChatItem.Assistant})
			}
		}
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: content})

	return messages, tools
}
