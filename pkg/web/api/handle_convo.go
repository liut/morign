package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jpillora/eventsource"
	"github.com/marcsv/go-binder/binder"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/utils/words"
)

func init() {

	regHI(true, "GET", "/config", "", func(a *api) http.HandlerFunc {
		return a.getConfig
	})
	regHI(true, "GET", "/welcome", "", func(a *api) http.HandlerFunc {
		return a.getWelcome
	})
	regHI(true, "GET", "/history/{cid}", "", func(a *api) http.HandlerFunc {
		return a.getHistory
	})
	regHI(true, "GET", "/tools", "", func(a *api) http.HandlerFunc {
		return a.getTools
	})
	regHI(true, "POST", "/summary", "", func(a *api) http.HandlerFunc {
		return a.postSummary
	})
	regHI(true, "PATCH", "/conversation/{csid}/title", "", func(a *api) http.HandlerFunc {
		return a.patchConversationTitle
	})
}

// chatRequest 内部聊天请求结构
type chatRequest struct {
	messages []llm.Message
	tools    []llm.ToolDefinition
	isSSE    bool
	cs       stores.Conversation
	hi       *aigc.HistoryItem
	chunkIdx int // 全局 chunk 计数器，用于 SSE 事件序号
	prompt   string
}

// convertMCPToolsToLLMTools 将 MCP 工具描述转换为 LLM 工具定义
func convertMCPToolsToLLMTools(tools []mcps.ToolDescriptor) []llm.ToolDefinition {
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

// prepareSystemMessage 准备系统消息，包括基础 prompt、记忆、工具或知识库
func prepareSystemMessage(ctx context.Context, sto stores.Storage,
	toolreg *tools.Registry, prompt string, cs stores.Conversation) (
	llm.Message, []llm.ToolDefinition) {
	var sb strings.Builder

	if len(sto.Preset().SystemPrompt) > 0 {
		sb.WriteString(sto.Preset().SystemPrompt)
	} else {
		sb.WriteString(dftSystemMsg)
	}

	if settings.Current.DateInContext {
		sb.WriteString("\n")
		sb.WriteString(thisMoment())
	}

	fmt.Fprintf(&sb, "\nCurrent SessionID: %s\n", cs.GetID())

	if user, ok := stores.UserFromContext(ctx); ok {
		sb.WriteString("\nCurrent User:\n")
		if user.UID == user.Name {
			fmt.Fprintf(&sb, "Name and uid: %s\n", user.UID)
		} else {
			fmt.Fprintf(&sb, "Name: %s\nuid: %s\n", user.Name, user.UID)
		}
		fmt.Fprintf(&sb, "ID: %s\n", user.OID)

		momories, _, err := sto.Convo().ListMemory(ctx, &stores.ConvoMemorySpec{IsOwner: true})
		if err == nil {
			mtext := momories.PrettyTextForOwner()
			logger().Debugw("load memories", "keys", momories.Keys(), "text", words.TakeTail(mtext, 10, ".."))
			sb.WriteString("\n")
			sb.WriteString(mtext)
		} else {
			logger().Infow("ListMemory fail", "err", err)
		}
	}

	// 转换 MCP 工具为 LLM 工具定义
	tools := convertMCPToolsToLLMTools(toolreg.ToolsFor(ctx))
	if len(tools) > 0 {
		toolsPrompt := dftToolsMsg
		if len(sto.Preset().ToolsPrompt) > 0 {
			toolsPrompt = sto.Preset().ToolsPrompt
		}
		sb.WriteString("\n")
		sb.WriteString(toolsPrompt)
		cs.SetTools(llm.Tools(tools).Names()...)
	} else {
		docs, err := sto.Corpus().MatchDocments(ctx, stores.MatchSpec{
			Query: prompt,
			Limit: 5,
		})
		if err == nil {
			logger().Infow("matches", "docs", len(docs), "prompt", prompt)
			content := docs.MarkdownText()
			if len(docs) == 0 {
				content += "\nPlease honestly state that you don't know rather than making up an answer."
			}
			sb.WriteString("\n")
			sb.WriteString(content)
		} else {
			logger().Warnw("match fail", "err", err)
			sb.WriteString("\n知识库查询服务暂时不可用，请稍后再试或联系管理员。")
		}
	}

	if len(cs.GetChannel()) > 0 && len(sto.Preset().ChannelPrompt) > 0 {
		sb.WriteString("\n")
		sb.WriteString(sto.Preset().ChannelPrompt)
	}

	msg := llm.Message{
		Role:    llm.RoleSystem,
		Content: sb.String(),
	}

	return msg, tools
}

func (a *api) prepareChatRequest(ctx context.Context, param *ChatRequest) *chatRequest {
	cs := stores.NewConversation(ctx, param.GetConversionID())

	sysMsg, tools := prepareSystemMessage(ctx, a.sto, a.toolreg, param.Prompt, cs)
	messages := []llm.Message{sysMsg}

	data, err := cs.ListHistory(ctx)
	if err == nil && len(data) > 0 {
		logger().Infow("found history", "size", len(data), "hist", aigc.HiLogged(data))
		data = data.RecentlyWithTokens(historyLimitToken)

		for i, hi := range data {
			if hi.ChatItem != nil {
				isLast := i == len(data)-1
				isRetry := hi.ChatItem.User == param.Prompt

				// 最后一条特殊处理：如果是重试或 Regenerate，完全跳过这一条历史
				if isLast && (isRetry || param.Regenerate) {
					logger().Debugw("skip last history", "retry", isRetry, "regenerate", param.Regenerate)
					break
				}

				// 添加 User
				if len(hi.ChatItem.User) > 0 {
					messages = append(messages, llm.Message{
						Role: llm.RoleUser, Content: hi.ChatItem.User})
				}

				// 添加 Assistant
				if len(hi.ChatItem.Assistant) > 0 {
					messages = append(messages, llm.Message{
						Role: llm.RoleAssistant, Content: hi.ChatItem.Assistant})
				}
			}
		}
	}

	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: param.Prompt,
	})

	return &chatRequest{
		messages: messages,
		tools:    tools,
		cs:       cs,
		hi: &aigc.HistoryItem{
			Time: time.Now().Unix(),
			ChatItem: &aigc.HistoryChatItem{
				User: param.Prompt,
			},
		},
		prompt: param.Prompt,
	}
}

