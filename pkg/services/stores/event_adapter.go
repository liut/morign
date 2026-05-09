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
	cs := NewConversation(ctx, sessionID)

	switch event.Author {
	case "user":
		return nil
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
		return cs.Save(ctx)
	}
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
