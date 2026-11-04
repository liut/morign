package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/cupogo/andvari/models/oid"
	"github.com/go-chi/render"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/agent"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
)

// ErrTurnConflict 同一幂等键对应了不同的请求内容。
var ErrTurnConflict = errors.New("idempotency key conflict: same key with different request")

// turnGate 是幂等判定后的处置结论。
type turnGate struct {
	// ID 是轮次编号。
	ID string
	// Cached 是命中已完成轮次时的既有答案。
	Cached string
	// Proceed 表示本次请求获得执行权。
	Proceed bool
	// Replay 表示命中已完成轮次，应直接返回既有答案。
	Replay bool
}

// turnStore 是轮次存储的最小接口，便于替换与测试。
type turnStore interface {
	CreateTurn(ctx context.Context, in convo.ChatTurnBasic) (*convo.ChatTurn, error)
	GetTurnByIdempotencyKey(ctx context.Context, key string) (*convo.ChatTurn, error)
	UpdateTurnStatus(ctx context.Context, id, status, result string) error
}

// acquireTurn 申请一次聊天轮次的执行权。
//
// 幂等键冲突时按既有轮次状态分派：succeeded 复用既有结果；
// failed/cancelled 允许同键重跑；queued/running/interrupted 既不执行也不返回结果。
// 轮次记录写入失败时返回错误——拿不到幂等声明就不进入模型与副作用工具（fail-closed）。
func acquireTurn(ctx context.Context, sto turnStore, in convo.ChatTurnBasic, owner oid.OID) (*turnGate, error) {
	turn, err := sto.CreateTurn(ctx, in)
	if err == nil {
		// 独占创建：立刻转 running，之后的重复投递只会看到 running，不再进入执行。
		if uerr := sto.UpdateTurnStatus(ctx, turn.StringID(), stores.TurnStatusRunning, ""); uerr != nil {
			return nil, uerr
		}
		return &turnGate{ID: turn.StringID(), Proceed: true}, nil
	}
	if !errors.Is(err, stores.ErrDuplicate) {
		return nil, err
	}

	existing, gerr := sto.GetTurnByIdempotencyKey(ctx, in.IdempotencyKey)
	if gerr != nil {
		return nil, gerr
	}
	// 同键但内容不同：拒绝复用既有结果，避免用户拿到另一个问题的答案。
	if existing.RequestDigest != "" && in.RequestDigest != "" && existing.RequestDigest != in.RequestDigest {
		return nil, ErrTurnConflict
	}
	// 归属不一致时不返回既有结果。
	if existing.OwnerID != oid.ZeroID && owner != oid.ZeroID && existing.OwnerID != owner {
		return &turnGate{ID: existing.StringID()}, nil
	}

	switch existing.Status {
	case stores.TurnStatusSucceeded:
		return &turnGate{ID: existing.StringID(), Replay: true, Cached: existing.Result}, nil
	case stores.TurnStatusFailed, stores.TurnStatusCancelled:
		if uerr := sto.UpdateTurnStatus(ctx, existing.StringID(), stores.TurnStatusRunning, ""); uerr != nil {
			return nil, uerr
		}
		return &turnGate{ID: existing.StringID(), Proceed: true}, nil
	default:
		return &turnGate{ID: existing.StringID()}, nil
	}
}

// finishTurn 写轮次终态。ctx 被取消且已有工具执行时记为 interrupted（副作用可能已发生），
// 否则取消记为 cancelled，其余失败记为 failed。
// 终态写入用与请求解绑的上下文：请求已取消时也要留下痕迹。
func finishTurn(ctx context.Context, sto turnStore, turnID, answer string, toolRan bool, runErr error) {
	if turnID == "" {
		return
	}
	status, result := stores.TurnStatusSucceeded, answer
	switch {
	case runErr == nil:
	case ctx.Err() != nil:
		status = stores.TurnStatusCancelled
		if toolRan {
			status = stores.TurnStatusInterrupted
		}
	default:
		status = stores.TurnStatusFailed
		result = runErr.Error()
	}
	wctx := context.WithoutCancel(ctx)
	if err := sto.UpdateTurnStatus(wctx, turnID, status, result); err != nil {
		logger().Warn("turn: update status failed", "turn", turnID, "status", status, "err", err)
	}
}

