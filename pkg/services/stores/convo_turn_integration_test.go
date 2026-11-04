//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/liut/morign/pkg/models/convo"
)

func turnKeyForTest() string {
	return fmt.Sprintf("test-turn-%d", os.Getpid())
}

func TestIntegration_ChatTurnLifecycle(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	key := turnKeyForTest()

	turn, err := sto.Convo().CreateTurn(ctx, convo.ChatTurnBasic{
		IdempotencyKey: key,
		Source:         "test",
		Status:         TurnStatusQueued,
		RequestDigest:  "digest",
	})
	if err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}

	found, err := sto.Convo().GetTurnByIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("GetTurnByIdempotencyKey() error = %v", err)
	}
	if found.StringID() != turn.StringID() {
		t.Errorf("found turn = %q, want %q", found.StringID(), turn.StringID())
	}

	if err := sto.Convo().UpdateTurnStatus(ctx, turn.StringID(), TurnStatusRunning, ""); err != nil {
		t.Fatalf("UpdateTurnStatus() error = %v", err)
	}
	found, err = sto.Convo().GetTurnByIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("GetTurnByIdempotencyKey() error = %v", err)
	}
	if found.Status != TurnStatusRunning {
		t.Errorf("status = %q, want %q", found.Status, TurnStatusRunning)
	}
}

func TestIntegration_ChatTurnDuplicateKey(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	key := turnKeyForTest() + "-dup"

	if _, err := sto.Convo().CreateTurn(ctx, convo.ChatTurnBasic{
		IdempotencyKey: key,
		Source:         "test",
		Status:         TurnStatusQueued,
		RequestDigest:  "digest",
	}); err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}

	_, err := sto.Convo().CreateTurn(ctx, convo.ChatTurnBasic{
		IdempotencyKey: key,
		Source:         "test",
		Status:         TurnStatusQueued,
		RequestDigest:  "digest",
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("CreateTurn() error = %v, want ErrDuplicate", err)
	}
}

func TestIntegration_ReclaimInterruptedTurns(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	key := turnKeyForTest() + "-reclaim"

	turn, err := sto.Convo().CreateTurn(ctx, convo.ChatTurnBasic{
		IdempotencyKey: key,
		Source:         "test",
		Status:         TurnStatusRunning,
		RequestDigest:  "digest",
	})
	if err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}

	if _, err := sto.Convo().ReclaimInterruptedTurns(ctx); err != nil {
		t.Fatalf("ReclaimInterruptedTurns() error = %v", err)
	}

	found, err := sto.Convo().GetTurnByIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("GetTurnByIdempotencyKey() error = %v", err)
	}
	if found.Status != TurnStatusInterrupted {
		t.Errorf("status = %q, want %q", found.Status, TurnStatusInterrupted)
	}
	_ = turn
}
