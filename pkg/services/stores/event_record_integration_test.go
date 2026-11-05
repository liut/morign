//go:build integration

package stores

import (
	"context"
	"testing"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/services/llm"
)

func TestIntegration_AppendEventWritesRawRecords(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	owner := newAccountID()
	asOwner := withAccount(ctx, owner)

	turn := testTurnFor(t, ctx, "append")
	sessionID := oid.NewID(oid.OtEvent).String()
	tctx := withTurnScope(asOwner, turn.StringID(), sessionID)
	hs := NewHistoryStore(sto)

	// 两个工具调用：各有配对的记录。
	for i, callID := range []string{"c1", "c2"} {
		err := hs.AppendEvent(tctx, sessionID, &llm.Event{
			Author: "big",
			ToolResult: &llm.ToolResult{
				CallID:     callID,
				Name:       "big",
				Content:    "结果" + callID,
				Status:     llm.ToolStatusOK,
				Arguments:  `{"q":1}`,
				Seq:        i,
				DurationMS: 7,
			},
		})
		if err != nil {
			t.Fatalf("AppendEvent(tool %s) error = %v", callID, err)
		}
	}

	rows, _, err := sto.Convo().ListToolRecord(tctx, &ConvoToolRecordSpec{
		TurnID: turn.StringID(),
	})
	if err != nil {
		t.Fatalf("ListToolRecord() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("tool records = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.ToolName != "big" {
			t.Errorf("tool name = %q, want big", r.ToolName)
		}
		if r.Status != llm.ToolStatusOK {
			t.Errorf("status = %q, want ok", r.Status)
		}
		if r.Content != "结果"+r.CallID {
			t.Errorf("content = %q, want 结果%s", r.Content, r.CallID)
		}
		if r.DurationMS != 7 {
			t.Errorf("duration = %d, want 7", r.DurationMS)
		}
	}

	// 助手消息：用户提问与助手回答各一行。
	err = hs.AppendEvent(tctx, sessionID, &llm.Event{
		Author:     "assistant",
		Delta:      "回答",
		UserPrompt: "提问",
	})
	if err != nil {
		t.Fatalf("AppendEvent(assistant) error = %v", err)
	}

	msgs, _, err := sto.Convo().ListMessage(tctx, &ConvoMessageSpec{SessionID: sessionID})
	if err != nil {
		t.Fatalf("ListMessage() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	roles := map[string]string{}
	for _, m := range msgs {
		roles[m.Role] = m.Content
		if m.OwnerID != owner {
			t.Errorf("owner = %v, want %v", m.OwnerID, owner)
		}
		if m.TurnID != oid.Cast(turn.StringID()) {
			t.Errorf("turn = %v, want %v", m.TurnID, turn.StringID())
		}
	}
	if roles["user"] != "提问" || roles["assistant"] != "回答" {
		t.Errorf("messages = %v, want user=提问 assistant=回答", roles)
	}
}