// @Tags 聊天
// @Summary 发送聊天消息
// @Accept json
// @Produce json
// @Param token header string false "登录票据凭证"
// @Param chatRequest body ChatRequest true "聊天请求"
// @Success 200 {object} Done{result=ChatMessage}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 500 {object} Failure "服务端错误"
// @Router /api/chat [post]
func (a *api) postChat(w http.ResponseWriter, r *http.Request) {
	var param ChatRequest
	if err := binder.BindBody(r, &param); err != nil {
		apiFail(w, r, 400, err)
		return
	}
	isSSE := param.Stream || strings.HasSuffix(r.URL.Path, "-sse")
	isStream := param.Stream || isSSE
	ccr := a.prepareChatRequest(r.Context(), &param)

	ccr.isSSE = isSSE

	logger().Infow("chat", "csid", param.GetConversionID(), "msgs", len(ccr.messages), "prompt", param.Prompt, "ip", r.RemoteAddr)

	if isStream {
		res := a.chatStreamResponseLoop(ccr, w, r)
		logger().Infow("stream response", "answer_len", len(res.answer), "toolCalls_len", len(res.toolCalls))
		if len(res.answer) > 0 {

			// TODO: migrate to convo.Message
			if settings.Current.QAChatLog {
				in := corpus.ChatLogBasic{
					ChatID:   ccr.cs.GetOID(),
					Question: param.Prompt,
					Answer:   res.answer,
				}
				ip, _, _ := strings.Cut(r.RemoteAddr, ":")
				in.MetaAddKVs("ip", ip)
				if res.usage != nil {
					in.MetaAddKVs("usage", res.usage)
				}
				_, err := stores.Sgt().Corpus().CreateChatLog(r.Context(), in)
				if err != nil {
					logger().Infow("save chat log fail", "err", err)
				}
			}
		}

		return
	}

	// 非流式场景：使用循环执行工具调用
	exec := func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, *llm.Usage, error) {
		result, err := a.llm.Chat(ctx, messages, tools)
		if err != nil {
			return "", nil, nil, err
		}
		return result.Content, result.ToolCalls, result.Usage, nil
	}
	answer, _, _, err := a.executeToolCallLoop(r.Context(), ccr.messages, ccr.tools, exec)
	if err != nil {
		apiFail(w, r, 500, err)
		return
	}
	logger().Infow("chat", "answer", answer)

	var cm ChatMessage
	cm.Text = answer
	render.JSON(w, r, &cm)
}