// turnDigest 计算请求内容摘要，用于识别「同一幂等键但内容不同」。
func turnDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// turnKeyForWeb 派生 Web 侧的幂等键：调用者身份 + 会话 + 提问内容 + 请求摘要。
// 身份参与派生，避免两个用户发同一条消息时互相命中对方的轮次。
func turnKeyForWeb(owner oid.OID, sessionID, prompt, digest string) string {
	ownerPart := "anon:"
	if owner != oid.ZeroID {
		ownerPart = owner.String()
	}
	h := sha256.New()
	h.Write([]byte(ownerPart))
	h.Write([]byte(sessionID))
	h.Write([]byte(prompt))
	h.Write([]byte(digest))
	return "web:" + hex.EncodeToString(h.Sum(nil))
}

// turnKeyForChannel 频道侧的幂等键：平台消息编号全局唯一，无需再拼内容。
func turnKeyForChannel(channel, messageID string) string {
	return channel + ":" + messageID
}

// turnOwner 从上下文取登录身份的归属编号，无登录身份时为零值。
func turnOwner(ctx context.Context) oid.OID {
	user, ok := stores.UserFromContext(ctx)
	if !ok {
		return oid.ZeroID
	}
	return oid.Cast(user.OID)
}

// turnNonce 生成一次性后缀，用于「重试/重新生成」这类明确要重新执行的请求：
// 仍然留下轮次记录，但不与既有轮次争同一个幂等键。
func turnNonce() string {
	return oid.NewID(oid.OtEvent).String()
}

// joinParts 把请求的可变部分拼成摘要输入。
func joinParts(parts ...string) string {
	return strings.Join(parts, "\x00")
}

// turnToolKey 工具调用追踪器在上下文中的键。
type turnToolKey struct{}

// turnToolTracker 记录本次请求是否已发生工具调用。
type turnToolTracker struct {
	called atomic.Bool
}

func newTurnToolTracker() *turnToolTracker {
	return &turnToolTracker{}
}

// contextWithTurnTracker 把工具调用追踪器挂到上下文上。
func contextWithTurnTracker(ctx context.Context, tr *turnToolTracker) context.Context {
	return context.WithValue(ctx, turnToolKey{}, tr)
}

// newToolExecutor 构造带工具调用追踪的执行器。
// 追踪器随上下文传递，用来区分「取消前已发生副作用」与「取消前无副作用」。
func newToolExecutor(reg *tools.Registry) *agent.ToolExecutor {
	te := agent.NewToolExecutor(reg)
	te.BeforeToolCall = func(ctx context.Context, name string, params map[string]any) (bool, string) {
		if tr, ok := ctx.Value(turnToolKey{}).(*turnToolTracker); ok {
			tr.called.Store(true)
		}
		return false, ""
	}
	return te
}

// acquireChatTurn 为 Web 请求申请轮次执行权。
// 重试与重新生成表示一次新的提问：仍然留下轮次记录，但不与既有轮次争同一个幂等键。
func (a *api) acquireChatTurn(ctx context.Context, param *ChatRequest, ccr *chatRequest) (*turnGate, error) {
	owner := turnOwner(ctx)
	key, digest := webTurnKey(owner, param, ccr.retry)
	in := convo.ChatTurnBasic{
		SessionID:      ccr.cs.GetOID(),
		IdempotencyKey: key,
		Source:         "web",
		Status:         stores.TurnStatusQueued,
		RequestDigest:  digest,
		OwnerID:        owner,
	}
	return acquireTurn(ctx, a.sto.Convo(), in, owner)
}

// replyCached 返回命中已完成轮次的既有答案，不重复执行模型。
func (a *api) replyCached(w http.ResponseWriter, r *http.Request, ccr *chatRequest, answer string, isSSE bool) {
	if !isSSE {
		var cm ChatMessage
		cm.Text = answer
		render.JSON(w, r, &cm)
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Add("Conversation-ID", ccr.cs.GetID())
	w.(http.Flusher).Flush()

	var cm ChatMessage
	cm.Delta = answer
	_ = writeEvent(w, "1", &cm)

	var doneCm ChatMessage
	doneCm.ConversationID = ccr.cs.GetID()
	_ = writeEvent(w, "2", &doneCm)
	_ = writeEvent(w, "3", esDone)
}

// webTurnKey 计算 Web 请求的幂等键与请求摘要。
// 重试与重新生成表示一次新的提问：键追加一次性后缀，不与既有轮次争同一个幂等键。
func webTurnKey(owner oid.OID, param *ChatRequest, retry bool) (key, digest string) {
	digest = turnDigest(joinParts(
		param.Prompt,
		param.GetConversionID(),
		strings.Join(param.Skills, ","),
		strings.Join(param.MCPs, ","),
	))
	key = turnKeyForWeb(owner, param.GetConversionID(), param.Prompt, digest)
	if retry {
		key += ":" + turnNonce()
	}
	return key, digest
}
