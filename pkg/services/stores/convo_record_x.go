package stores

import (
	"context"
	"errors"

	"github.com/spf13/cast"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/mcps"
)

// ErrNoTurnScope 上下文缺少轮次或会话锚点，原始记录无处归属。
var ErrNoTurnScope = errors.New("no turn scope")

const (
	// toolResultReadDefaultLimit 单次回读的默认字符数。
	toolResultReadDefaultLimit = 4000
	// toolResultReadMaxLimit 单次回读的最大字符数。
	// 上限远小于工具结果上限，回读的内容不会被再次截断。
	toolResultReadMaxLimit = 8000
)

// toolScope 取出原始记录的身份：轮次、会话、归属。
// 三者一律来自服务端上下文，调用方无法指定。
func toolScope(ctx context.Context) (turnID, sessionID, owner oid.OID, err error) {
	turn, ok := TurnIDFromContext(ctx)
	if !ok {
		return 0, 0, 0, ErrNoTurnScope
	}
	csid := ConvoIDFromContext(ctx)
	if csid == "" {
		return 0, 0, 0, ErrNoTurnScope
	}
	if u, uok := UserFromContext(ctx); uok {
		owner = oid.Cast(u.OID)
	}
	return oid.Cast(turn), oid.Cast(csid), owner, nil
}

// toolRecordBasic 组装工具记录的基本字段。
func toolRecordBasic(ctx context.Context, in convo.ToolRecordSpill) (convo.ToolRecordBasic, error) {
	turnID, sessionID, owner, err := toolScope(ctx)
	if err != nil {
		return convo.ToolRecordBasic{}, err
	}
	return convo.ToolRecordBasic{
		TurnID:     turnID,
		SessionID:  sessionID,
		OwnerID:    owner,
		Seq:        in.Seq,
		CallID:     in.CallID,
		ToolName:   in.ToolName,
		Arguments:  in.Arguments,
		Status:     in.Status,
		Content:    in.Content,
		Preview:    in.Preview,
		Spilled:    in.Spilled,
		ErrorCode:  in.ErrorCode,
		DurationMS: in.DurationMS,
	}, nil
}

// SpillToolResult 写入工具结果原文，返回可回读的 handle。
// 截断处是唯一写入方：handle 指向的就是这一行，回读时拿到的正是全文。
func (s *convoStore) SpillToolResult(ctx context.Context, in convo.ToolRecordSpill) (string, error) {
	basic, err := toolRecordBasic(ctx, in)
	if err != nil {
		return "", err
	}
	obj, err := s.CreateToolRecord(ctx, basic)
	if err != nil {
		return "", err
	}
	return obj.StringID(), nil
}

// UpsertToolRecord 按 (轮次, 调用编号) 落工具记录。
// 已存在时只更新结果状态，不覆盖 Content：Content 由截断处写入，是回读 handle 的依据。
func (s *convoStore) UpsertToolRecord(ctx context.Context, in convo.ToolRecordSpill) error {
	basic, err := toolRecordBasic(ctx, in)
	if err != nil {
		return err
	}
	exist, err := s.findToolRecord(ctx, basic.TurnID, basic.CallID)
	if err == nil {
		status, code, dur := basic.Status, basic.ErrorCode, basic.DurationMS
		return s.UpdateToolRecord(ctx, exist.StringID(), convo.ToolRecordSet{
			Status: &status, ErrorCode: &code, DurationMS: &dur,
		})
	}
	if !errors.Is(err, ErrNoRows) && !errors.Is(err, ErrNotFound) {
		return err
	}
	// 截断处没有建行（结果未超限）：此处补建，内容是模型实际看到的那一份。
	basic.Spilled = false
	basic.Preview = basic.Content
	_, err = s.CreateToolRecord(ctx, basic)
	return err
}

// findToolRecord 按 (轮次, 调用编号) 查找工具记录。
func (s *convoStore) findToolRecord(ctx context.Context, turnID oid.OID, callID string) (*convo.ToolRecord, error) {
	obj := new(convo.ToolRecord)
	err := s.w.db.NewSelect().Model(obj).
		Where("turn_id = ? AND call_id = ?", int64(turnID), callID).
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// ReadToolRecord 按 handle 读取工具结果全文。
// 强制同会话同归属：无法确定归属时一律拒绝。
func (s *convoStore) ReadToolRecord(ctx context.Context, id string, offset, limit int) (string, int, error) {
	_, sessionID, owner, err := toolScope(ctx)
	if err != nil {
		return "", 0, ErrForbidden
	}
	pid := oid.Cast(id)
	if !pid.Valid() {
		return "", 0, ErrForbidden
	}
	obj := new(convo.ToolRecord)
	err = s.w.db.NewSelect().Model(obj).
		Where("id = ? AND session_id = ? AND owner_id = ?", int64(pid), int64(sessionID), int64(owner)).
		Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, ErrNoRows) || errors.Is(err, ErrNotFound) {
			return "", 0, ErrForbidden
		}
		return "", 0, err
	}

	runes := []rune(obj.Content)
	total := len(runes)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = toolResultReadDefaultLimit
	}
	if limit > toolResultReadMaxLimit {
		limit = toolResultReadMaxLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return string(runes[offset:end]), total, nil
}

// InvokerForToolResultRead 返回回读工具结果全文的内置工具执行器。
func (s *convoStore) InvokerForToolResultRead() mcps.Invoker {
	return func(ctx context.Context, params map[string]any) (map[string]any, error) {
		handle, _ := params["handle"].(string)
		if handle == "" {
			return nil, errors.New("handle is required")
		}
		content, total, err := s.ReadToolRecord(ctx, handle, cast.ToInt(params["offset"]), cast.ToInt(params["limit"]))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"content":     []map[string]any{{"text": content}},
			"offset":      cast.ToInt(params["offset"]),
			"length":      len([]rune(content)),
			"totalLength": total,
		}, nil
	}
}
