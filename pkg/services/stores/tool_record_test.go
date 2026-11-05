package stores

import (
	"context"
	"errors"
	"testing"
)

// withTurnScope 造一个带轮次与会话锚点的上下文。
func withTurnScope(ctx context.Context, turnID, csid string) context.Context {
	ctx = ContextWithTurnID(ctx, turnID)
	return ContextWithConvoID(ctx, csid)
}

func TestToolScopeRequiresTurnAndSession(t *testing.T) {
	ctx := context.Background()

	if _, _, _, err := toolScope(ctx); !errors.Is(err, ErrNoTurnScope) {
		t.Errorf("without turn = %v, want ErrNoTurnScope", err)
	}
	if _, _, _, err := toolScope(ContextWithTurnID(ctx, "ev_1")); !errors.Is(err, ErrNoTurnScope) {
		t.Errorf("without session = %v, want ErrNoTurnScope", err)
	}
}

func TestToolScopeDerivesOwnerFromLogin(t *testing.T) {
	ctx := context.Background()
	owner := newAccountID()

	_, _, got, err := toolScope(withTurnScope(withAccount(ctx, owner), "ev_1", "cs_1"))
	if err != nil {
		t.Fatalf("toolScope: %v", err)
	}
	if got != owner {
		t.Errorf("owner = %v, want %v", got, owner)
	}

	_, _, got, err = toolScope(withTurnScope(ctx, "ev_1", "cs_1"))
	if err != nil {
		t.Fatalf("toolScope anonymous: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("anonymous owner = %v, want zero", got)
	}
}
