package stores

import (
	"context"
	"errors"
	"strings"
	"testing"

	auth "github.com/liut/simpauth"

	"github.com/liut/morign/pkg/models/convo"
)

func TestMessageCreateHookForcesOwner(t *testing.T) {
	ctx := context.Background()
	requester := newAccountID()

	obj := convo.NewMessageWithBasic(convo.MessageBasic{Role: "user", Content: "你好"})
	if err := dbBeforeCreateMessage(withAccount(ctx, requester), nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMessage: %v", err)
	}
	if obj.OwnerID != requester {
		t.Errorf("owner = %v, want requester %v", obj.OwnerID, requester)
	}

	keeper := newAccountID()
	obj = convo.NewMessageWithBasic(convo.MessageBasic{Role: "user", Content: "你好"})
	if err := dbBeforeCreateMessage(withKeeper(ctx, keeper), nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMessage keeper: %v", err)
	}
	if obj.OwnerID != keeper {
		t.Errorf("keeper owner = %v, want keeper %v", obj.OwnerID, keeper)
	}

	keep := newAccountID()
	obj = convo.NewMessageWithBasic(convo.MessageBasic{Role: "user", Content: "你好", OwnerID: keep})
	if err := dbBeforeCreateMessage(ctx, nil, obj); err != nil {
		t.Fatalf("dbBeforeCreateMessage internal: %v", err)
	}
	if obj.OwnerID != keep {
		t.Errorf("internal owner = %v, want keep %v", obj.OwnerID, keep)
	}
}

func TestMessageAccessHooks(t *testing.T) {
	ctx := context.Background()
	a, b := newAccountID(), newAccountID()
	mine := convo.NewMessageWithBasic(convo.MessageBasic{Role: "user", Content: "我的", OwnerID: a})
	other := convo.NewMessageWithBasic(convo.MessageBasic{Role: "user", Content: "别人的", OwnerID: b})
	sto := &convoStore{}

	asUser := withAccount(ctx, a)
	if err := sto.afterLoadMessage(asUser, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("load another's message = %v, want ErrForbidden", err)
	}
	if err := dbBeforeUpdateMessage(asUser, nil, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("update another's message = %v, want ErrForbidden", err)
	}
	if err := dbBeforeDeleteMessage(asUser, nil, other); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete another's message = %v, want ErrForbidden", err)
	}

	if err := sto.afterLoadMessage(asUser, mine); err != nil {
		t.Errorf("load own message = %v, want nil", err)
	}
	if err := dbBeforeUpdateMessage(asUser, nil, mine); err != nil {
		t.Errorf("update own message = %v, want nil", err)
	}
	if err := dbBeforeDeleteMessage(asUser, nil, mine); err != nil {
		t.Errorf("delete own message = %v, want nil", err)
	}

	asKeeper := withKeeper(ctx, a)
	if err := sto.afterLoadMessage(asKeeper, other); err != nil {
		t.Errorf("keeper load = %v, want nil", err)
	}
}

func TestMessageListOwnerScope(t *testing.T) {
	ctx := context.Background()
	a, b := newAccountID(), newAccountID()
	asUser := withAccount(ctx, a)

	spec := &ConvoMessageSpec{OwnerID: b.String()}
	spec.SiftX(asUser, probeQuery())
	if spec.OwnerID != a.String() {
		t.Errorf("owner = %q, want requester %q", spec.OwnerID, a.String())
	}

	keeperSpec := &ConvoMessageSpec{OwnerID: b.String()}
	keeperSpec.SiftX(withKeeper(ctx, a), probeQuery())
	if keeperSpec.OwnerID != b.String() {
		t.Errorf("keeper owner = %q, want %q", keeperSpec.OwnerID, b.String())
	}

	invalid := &ConvoMessageSpec{}
	sql := invalid.SiftX(auth.ContextWithUser(ctx, &auth.User{OID: "o1", UID: "u"}), probeQuery()).String()
	if !strings.Contains(sql, "FALSE") {
		t.Errorf("sql = %q, want an empty result for an invalid identity", sql)
	}
}
