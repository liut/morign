package stores

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cupogo/andvari/models/comm"
	"github.com/cupogo/andvari/models/field"
	"github.com/cupogo/andvari/models/oid"
	"github.com/liut/morign/pkg/settings"
	auth "github.com/liut/simpauth"
)

const (
	TokenExpire = time.Hour * 24
)

func tokenUserKey(token string) string {
	return "tk-o-user-" + token
}

func userTokenKey(id string) string {
	return "user-o-token-" + id
}

type IUser interface {
	auth.IUser

	GetEmail() string
	GetPhone() string
}

type Encoder = auth.Encoder
type User = auth.User

// UserFromContext gets user information from context
func UserFromContext(ctx context.Context) (user *User, ok bool) {
	if iu, iok := auth.UserFromContext(ctx); iok {
		if user, ok = iu.(*User); ok {
			return
		}
		au := auth.ToUser(iu)
		user = &au
		ok = true
	}
	return
}

// IsKeeper checks if the user in the current context has keeper role or UID
func IsKeeper(ctx context.Context) bool {
	user, ok := UserFromContext(ctx)
	if !ok {
		return false
	}
	return UserIsKeeper(user)
}

// UserIsKeeper checks if a user has keeper role or UID
func UserIsKeeper(user *User) bool {
	if user == nil {
		return false
	}
	// 检查 UID 是否在白名单中
	if len(settings.Current.KeeperUIDs) > 0 && slices.Contains(settings.Current.KeeperUIDs, user.UID) {
		return true
	}
	return slices.Contains(user.Roles, settings.Current.KeeperRole)
}

// contextKey for OAuth token
type oauthTokenKeyType struct{}

var oauthTokenKey = oauthTokenKeyType{}

// OAuthTokenFromContext 从 context 获取 token
func OAuthTokenFromContext(ctx context.Context) string {
	if tok, ok := ctx.Value(oauthTokenKey).(string); ok {
		return tok
	}
	return ""
}

// OAuthContextWithToken 将 token 添加到 context
func OAuthContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, oauthTokenKey, token)
}

// SaveUserWithToken saves user.Encode() into redis
func SaveUserWithToken(ctx context.Context, user Encoder, token string) error {
	s, err := user.Encode()
	if err != nil {
		logger().Info("encode user failed", "err", err)
		return err
	}
	key := tokenUserKey(token)
	if err := SgtRC().Set(ctx, key, s, TokenExpire).Err(); err != nil {
		logger().Info("save user to redis failed", "key", key, "err", err)
		return err
	}
	return nil
}

// LoadUserFromToken loads saved user from redis by token
func LoadUserFromToken(ctx context.Context, token string) (*User, error) {
	key := tokenUserKey(token)
	s, err := SgtRC().Get(ctx, key).Result()
	if err != nil {
		logger().Info("load user from redis failed", "key", key, "err", err)
		return nil, err
	}
	var user User
	if err := user.Decode(s); err != nil {
		logger().Info("decode user failed", "err", err)
		return nil, err
	}
	return &user, nil
}

func DeleteUserToken(ctx context.Context, token string) error {
	key := tokenUserKey(token)
	err := SgtRC().Del(ctx, key).Err()
	if err != nil {
		logger().Info("del token from redis failed", "key", key, "err", err)
		return err
	}
	return nil
}

func SaveTokenWithUser(ctx context.Context, id, token string) error {
	return SaveTokenWithExpiry(ctx, id, token, TokenExpire)
}

func SaveTokenWithExpiry(ctx context.Context, id, token string, expiry time.Duration) error {
	if len(id) == 0 || len(token) == 0 {
		return fmt.Errorf("empty id or token")
	}
	key := userTokenKey(id)
	if err := SgtRC().Set(ctx, key, token, expiry).Err(); err != nil {
		logger().Info("save token to redis failed", "key", key, "err", err)
		return err
	}
	return nil
}

func LoadTokenWithUser(ctx context.Context, id string) (string, error) {
	key := userTokenKey(id)
	tok, err := SgtRC().Get(ctx, key).Result()
	if err != nil {
		logger().Info("load token from redis failed", "key", key, "err", err)
		return "", err
	}
	return tok, nil
}

const (
	WecomUID  = "wecomUID"
	WecomHead = "wecom:"
)

type wecomUIDKey struct{}

func WecomUIDFromContext(ctx context.Context) string {
	if uid, ok := ctx.Value(wecomUIDKey{}).(string); ok {
		return uid
	}
	return ""
}

func ContextWithWecomUID(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, wecomUIDKey{}, uid)
}

type ModelMeta = comm.ModelMeta

// ModelMetaCreator is the model metadata creator interface
type ModelMetaCreator interface {
	ModelMeta
	comm.ModelCreator
}

type ModelMetaOwner interface {
	ModelMeta
	comm.ModelCreator
	comm.ModelOwner
}

// OpModelCreator gets model creator information from context
func OpModelCreator(ctx context.Context, obj comm.ModelCreator) (id oid.OID, name string) {
	if user, ok := UserFromContext(ctx); ok {
		if obj.GetCreatorID().IsZero() {
			id := oid.Cast(user.OID)
			obj.SetCreatorID(id)
			return id, user.Name
		}
	}

	return obj.GetCreatorID(), ""
}

// DbOpModelMetaCreator sets model creator information in database operations
func DbOpModelMetaCreator(ctx context.Context, db ormDB, obj ModelMetaCreator) (err error) {
	id, name := OpModelCreator(ctx, obj)
	if !id.Valid() {
		return
	}

	if len(name) > 0 {
		obj.MetaSet(field.MetaCreator, name)
		if obj.CountChange() > 0 {
			obj.SetChange(field.Meta)
		}
	}

	return
}

func DbOpModelMetaOwner(ctx context.Context, db ormDB, obj ModelMetaOwner) (err error) {
	ownID := obj.GetOwnerID()
	if !ownID.Valid() {
		return
	}
	if _, cat, _ := ownID.Split(); cat != oid.OtAccount {
		logger().Info("not account", "ownID", ownID)
		return
	}
	return opModelMetaSet(ctx, obj, field.MetaOwner, obj.GetOwnerID(), func(ctx context.Context, id oid.OID) (any, error) {
		user := new(ConvoUser)
		err := dbGetWithPKID(ctx, db, user, id)
		if err != nil {
			return "", err
		}
		return user.Nickname, nil
	})
}

// dbModelMetaUps updates model metadata before and after database operations
func dbModelMetaUps(ctx context.Context, db ormDB, obj Model) {
	if v, ok := obj.(ModelMetaCreator); ok {
		_ = DbOpModelMetaCreator(ctx, db, v)
	} else if v, ok := obj.(comm.ModelCreator); ok {
		_, _ = OpModelCreator(ctx, v)
	}

	if v, ok := obj.(ModelMetaOwner); ok {
		_ = DbOpModelMetaOwner(ctx, db, v)
	}

}
