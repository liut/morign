package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/stores"
)

// turnUpdate 记录一次轮次状态更新。
type turnUpdate struct {
	id     string
	status string
	result string
}

// fakeTurnStore 是轮次存储的测试替身：内存里模拟唯一键冲突与状态流转。
type fakeTurnStore struct {
	mu      sync.Mutex
	byKey   map[string]*convo.ChatTurn
	byID    map[string]*convo.ChatTurn
	creates int
	updates []turnUpdate
}

func newFakeTurnStore() *fakeTurnStore {
	return &fakeTurnStore{byKey: map[string]*convo.ChatTurn{}, byID: map[string]*convo.ChatTurn{}}
}

func (f *fakeTurnStore) CreateTurn(_ context.Context, in convo.ChatTurnBasic) (*convo.ChatTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if _, ok := f.byKey[in.IdempotencyKey]; ok {
		return nil, stores.ErrDuplicate
	}
	turn := convo.NewChatTurnWithBasic(in)
	_ = turn.SetID(oid.NewID(oid.OtEvent))
	f.byKey[in.IdempotencyKey] = turn
	f.byID[turn.StringID()] = turn
	return turn, nil
}

func (f *fakeTurnStore) GetTurnByIdempotencyKey(_ context.Context, key string) (*convo.ChatTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	turn, ok := f.byKey[key]
	if !ok {
		return nil, stores.ErrNotFound
	}
	return turn, nil
}

func (f *fakeTurnStore) UpdateTurnStatus(_ context.Context, id, status, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, turnUpdate{id: id, status: status, result: result})
	if turn, ok := f.byID[id]; ok {
		turn.Status = status
		turn.Result = result
	}
	return nil
}

// seed 预置一条轮次，返回它的编号。
func (f *fakeTurnStore) seed(in convo.ChatTurnBasic) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	turn := convo.NewChatTurnWithBasic(in)
	_ = turn.SetID(oid.NewID(oid.OtEvent))
	f.byKey[in.IdempotencyKey] = turn
	f.byID[turn.StringID()] = turn
	return turn.StringID()
}

func (f *fakeTurnStore) lastUpdate() (turnUpdate, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) == 0 {
		return turnUpdate{}, false
	}
	return f.updates[len(f.updates)-1], true
}

func testTurnBasic(key string) convo.ChatTurnBasic {
	return convo.ChatTurnBasic{
		IdempotencyKey: key,
		Source:         "channel",
		Status:         stores.TurnStatusQueued,
		RequestDigest:  "digest",
	}
}

func TestAcquireTurnFirstRequestProceeds(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)

	gate, err := acquireTurn(context.Background(), sto, testTurnBasic("wecom:m1"), owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if !gate.Proceed || gate.Replay {
		t.Errorf("gate = %+v, want proceed without replay", gate)
	}
	if gate.ID == "" {
		t.Error("gate.ID is empty")
	}
	upd, ok := sto.lastUpdate()
	if !ok || upd.status != stores.TurnStatusRunning {
		t.Errorf("status update = %+v, want running", upd)
	}
}

func TestAcquireTurnDuplicateSucceededReplays(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	in.OwnerID = owner
	seeded := sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Source:         in.Source,
		Status:         stores.TurnStatusSucceeded,
		RequestDigest:  in.RequestDigest,
		OwnerID:        owner,
		Result:         "已完成的答案",
	})

	gate, err := acquireTurn(context.Background(), sto, in, owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if !gate.Replay {
		t.Errorf("gate = %+v, want replay", gate)
	}
	if gate.Cached != "已完成的答案" {
		t.Errorf("cached = %q, want 已完成的答案", gate.Cached)
	}
	if gate.ID != seeded {
		t.Errorf("gate.ID = %q, want %q", gate.ID, seeded)
	}
	if sto.creates != 1 {
		t.Errorf("creates = %d, want 1", sto.creates)
	}
}

func TestAcquireTurnDuplicateRunningSkips(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	in.OwnerID = owner
	sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Status:         stores.TurnStatusRunning,
		RequestDigest:  in.RequestDigest,
		OwnerID:        owner,
	})

	gate, err := acquireTurn(context.Background(), sto, in, owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if gate.Proceed || gate.Replay {
		t.Errorf("gate = %+v, want neither proceed nor replay", gate)
	}
}

func TestAcquireTurnDuplicateInterruptedSkips(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	in.OwnerID = owner
	sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Status:         stores.TurnStatusInterrupted,
		RequestDigest:  in.RequestDigest,
		OwnerID:        owner,
	})

	gate, err := acquireTurn(context.Background(), sto, in, owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if gate.Proceed || gate.Replay {
		t.Errorf("gate = %+v, want neither proceed nor replay", gate)
	}
}

func TestAcquireTurnDuplicateFailedReruns(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	in.OwnerID = owner
	seeded := sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Status:         stores.TurnStatusFailed,
		RequestDigest:  in.RequestDigest,
		OwnerID:        owner,
	})

	gate, err := acquireTurn(context.Background(), sto, in, owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if !gate.Proceed || gate.Replay {
		t.Errorf("gate = %+v, want proceed", gate)
	}
	if gate.ID != seeded {
		t.Errorf("gate.ID = %q, want %q", gate.ID, seeded)
	}
	upd, ok := sto.lastUpdate()
	if !ok || upd.status != stores.TurnStatusRunning {
		t.Errorf("status update = %+v, want running", upd)
	}
}

