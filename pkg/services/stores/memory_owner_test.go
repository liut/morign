package stores

import (
	"context"
	"errors"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	auth "github.com/liut/simpauth"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/settings"
)

func newAccountID() oid.OID { return oid.NewID(oid.OtAccount) }

func withAccount(ctx context.Context, id oid.OID) context.Context {
	return auth.ContextWithUser(ctx, &auth.User{OID: id.String(), UID: "u-" + id.String()})
}

func withKeeper(ctx context.Context, id oid.OID) context.Context {
	return auth.ContextWithUser(ctx, &auth.User{
		OID:   id.String(),
		UID:   "u-" + id.String(),
		Roles: auth.Names{settings.Current.KeeperRole},
	})
}

func TestOwnerRestricted(t *testing.T) {
	ctx := context.Background()
	id := newAccountID()

	if restrictedToOwner(ctx) {
		t.Error("internal caller should not be restricted")
	}
	if !restrictedToOwner(withAccount(ctx, id)) {
		t.Error("regular user should be restricted")
	}
	if restrictedToOwner(withKeeper(ctx, id)) {
		t.Error("keeper should not be restricted")
	}
}

func TestOwnerIDFromContext(t *testing.T) {
	ctx := context.Background()
	id := newAccountID()

	if _, ok := ownerIDFromContext(ctx); ok {
		t.Error("internal caller has no owner id")
	}
	if got, ok := ownerIDFromContext(withAccount(ctx, id)); !ok || got != id {
		t.Errorf("owner id = %v, %v; want %v, true", got, ok, id)
	}
	invalid := auth.ContextWithUser(ctx, &auth.User{OID: "o1", UID: "u"})
	if _, ok := ownerIDFromContext(invalid); ok {
		t.Error("invalid oid should not resolve")
	}
}

func TestOwnerAllowed(t *testing.T) {
	ctx := context.Background()
	owner, other := newAccountID(), newAccountID()

	if !ownerAllowed(ctx, owner) || !ownerAllowed(ctx, other) {
		t.Error("internal caller should traverse all owners")
	}

	asOwner := withAccount(ctx, owner)
	if !ownerAllowed(asOwner, owner) {
		t.Error("user should reach own memory")
	}
	if ownerAllowed(asOwner, other) {
		t.Error("user should not reach another's memory")
	}

	if !ownerAllowed(withKeeper(ctx, owner), other) {
		t.Error("keeper should reach another's memory")
	}

	invalid := auth.ContextWithUser(ctx, &auth.User{OID: "", UID: "u"})
	if ownerAllowed(invalid, oid.ZeroID) {
		t.Error("invalid identity should be denied")
	}
}

func TestMemoryApplyOwnerScope(t *testing.T) {
	ctx := context.Background()
	a, b := newAccountID(), newAccountID()
	asUser, asKeeper := withAccount(ctx, a), withKeeper(ctx, a)

	cases := []struct {
		name      string
		ctx       context.Context
		spec      *ConvoMemorySpec
		wantOwner string
		wantLimit int
	}{
		{"user ignores ownerID", asUser, &ConvoMemorySpec{OwnerID: b.String(), IsOwner: true}, a.String(), defaultMemoryPageLimit},
		{"user defaults page limit", asUser, &ConvoMemorySpec{}, a.String(), defaultMemoryPageLimit},
		{"user keeps explicit limit", asUser, &ConvoMemorySpec{PageSpec: PageSpec{Limit: 3}}, a.String(), 3},
		{"keeper ownerID wins over own", asKeeper, &ConvoMemorySpec{OwnerID: b.String(), IsOwner: true}, b.String(), defaultMemoryPageLimit},
		{"keeper own is self", asKeeper, &ConvoMemorySpec{IsOwner: true}, a.String(), defaultMemoryPageLimit},
		{"keeper all owners", asKeeper, &ConvoMemorySpec{}, "", defaultMemoryPageLimit},
		{"internal keeps params", ctx, &ConvoMemorySpec{}, "", 0},
	}
	for _, c := range cases {
		if !c.spec.applyOwnerScope(c.ctx) {
			t.Errorf("%s: unexpectedly denied", c.name)
			continue
		}
		if c.spec.OwnerID != c.wantOwner {
			t.Errorf("%s: ownerID = %q, want %q", c.name, c.spec.OwnerID, c.wantOwner)
		}
		if c.spec.Limit != c.wantLimit {
			t.Errorf("%s: limit = %d, want %d", c.name, c.spec.Limit, c.wantLimit)
		}
	}
}

func TestMemoryApplyOwnerScopeDeniesInvalidIdentity(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.User{OID: "o1", UID: "u"})
	if (&ConvoMemorySpec{}).applyOwnerScope(ctx) {
		t.Error("invalid identity should be denied")
	}
}

func TestMemoryCreateHookForcesOwner(t *testing.T) {
	ctx := context.Background()
	requester := newAccountID()

	obj := convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: newAccountID(), Key: "k"})
	if err := dbBeforeCreateMemory(withAccount(ctx, requester), nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMemory: %v", err)
	}
	if obj.OwnerID != requester {
		t.Errorf("owner = %v, want requester %v", obj.OwnerID, requester)
	}

	keeper := newAccountID()
	obj = convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: requester, Key: "k2"})
	if err := dbBeforeCreateMemory(withKeeper(ctx, keeper), nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMemory keeper: %v", err)
	}
	if obj.OwnerID != keeper {
		t.Errorf("keeper owner = %v, want keeper %v", obj.OwnerID, keeper)
	}

	keep := newAccountID()
	obj = convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: keep, Key: "k3"})
	if err := dbBeforeCreateMemory(ctx, nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMemory internal: %v", err)
	}
	if obj.OwnerID != keep {
		t.Errorf("internal owner = %v, want %v", obj.OwnerID, keep)
	}
}

func TestMemoryAccessHooks(t *testing.T) {
	ctx := context.Background()
	a, b := newAccountID(), newAccountID()
	mine := convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: a, Key: "k"})
	other := convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: b, Key: "k"})
	sto := &convoStore{}

	asUser := withAccount(ctx, a)
	if err := sto.afterLoadMemory(asUser, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("load another's memory = %v, want ErrForbidden", err)
	}
	if err := dbBeforeUpdateMemory(asUser, nil, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("update another's memory = %v, want ErrForbidden", err)
	}
	if err := dbBeforeDeleteMemory(asUser, nil, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete another's memory = %v, want ErrForbidden", err)
	}

	if err := sto.afterLoadMemory(asUser, mine); err != nil {
		t.Errorf("load own memory = %v, want nil", err)
	}
	if err := dbBeforeUpdateMemory(asUser, nil, mine); err != nil {
		t.Errorf("update own memory = %v, want nil", err)
	}

	asKeeper := withKeeper(ctx, a)
	if err := sto.afterLoadMemory(asKeeper, other); err != nil {
		t.Errorf("keeper load = %v, want nil", err)
	}
	if err := dbBeforeUpdateMemory(asKeeper, nil, other); err != nil {
		t.Errorf("keeper update = %v, want nil", err)
	}
}
