//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/convo"
)

func testTurnFor(t *testing.T, ctx context.Context, suffix string) *convo.ChatTurn {
	t.Helper()
	turn, err := Sgt().Convo().CreateTurn(ctx, convo.ChatTurnBasic{
		IdempotencyKey: fmt.Sprintf("test-tool-record-%s-%d", suffix, os.Getpid()),
		Source:         "test",
		Status:         convo.TurnStatusQueued,
		RequestDigest:  "digest",
	})
	if err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}
	return turn
}

func TestIntegration_ToolRecordSpillAndRead(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	owner := newAccountID()
	asOwner := withAccount(ctx, owner)

	turn := testTurnFor(t, ctx, "spill")
	sessionID := oid.NewID(oid.OtEvent).String()
	tctx := withTurnScope(asOwner, turn.StringID(), sessionID)

	handle, err := sto.Convo().SpillToolResult(tctx, convo.ToolRecordSpill{
		Seq:       0,
		CallID:    "c1",
		ToolName:  "big",
		Arguments: `{"q":1}`,
		Status:    "ok",
		Content:   "结果全文",
		Preview:   "预览",
		Spilled:   true,
	})
	if err != nil {
		t.Fatalf("SpillToolResult() error = %v", err)
	}
	if handle == "" {
		t.Fatal("SpillToolResult() returned an empty handle")
	}

	// 事件路径按 (轮次, 调用编号) 落同一行：只更新状态，不覆盖原文。
	if err := sto.Convo().UpsertToolRecord(tctx, convo.ToolRecordSpill{
		Seq:        0,
		CallID:     "c1",
		ToolName:   "big",
		Status:     "error",
		Content:    "预览",
		Preview:    "预览",
		ErrorCode:  "boom",
		DurationMS: 42,
	}); err != nil {
		t.Fatalf("UpsertToolRecord() error = %v", err)
	}

	rows, _, err := sto.Convo().ListToolRecord(tctx, &ConvoToolRecordSpec{
		TurnID: turn.StringID(),
		CallID: "c1",
	})
	if err != nil {
		t.Fatalf("ListToolRecord() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("tool records = %d, want 1", len(rows))
	}
	rec := rows[0]
	if rec.Content != "结果全文" {
		t.Errorf("content = %q, want 结果全文", rec.Content)
	}
	if rec.Status != "error" || rec.ErrorCode != "boom" {
		t.Errorf("status = %q/%q, want error/boom", rec.Status, rec.ErrorCode)
	}
	if rec.DurationMS != 42 {
		t.Errorf("duration = %d, want 42", rec.DurationMS)
	}
	if !rec.Spilled {
		t.Error("spilled = false, want true")
	}

	content, total, err := sto.Convo().ReadToolRecord(tctx, handle, 0, 100)
	if err != nil {
		t.Fatalf("ReadToolRecord() error = %v", err)
	}
	if content != "结果全文" {
		t.Errorf("read = %q, want 结果全文", content)
	}
	if total != len([]rune("结果全文")) {
		t.Errorf("total = %d, want %d", total, len([]rune("结果全文")))
	}

	otherSession := withTurnScope(asOwner, turn.StringID(), oid.NewID(oid.OtEvent).String())
	if _, _, err := sto.Convo().ReadToolRecord(otherSession, handle, 0, 100); !errors.Is(err, ErrForbidden) {
		t.Errorf("cross session read = %v, want ErrForbidden", err)
	}

	otherOwner := withTurnScope(withAccount(ctx, newAccountID()), turn.StringID(), sessionID)
	if _, _, err := sto.Convo().ReadToolRecord(otherOwner, handle, 0, 100); !errors.Is(err, ErrForbidden) {
		t.Errorf("cross owner read = %v, want ErrForbidden", err)
	}

	// 内置回读工具：同一 handle 走工具入口拿到同样的内容。
	inv := sto.Convo().InvokerForToolResultRead()
	res, err := inv(tctx, map[string]any{"handle": handle, "offset": 0, "limit": 100})
	if err != nil {
		t.Fatalf("InvokerForToolResultRead() error = %v", err)
	}
	parts, _ := res["content"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "结果全文" {
		t.Errorf("content = %v, want 结果全文", res["content"])
	}
	if _, err := inv(otherOwner, map[string]any{"handle": handle}); err == nil {
		t.Error("cross owner tool read should be refused")
	}
}

func TestIntegration_MessageOwnership(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	owner := newAccountID()
	asOwner := withAccount(ctx, owner)

	msg, err := sto.Convo().CreateMessage(asOwner, convo.MessageBasic{
		SessionID: oid.NewID(oid.OtEvent),
		Role:      "user",
		Content:   "只给本人看",
		TurnID:    oid.NewID(oid.OtEvent),
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	if msg.OwnerID != owner {
		t.Errorf("owner = %v, want %v", msg.OwnerID, owner)
	}

	if _, err := sto.Convo().GetMessage(withAccount(ctx, newAccountID()), msg.StringID()); !errors.Is(err, ErrForbidden) {
		t.Errorf("read another's message = %v, want ErrForbidden", err)
	}
	if _, err := sto.Convo().GetMessage(asOwner, msg.StringID()); err != nil {
		t.Errorf("read own message = %v, want nil", err)
	}
}
