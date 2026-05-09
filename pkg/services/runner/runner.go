package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/liut/morign/pkg/services/llm"
)

// SessionStore 会话级状态存储接口。
type SessionStore interface {
	MergeDelta(ctx context.Context, sessionID string, delta map[string]any) error
}

// HistoryStore 对话历史持久化接口。
type HistoryStore interface {
	AppendEvent(ctx context.Context, sessionID string, event *llm.Event) error
	CreateUsageRecord(ctx context.Context, sessionID string, event *llm.Event) error
}

// Runner 统一持久化入口：历史追加、状态合并、用量记录。
type Runner struct {
	sessionStore SessionStore
	historyStore HistoryStore
}

// New 创建 Runner。
func New(sessionStore SessionStore, historyStore HistoryStore) *Runner {
	return &Runner{
		sessionStore: sessionStore,
		historyStore: historyStore,
	}
}

// Persist 持久化一个事件：追加历史、合并 StateDelta、记录用量。
func (r *Runner) Persist(ctx context.Context, sessionID string, event *llm.Event) error {
	if err := r.historyStore.AppendEvent(ctx, sessionID, event); err != nil {
		return err
	}
	var errs []error
	if len(event.Actions.StateDelta) > 0 && r.sessionStore != nil {
		if err := r.sessionStore.MergeDelta(ctx, sessionID, event.Actions.StateDelta); err != nil {
			errs = append(errs, fmt.Errorf("merge delta: %w", err))
		}
	}
	if event.Usage != nil {
		if err := r.historyStore.CreateUsageRecord(ctx, sessionID, event); err != nil {
			errs = append(errs, fmt.Errorf("create usage: %w", err))
		}
	}
	return errors.Join(errs...)
}