// writeEvent write and auto flush
func writeEvent(w io.Writer, id string, m any) bool {
	var b []byte
	var err error
	if s, ok := m.(string); ok {
		b = []byte(s)
	} else {
		b, err = json.Marshal(m)
		if err != nil {
			logger().Infow("json marshal fail", "m", m, "err", err)
			return false
		}
	}

	if err = eventsource.WriteEvent(w, eventsource.Event{
		ID:   id,
		Data: b,
	}); err != nil {
		logger().Infow("eventsource write fail", "err", err)
		return false
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

// chatStreamResponseLoop 循环处理流式响应，支持工具调用循环
func (a *api) chatStreamResponseLoop(ccr *chatRequest, w http.ResponseWriter, r *http.Request) (res chatResponse) {
	// 预先设置 HTTP 头信息（只设置一次）
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return chatResponse{}
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Add("Conversation-ID", ccr.cs.GetID())

	w.(http.Flusher).Flush()

	var iter int
	maxLoopIterations := settings.Current.MaxLoopIterations
	if maxLoopIterations <= 0 {
		maxLoopIterations = 5
	}

	cctx := stores.ContextWithConvoID(r.Context(), ccr.cs.GetID())

	for {
		iter++
		// 达到迭代次数限制，跳出循环
		if iter > maxLoopIterations {
			logger().Infow("chat loop iteration limit reached", "maxIter", maxLoopIterations)
			break
		}

		// 调用流式响应处理
		streamRes := a.doChatStream(ccr, w, r)
		logger().Infow("stream round done", "iter", iter, "maxIter", maxLoopIterations,
			"answer_len", len(streamRes.answer), "toolCalls_len", len(streamRes.toolCalls))

		// 累积答案
		res.answer += streamRes.answer
		if streamRes.usage != nil {
			res.usage = streamRes.usage
		}

		// 如果没有工具调用，跳出循环
		if len(streamRes.toolCalls) == 0 {
			res.finish = streamRes.finish
			break
		}
		logger().Infow("before execute tool calls", "tools", len(streamRes.toolCalls), "msgs", len(ccr.messages),
			"think_len", len(streamRes.think))

		// 执行工具调用，传入 reasoning_content 以便回传
		evs, msgs := a.toolExec.ExecuteToolCalls(cctx, ccr.messages, streamRes.toolCalls, streamRes.think)
		ccr.messages = msgs
		logger().Infow("executed tool calls", "executed", len(evs), "msgs", len(ccr.messages))
		if len(evs) == 0 {
			// 没有成功执行任何工具，跳出循环
			res.finish = streamRes.finish
			break
		}

	}

	if len(res.answer) > 0 {
		if err := a.rnr.Persist(r.Context(), ccr.cs.GetID(), &llm.Event{
			Author:     "assistant",
			Delta:      res.answer,
			Think:      res.think,
			UserPrompt: ccr.prompt,
		}); err != nil {
			logger().Infow("persist fail", "err", err)
		}
	}

	// 请求完成处理：生成标题（限时同步执行）
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	var cm ChatMessage
	ccr.chunkIdx++
	cm.ConversationID = ccr.cs.GetID()
	cm.FinishReason = string(res.finish)

	history, err := ccr.cs.ListHistory(ctx)
	if err == nil && len(history) > 0 {
		title, err := stores.GetHistorySummary(ctx, history)
		if err == nil {
			cm.Title = title
		}
	}

	_ = writeEvent(w, strconv.Itoa(ccr.chunkIdx), &cm)

	// 发送完成事件（最后）
	ccr.chunkIdx++
	_ = writeEvent(w, strconv.Itoa(ccr.chunkIdx), esDone)

	return res
}

// doChatStream 执行一次流式调用，返回累积的 answer 和 toolCalls
func (a *api) doChatStream(ccr *chatRequest, w http.ResponseWriter, r *http.Request) chatResponse {
	var res chatResponse
	var lastWriteEmpty bool // 标记上一次是否写入了空消息

	for result, err := range a.llm.StreamChat(r.Context(), ccr.messages, ccr.tools) {
		if err != nil {
			logger().Infow("stream error", "err", err)
			break
		}

		var cm ChatMessage

		cm.Delta = result.Delta
		cm.Think = result.Think
		res.answer += result.Delta
		res.think += result.Think
		res.usage = result.Usage
		if len(result.ToolCalls) > 0 && result.StopReason == llm.FinishReasonToolCalls {
			cm.ToolCalls = convertToolCallsForJSON(result.ToolCalls)
			ccr.chunkIdx++
			cm.ConversationID = ccr.cs.GetID()
			cm.FinishReason = string(result.StopReason)
			_ = writeEvent(w, strconv.Itoa(ccr.chunkIdx), &cm)
		}

		if len(result.Model) > 0 {
			res.model = result.Model
		}
		if len(result.ResponseID) > 0 {
			res.llmResID = result.ResponseID
		}

		if result.Done {
			logger().Infow("result done", "finish", result.StopReason)
			res.finish = result.StopReason
			res.toolCalls = result.ToolCalls
			break
		}

		// 判断当前是否为空消息
		isEmpty := result.Delta == "" && result.Think == "" && len(cm.ToolCalls) == 0
		if !isEmpty || !lastWriteEmpty {
				// 有内容，或者上一次不是空的，则输出
			ccr.chunkIdx++
			if wrote := writeEvent(w, strconv.Itoa(ccr.chunkIdx), &cm); !wrote {
				break
			}
		}
			// 如果当前是空的且上一次也是空的，跳过（连续空消息只保留第一个）
		lastWriteEmpty = isEmpty
	}
	if res.usage != nil {
		meta := map[string]any{
			"prompt":     ccr.prompt,
			"answerHead": words.TakeHead(res.answer, 12, ".."),
			"answerTail": words.TakeTail(res.answer, 15, ".."),
		}
		if len(res.llmResID) > 0 {
			meta["resposeID"] = res.llmResID
		}
		if err := a.rnr.Persist(r.Context(), ccr.cs.GetID(), &llm.Event{
			Author:   "assistant",
			Usage:    res.usage,
			Model:    res.model,
			MsgCount: len(ccr.messages),
			Meta:     meta,
		}); err != nil {
			logger().Infow("persist usage fail", "err", err)
		}
	}
	logger().Infow("chat stream done", "finish", res.finish, "answer", len(res.answer),
		"ahead", cutTxt(res.answer, 20))
	return res
}

// @Tags 聊天
// @Summary 获取配置信息
// @Accept json
// @Produce json
// @Success 200 {object} Done{result=string}
// @Router /api/config [get]
func (a *api) getConfig(w http.ResponseWriter, r *http.Request) {
	apiOk(w, r, M{
		"apiModel": settings.Current.Interact.Model,
	})
}

// @Tags 聊天
// @Summary 获取欢迎信息
// @Accept json
// @Produce json
// @Success 200 {object} Done{result=string}
// @Router /api/welcome [get]
func (a *api) getWelcome(w http.ResponseWriter, r *http.Request) {
	content := a.preset.Welcome
	if content == "" {
		content = welcomeText
	}

	cs := stores.NewConversation(r.Context(), "")
	apiOk(w, r, M{"id": cs.GetID(), "content": content})
}

// SummaryRequest 摘要请求
type SummaryRequest struct {
	Tips string `json:"tips,omitempty"`
	Text string `json:"text"`
}

// @Summary 生成聊天记录摘要
// @Description 根据聊天记录生成简短标题
// @Accept json
// @Produce json
// @Param request body SummaryRequest true "请求参数"
// @Success 200 {object} resp.Done
// @Router /api/summary [post]
func (a *api) postSummary(w http.ResponseWriter, r *http.Request) {
	var req SummaryRequest
	if err := binder.BindBody(r, &req); err != nil {
		fail(w, r, 400, "invalid request body")
		return
	}
	if req.Text == "" {
		fail(w, r, 400, "text is required")
		return
	}

	summary, err := stores.GetSummary(r.Context(), req.Text, req.Tips)
	if err != nil {
		fail(w, r, 500, err)
		return
	}

	apiOk(w, r, summary)
}

// @Tags 聊天
// @Summary 生成会话标题
// @Accept json
// @Produce json
// @Param token header string false "登录票据凭证"
// @Param csid path string true "会话ID"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求错误"
// @Failure 500 {object} Failure "服务端错误"
// @Router /api/conversation/{csid}/title [patch]
func (a *api) patchConversationTitle(w http.ResponseWriter, r *http.Request) {
	csid := chi.URLParam(r, "csid")
	if csid == "" {
		fail(w, r, 400, "csid is required")
		return
	}

	// 获取会话历史
	cs := stores.NewConversation(r.Context(), csid)
	history, err := cs.ListHistory(r.Context())
	if err != nil {
		fail(w, r, 500, err)
		return
	}

	if len(history) == 0 {
		fail(w, r, 400, "no history found")
		return
	}

	// 调用 GetHistorySummary 生成标题
	summary, err := stores.GetHistorySummary(r.Context(), history)
	if err != nil {
		fail(w, r, 500, err)
		return
	}

	// 更新会话标题
	title := summary
	err = a.sto.Convo().UpdateSession(r.Context(), csid, convo.SessionSet{Title: &title})
	if err != nil {
		fail(w, r, 500, err)
		return
	}

	apiOk(w, r, M{"title": summary})
}

// @Tags 聊天
// @Summary 获取会话历史
// @Accept json
// @Produce json
// @Param token header string false "登录票据凭证"
// @Param cid path string true "会话ID"
// @Success 200 {object} Done{result=aigc.HistoryItem}
// @Failure 500 {object} Failure "服务端错误"
// @Router /api/history/{cid} [get]
func (a *api) getHistory(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	cs := stores.NewConversation(r.Context(), cid)
	data, err := cs.ListHistory(r.Context())
	if err != nil {
		apiFail(w, r, 500, err)
		return
	}
	apiOk(w, r, data, 0)
}

// @Tags 聊天
// @Summary 获取可用工具列表
// @Accept json
// @Produce json
// @Param token header string false "登录票据凭证"
// @Success 200 {object} Done{result=[]mcps.ToolDescriptor}
// @Router /api/tools [get]
func (a *api) getTools(w http.ResponseWriter, r *http.Request) {
	apiOk(w, r, a.toolreg.ToolsFor(r.Context()), 0)
}

// formatToolResult 将工具结果转换为文本字符串
// 优先提取 content 数组中的 text，否则使用 structuredContent
func formatToolResult(result map[string]any) string {
	if result == nil {
		return ""
	}
	// logger().Debugw("formatToolResult", "result", result)
	// 优先提取 content 数组中的 text
	if content, ok := result["content"].([]any); ok {
		for _, c := range content {
			if cMap, ok := c.(map[string]any); ok {
				if text, ok := cMap["text"].(string); ok && text != "" {
					return text
				}
			}
		}
	}
	// 备选：使用 structuredContent
	if sc, ok := result["structuredContent"].(string); ok {
		return sc
	}
	if sc, ok := result["structuredContent"].(map[string]any); ok {
		for k, v := range sc {
			if s, ok := v.(string); ok && k == "text" {
				return s
			}
		}
		if b, err := json.Marshal(sc); err == nil {
			return string(b)
		}
	}
	// 最后：序列化为 JSON
	if b, err := json.Marshal(result); err == nil {
		return string(b)
	}
	return ""
}

// convertToolCallsForJSON 将 llm.ToolCall 转换为可序列化的 map 格式
func convertToolCallsForJSON(tcs []llm.ToolCall) []map[string]any {
	if len(tcs) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tcs))
	for i, tc := range tcs {
		args := string(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		result[i] = map[string]any{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]any{
				"name":      tc.Function.Name,
				"arguments": args,
			},
		}
	}
	return result
}

// executeToolCallLoop 执行工具调用循环，直到没有 tool calls
// - messages: 初始消息列表，会被修改
// - tools: 工具定义
// - exec: 执行聊天的函数（流式或非流式）
// 返回最终的 answer、最后的 toolCalls（如果有）、usage
func (a *api) executeToolCallLoop(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, exec chatExecutor) (string, []llm.ToolCall, *llm.Usage, error) {
	return a.toolExec.ExecuteToolCallLoop(ctx, messages, tools, exec)
}
