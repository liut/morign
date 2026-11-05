package stores

import (
	"context"
	"time"

	oid "github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/runner"
)

// NewHistoryStore 创建 runner.HistoryStore 的适配实现。
func NewHistoryStore(sto Storage) runner.HistoryStore {
	return &historyAdapter{sto: sto}
}

// NewSessionStore 创建 runner.SessionStore 的适配实现。
func NewSessionStore(sto Storage) runner.SessionStore {
	return &sessionAdapter{sto: sto}
}

type historyAdapter struct {
	sto Storage
}

func (a *historyAdapter) AppendEvent(ctx context.Context, sessionID string, event *llm.Event) error {
	// 工具结果不进 Redis 历史：它只进原始记录，模型上下文由原始记录投影生成。
	if event.ToolResult != nil {
		return a.appendToolRecord(ctx, event)
	}

	cs := NewConversation(ctx, sessionID)

	switch event.Author {
	case "user":
		return a.appendMessage(ctx, sessionID, "user", event.Delta)
	default:
		item := &aigc.HistoryItem{
			Time: time.Now().Unix(),
			ChatItem: &aigc.HistoryChatItem{
				Assistant: event.Delta,
			},
		}
		item.ChatItem.Think = event.Think
		if event.UserID != "" {
			item.UID = event.UserID
		}
		if event.UserPrompt != "" {
			item.ChatItem.User = event.UserPrompt
		}
		if err := cs.AddHistory(ctx, item); err != nil {
			return err
		}
		if err := cs.Save(ctx); err != nil {
			return err
		}
		// 原始记录：用户提问与助手回答各一行。
		if err := a.appendMessage(ctx, sessionID, "user", event.UserPrompt); err != nil {
			return err
		}
		return a.appendMessage(ctx, sessionID, "assistant", event.Delta)
	}
}

// appendMessage 写入一条原始消息记录。
// 归属由服务端从登录上下文决定，请求体指定的归属不生效。
func (a *historyAdapter) appendMessage(ctx context.Context, sessionID, role, content string) error {
	if content == "" {
		return nil
	}
	turn, _ := TurnIDFromContext(ctx)
	owner := oid.ZeroID
	if u, ok := UserFromContext(ctx); ok {
		owner = oid.Cast(u.OID)
	}
	_, err := a.sto.Convo().CreateMessage(ctx, convo.MessageBasic{
		SessionID: oid.Cast(sessionID),
		Role:      role,
		Content:   content,
		TurnID:    oid.Cast(turn),
		OwnerID:   owner,
	})
	return err
}

// appendToolRecord 落一条工具调用记录。
// 已存在同一 (轮次, 调用编号) 时只更新结果状态，不覆盖已写入的原文。
func (a *historyAdapter) appendToolRecord(ctx context.Context, event *llm.Event) error {
	tr := event.ToolResult
	return a.sto.Convo().UpsertToolRecord(ctx, convo.ToolRecordSpill{
		Seq:        tr.Seq,
		CallID:     tr.CallID,
		ToolName:   tr.Name,
		Arguments:  tr.Arguments,
		Status:     tr.Status,
		Content:    tr.Content,
		Preview:    tr.Content,
		ErrorCode:  tr.ErrorCode,
		DurationMS: tr.DurationMS,
	})
}

func (a *historyAdapter) CreateUsageRecord(ctx context.Context, sessionID string, event *llm.Event) error {
	basic := convo.UsageRecordBasic{
		SessionID:    oid.Cast(sessionID),
		MsgCount:     event.MsgCount,
		InputTokens:  event.Usage.InputTokens,
		OutputTokens: event.Usage.OutputTokens,
		TotalTokens:  event.Usage.TotalTokens,
		Model:        event.Model,
	}
	if len(event.Meta) > 0 {
		basic.MetaAddKVs(flattenDelta(event.Meta)...)
	}
	_, err := a.sto.Convo().CreateUsageRecord(ctx, basic)
	return err
}

type sessionAdapter struct {
	sto Storage
}

func (a *sessionAdapter) MergeDelta(ctx context.Context, sessionID string, delta map[string]any) error {
	if len(delta) == 0 {
		return nil
	}
	set := convo.SessionSet{}
	set.MetaAddKVs(flattenDelta(delta)...)
	return a.sto.Convo().UpdateSession(ctx, sessionID, set)
}

// flattenDelta 将 map[string]any 展平为 key, value 交替的切片，供 MetaAddKVs 使用。
func flattenDelta(delta map[string]any) []any {
	args := make([]any, 0, len(delta)*2)
	for k, v := range delta {
		args = append(args, k, v)
	}
	return args
}