func TestAcquireTurnDigestConflictRejected(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	in.OwnerID = owner
	sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Status:         stores.TurnStatusSucceeded,
		RequestDigest:  "另一个请求的摘要",
		OwnerID:        owner,
		Result:         "旧答案",
	})

	_, err := acquireTurn(context.Background(), sto, in, owner)
	if !errors.Is(err, ErrTurnConflict) {
		t.Errorf("err = %v, want ErrTurnConflict", err)
	}
}

func TestAcquireTurnOtherOwnerGetsNoResult(t *testing.T) {
	sto := newFakeTurnStore()
	owner := oid.NewID(oid.OtEvent)
	other := oid.NewID(oid.OtEvent)
	in := testTurnBasic("wecom:m1")
	sto.seed(convo.ChatTurnBasic{
		IdempotencyKey: in.IdempotencyKey,
		Status:         stores.TurnStatusSucceeded,
		RequestDigest:  in.RequestDigest,
		OwnerID:        other,
		Result:         "别人的答案",
	})

	gate, err := acquireTurn(context.Background(), sto, in, owner)
	if err != nil {
		t.Fatalf("acquireTurn() error = %v", err)
	}
	if gate.Replay {
		t.Error("gate.Replay = true, want false for another owner")
	}
	if gate.Cached != "" {
		t.Errorf("cached = %q, want empty", gate.Cached)
	}
}

func TestFinishTurnStatuses(t *testing.T) {
	owner := oid.NewID(oid.OtEvent)
	runErr := errors.New("boom")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name    string
		ctx     context.Context
		answer  string
		toolRan bool
		runErr  error
		want    string
	}{
		{name: "成功", ctx: context.Background(), answer: "答案", want: stores.TurnStatusSucceeded},
		{name: "失败", ctx: context.Background(), runErr: runErr, want: stores.TurnStatusFailed},
		{name: "取消且无工具", ctx: cancelled, runErr: runErr, want: stores.TurnStatusCancelled},
		{name: "取消且已有工具", ctx: cancelled, runErr: runErr, toolRan: true, want: stores.TurnStatusInterrupted},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sto := newFakeTurnStore()
			in := testTurnBasic("wecom:m1")
			in.OwnerID = owner
			turnID := sto.seed(in)

			finishTurn(tt.ctx, sto, turnID, tt.answer, tt.toolRan, tt.runErr)

			upd, ok := sto.lastUpdate()
			if !ok {
				t.Fatal("no status update recorded")
			}
			if upd.status != tt.want {
				t.Errorf("status = %q, want %q", upd.status, tt.want)
			}
		})
	}
}

func TestFinishTurnWithoutTurnIDDoesNothing(t *testing.T) {
	sto := newFakeTurnStore()
	finishTurn(context.Background(), sto, "", "答案", false, nil)
	if len(sto.updates) != 0 {
		t.Errorf("updates = %v, want none", sto.updates)
	}
}

func TestTurnKeyForWebSeparatesOwners(t *testing.T) {
	a := oid.NewID(oid.OtEvent)
	b := oid.NewID(oid.OtEvent)

	ka := turnKeyForWeb(a, "cs1", "同样的问题", "digest")
	kb := turnKeyForWeb(b, "cs1", "同样的问题", "digest")
	if ka == kb {
		t.Error("different owners must not share an idempotency key")
	}
	if turnKeyForWeb(a, "cs1", "同样的问题", "digest") != ka {
		t.Error("key derivation is not stable")
	}
	if ka == turnKeyForWeb(a, "cs2", "同样的问题", "digest") {
		t.Error("different sessions must not share an idempotency key")
	}
}

func TestTurnKeyForChannelUsesMessageID(t *testing.T) {
	if got := turnKeyForChannel("wecom", "m1"); got != "wecom:m1" {
		t.Errorf("key = %q, want wecom:m1", got)
	}
}

func TestWebTurnKeyRetryGetsFreshKey(t *testing.T) {
	owner := oid.NewID(oid.OtEvent)
	param := &ChatRequest{Prompt: "同样的问题", ConversationID: "cs1"}

	key1, digest1 := webTurnKey(owner, param, false)
	key2, digest2 := webTurnKey(owner, param, false)
	if key1 != key2 {
		t.Errorf("key = %q vs %q, want stable", key1, key2)
	}
	if digest1 != digest2 {
		t.Errorf("digest = %q vs %q, want stable", digest1, digest2)
	}

	retryKey, retryDigest := webTurnKey(owner, param, true)
	if retryKey == key1 {
		t.Error("retry must not reuse the idempotency key")
	}
	if retryDigest != digest1 {
		t.Errorf("retry digest = %q, want %q", retryDigest, digest1)
	}
}

func TestWebTurnKeySeparatesOwners(t *testing.T) {
	param := &ChatRequest{Prompt: "同样的问题", ConversationID: "cs1"}

	ka, _ := webTurnKey(oid.NewID(oid.OtEvent), param, false)
	kb, _ := webTurnKey(oid.NewID(oid.OtEvent), param, false)
	if ka == kb {
		t.Error("different owners must not share an idempotency key")
	}
}
