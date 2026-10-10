package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cupogo/querybind"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jpillora/eventsource"
	"github.com/marcsv/go-binder/binder"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/agent"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/stores"
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
	regHI(true, "GET", "/memories", "", func(a *api) http.HandlerFunc {
		return a.getConvoMemories
	})
	regHI(true, "GET", "/memories/{id}", "", func(a *api) http.HandlerFunc {
		return a.getConvoMemory
	})
	regHI(true, "POST", "/memories", "", func(a *api) http.HandlerFunc {
		return a.postConvoMemory
	})
	regHI(true, "PUT", "/memories/{id}", "", func(a *api) http.HandlerFunc {
		return a.putConvoMemory
	})
	regHI(true, "DELETE", "/memories/{id}", "", func(a *api) http.HandlerFunc {
		return a.deleteConvoMemory
	})
}

// chatRequest 内部聊天请求结构
type chatRequest struct {
	messages []llm.Message
	msgCount int // conversation messages, excluding an injected activation message
	tools    []llm.ToolDefinition
	isSSE    bool
	cs       stores.Conversation
	prompt   string
	skills   []string
}

func (a *api) prepareChatRequest(ctx context.Context, param *ChatRequest) *chatRequest {
	cs := stores.NewConversation(ctx, param.GetConversionID())

	sysMsg, tools := prepareSystemMessage(ctx, storagePromptStore{a.sto}, a.toolreg, param.Skills, cs)

	history, err := cs.ListHistory(ctx)
	if err != nil || len(history) == 0 {
		history = nil
	} else {
		logger().Info("found history", "size", len(history), "hist", aigc.HiLogged(history))
		history = history.RecentlyWithTokens(historyLimitToken)
	}

	// 最后一条特殊处理：如果是重试或 Regenerate，完全跳过这一条历史
	var skipLast bool
	if len(history) > 0 {
		if last := history[len(history)-1]; last.ChatItem != nil {
			isRetry := last.ChatItem.User == param.Prompt
			if isRetry || param.Regenerate {
				logger().Debug("skip last history", "retry", isRetry, "regenerate", param.Regenerate)
				skipLast = true
			}
		}
	}

	messages := chatMessages(sysMsg, nil, history, param.Prompt, skipLast)

	return &chatRequest{
		messages: messages,
		msgCount: conversationMsgCount(messages, nil),
		tools:    tools,
		cs:       cs,
		prompt:   param.Prompt,
		skills:   param.Skills,
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

	loop := agent.NewAgentLoop(agent.AgentLoopConfig{
		LLM:      a.llm,
		ToolExec: a.toolExec,
		MaxLoop:  settings.Current.MaxLoopIterations,
	})

	logger().Info("chat", "csid", param.GetConversionID(), "msgs", len(ccr.messages), "prompt", param.Prompt, "ip", r.RemoteAddr)

	if isStream {
		a.handleSSEStream(w, r, loop, ccr)
		return
	}

	answer, err := loop.RunNonStreaming(r.Context(), ccr.messages, ccr.tools)
	if err != nil {
		apiFail(w, r, 500, err)
		return
	}
	logger().Info("chat", "answer", answer)

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
			logger().Info("json marshal fail", "m", m, "err", err)
			return false
		}
	}

	if err = eventsource.WriteEvent(w, eventsource.Event{
		ID:   id,
		Data: b,
	}); err != nil {
		logger().Info("eventsource write fail", "err", err)
		return false
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

// handleSSEStream runs the agent loop and writes events to the SSE response.
func (a *api) handleSSEStream(w http.ResponseWriter, r *http.Request, loop *agent.AgentLoop, ccr *chatRequest) {
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Add("Conversation-ID", ccr.cs.GetID())
	w.(http.Flusher).Flush()

	cctx := stores.ContextWithConvoID(r.Context(), ccr.cs.GetID())

	var (
		answer         string
		think          string
		usage          *llm.Usage
		finish         llm.FinishReason
		chunkIdx       int
		lastWriteEmpty bool
	)

	for event, err := range loop.Run(cctx, ccr.messages, ccr.tools) {
		if err != nil {
			logger().Info("agent loop error", "err", err)
			break
		}

		if event.ToolResult != nil {
			continue
		}

		var cm ChatMessage
		cm.Delta = event.Delta
		cm.Think = event.Think

		answer += event.Delta
		think += event.Think

		if event.Usage != nil {
			usage = event.Usage
		}

		if event.Done {
			finish = event.StopReason

			if len(event.ToolCalls) > 0 && event.StopReason == llm.FinishReasonToolCalls {
				cm.ToolCalls = convertToolCallsForJSON(event.ToolCalls)
				chunkIdx++
				cm.ConversationID = ccr.cs.GetID()
				cm.FinishReason = string(event.StopReason)
				_ = writeEvent(w, strconv.Itoa(chunkIdx), &cm)
			}

			if event.Usage != nil {
				meta := map[string]any{
					"prompt":     ccr.prompt,
					"answerHead": words.TakeHead(answer, 12, ".."),
					"answerTail": words.TakeTail(answer, 15, ".."),
				}
				if len(event.ResponseID) > 0 {
					meta["resposeID"] = event.ResponseID
				}
				if err := a.rnr.Persist(r.Context(), ccr.cs.GetID(), &llm.Event{
					Author:   "assistant",
					Usage:    event.Usage,
					Model:    event.Model,
					MsgCount: ccr.msgCount,
					Meta:     meta,
				}); err != nil {
					logger().Info("persist usage fail", "err", err)
				}
			}

			lastWriteEmpty = false
			continue
		}

		isEmpty := event.Delta == "" && event.Think == "" && len(cm.ToolCalls) == 0
		if !isEmpty || !lastWriteEmpty {
			chunkIdx++
			if !writeEvent(w, strconv.Itoa(chunkIdx), &cm) {
				break
			}
		}
		lastWriteEmpty = isEmpty
	}

	logger().Info("stream response", "answer_len", len(answer), "finish", finish)

	if len(answer) > 0 {
		if err := a.rnr.Persist(r.Context(), ccr.cs.GetID(), &llm.Event{
			Author:     "assistant",
			Delta:      answer,
			Think:      think,
			UserPrompt: ccr.prompt,
		}); err != nil {
			logger().Info("persist fail", "err", err)
		}
	}

	if settings.Current.QAChatLog && len(answer) > 0 {
		in := corpus.ChatLogBasic{
			ChatID:   ccr.cs.GetOID(),
			Question: ccr.prompt,
			Answer:   answer,
		}
		ip, _, _ := strings.Cut(r.RemoteAddr, ":")
		in.MetaAddKVs("ip", ip)
		if usage != nil {
			in.MetaAddKVs("usage", usage)
		}
		_, err := stores.Sgt().Corpus().CreateChatLog(r.Context(), in)
		if err != nil {
			logger().Info("save chat log fail", "err", err)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	var doneCm ChatMessage
	chunkIdx++
	doneCm.ConversationID = ccr.cs.GetID()
	doneCm.FinishReason = string(finish)

	history, err := ccr.cs.ListHistory(ctx)
	if err == nil && len(history) > 0 {
		title, err := stores.GetHistorySummary(ctx, history)
		if err == nil {
			doneCm.Title = title
		}
	}

	_ = writeEvent(w, strconv.Itoa(chunkIdx), &doneCm)

	chunkIdx++
	_ = writeEvent(w, strconv.Itoa(chunkIdx), esDone)
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

// failStoreErr 把存储层的拒绝映射为 403、把未找到映射为 404，其余按 503 处理。
func failStoreErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, stores.ErrForbidden):
		fail(w, r, 403, err)
	case errors.Is(err, stores.ErrNotFound):
		fail(w, r, 404, err)
	default:
		fail(w, r, 503, err)
	}
}

// @Tags Memory
// @ID convo-memories-get
// @Summary 查询 记忆 列表 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.ConvoMemorySpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=convo.Memories}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/memories [get]
func (a *api) getConvoMemories(w http.ResponseWriter, r *http.Request) {
	var spec stores.ConvoMemorySpec
	if err := querybind.Bind(&spec, r.URL.Query()); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Convo().ListMemory(ctx, &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, dtResult(data, total))
}

// @Tags Memory
// @ID convo-memories-id-get
// @Summary 获取 记忆 详情 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=convo.Memory}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/memories/{id} [get]
func (a *api) getConvoMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *convo.Memory
	var err error
	obj, err = a.sto.Convo().GetMemory(r.Context(), id)
	if err != nil {
		failStoreErr(w, r, err)
		return
	}

	success(w, r, obj)
}

// @Tags Memory
// @ID convo-memories-post
// @Summary 录入 记忆 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  body   convo.MemoryBasic  true   "Object"
// @Success 200 {object} Done{result=ResultID}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/memories [post]
func (a *api) postConvoMemory(w http.ResponseWriter, r *http.Request) {
	var in convo.MemoryBasic
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	obj, err := a.sto.Convo().CreateMemory(r.Context(), in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, idResult(obj.ID))
}

// @Tags Memory
// @ID convo-memories-id-put
// @Summary 更新 记忆 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Param   query  body   convo.MemorySet  true   "Object"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/memories/{id} [put]
func (a *api) putConvoMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in convo.MemorySet
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	err := a.sto.Convo().UpdateMemory(r.Context(), id, in)
	if err != nil {
		failStoreErr(w, r, err)
		return
	}

	success(w, r, "ok")
}

// @Tags Memory
// @ID convo-memories-id-delete
// @Summary 删除 记忆 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/memories/{id} [delete]
func (a *api) deleteConvoMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Convo().DeleteMemory(r.Context(), id)
	if err != nil {
		failStoreErr(w, r, err)
		return
	}

	success(w, r, "ok")
}
