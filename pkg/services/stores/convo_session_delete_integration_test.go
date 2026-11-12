//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/convo"
)

// sessionTree 是一个会话连同挂在它下面的三张子表的行。
type sessionTree struct {
	sessionID string
	owner     oid.OID
}

// seedSessionTree 建会话并挂上消息、工具记录、轮次各一行。
func seedSessionTree(t *testing.T, ctx context.Context, suffix string) sessionTree {
	t.Helper()

	owner := newAccountID()
	asOwner := withAccount(ctx, owner)

	sess := convo.NewSessionWithBasic(convo.SessionBasic{Title: "cascade-" + suffix})
	if err := Sgt().Convo().SaveSession(asOwner, sess); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	if _, err := Sgt().Convo().CreateMessage(asOwner, convo.MessageBasic{
		SessionID: sess.ID,
		Role:      "user",
		Content:   "原始提问",
	}); err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}

	turn, err := Sgt().Convo().CreateTurn(asOwner, convo.ChatTurnBasic{
		SessionID:      sess.ID,
		IdempotencyKey: fmt.Sprintf("cascade-%s-%d", suffix, os.Getpid()),
		Source:         "test",
		Status:         convo.TurnStatusSucceeded,
		RequestDigest:  "digest",
		Result:         "模型最终答案",
	})
	if err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}

	if _, err := Sgt().Convo().CreateToolRecord(asOwner, convo.ToolRecordBasic{
		TurnID:    turn.ID,
		SessionID: sess.ID,
		Seq:       1,
		CallID:    "c1",
		ToolName:  "kb_search",
		Arguments: "{}",
		Status:    "ok",
		Content:   "工具结果",
	}); err != nil {
		t.Fatalf("CreateToolRecord() error = %v", err)
	}

	return sessionTree{sessionID: sess.StringID(), owner: owner}
}

// countSessionRows 统计某表里属于该会话的行数；表不存在时返回 0。
func countSessionRows(t *testing.T, ctx context.Context, table string, sessionID int64) int {
	t.Helper()
	count, err := Sgt().db.NewSelect().Table(table).
		Where("session_id = ?", sessionID).
		Count(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return 0
		}
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// childTables 是会话的附属表：消息、工具记录、轮次。
var childTables = []string{"convo_message", "convo_tool_record", "convo_chat_turn"}

func assertChildrenGone(t *testing.T, ctx context.Context, sessionID int64) {
	t.Helper()
	for _, tbl := range childTables {
		if n := countSessionRows(t, ctx, tbl, sessionID); n != 0 {
			t.Errorf("%s still holds %d rows of the deleted session", tbl, n)
		}
	}
}

func assertChildrenIntact(t *testing.T, ctx context.Context, sessionID int64) {
	t.Helper()
	for _, tbl := range childTables {
		if n := countSessionRows(t, ctx, tbl, sessionID); n == 0 {
			t.Errorf("%s lost its rows after a rejected delete", tbl)
		}
	}
}

func TestIntegration_DeleteSessionCascadesChildren(t *testing.T) {
	ctx := context.Background()
	tree := seedSessionTree(t, ctx, "cascade")
	sessionID := int64(oid.Cast(tree.sessionID))

	assertChildrenIntact(t, ctx, sessionID)

	if err := Sgt().Convo().DeleteSession(withAccount(ctx, tree.owner), tree.sessionID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	assertChildrenGone(t, ctx, sessionID)

	// 答案原文不许留在 trash schema：移动删除等于换个地方存着答案。
	for _, tbl := range childTables {
		trash := fmt.Sprintf("%s.%s", Sgt().db.SchemaCrap(), tbl)
		if n := countSessionRows(t, ctx, trash, sessionID); n != 0 {
			t.Errorf("%s keeps %d rows of the deleted session", trash, n)
		}
	}
}

func TestIntegration_DeleteSessionForbiddenForOtherUser(t *testing.T) {
	ctx := context.Background()
	tree := seedSessionTree(t, ctx, "forbidden")
	sessionID := int64(oid.Cast(tree.sessionID))

	other := newAccountID()
	err := Sgt().Convo().DeleteSession(withAccount(ctx, other), tree.sessionID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeleteSession() error = %v, want ErrForbidden", err)
	}

	assertChildrenIntact(t, ctx, sessionID)
}

func TestIntegration_KeeperDeletesOtherSession(t *testing.T) {
	ctx := context.Background()
	tree := seedSessionTree(t, ctx, "keeper")
	sessionID := int64(oid.Cast(tree.sessionID))

	keeper := newAccountID()
	if err := Sgt().Convo().DeleteSession(withKeeper(ctx, keeper), tree.sessionID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	assertChildrenGone(t, ctx, sessionID)
}
