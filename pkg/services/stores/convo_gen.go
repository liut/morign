// This file is generated - Do Not Edit.

package stores

import (
	"context"

	"github.com/liut/morign/pkg/models/convo"
)

type ConvoUser = convo.User

// type ConvoMemory = convo.Memory
// type ConvoMessage = convo.Message
// type ConvoSession = convo.Session
// type ConvoThirdUser = convo.ThirdUser
// type ConvoUsageRecord = convo.UsageRecord

func init() {
	RegisterModel((*convo.Session)(nil), (*convo.Message)(nil), (*convo.UsageRecord)(nil), (*convo.User)(nil), (*convo.ThirdUser)(nil), (*convo.Memory)(nil))
}

type ConvoStore interface {
	ConvoStoreX

	ListSession(ctx context.Context, spec *ConvoSessionSpec) (data convo.Sessions, total int, err error)
	GetSession(ctx context.Context, id string) (obj *convo.Session, err error)
	UpdateSession(ctx context.Context, id string, in convo.SessionSet) error
	DeleteSession(ctx context.Context, id string) error

	ListMessage(ctx context.Context, spec *ConvoMessageSpec) (data convo.Messages, total int, err error)
	GetMessage(ctx context.Context, id string) (obj *convo.Message, err error)
	DeleteMessage(ctx context.Context, id string) error

	ListUser(ctx context.Context, spec *ConvoUserSpec) (data convo.Users, total int, err error)
	GetUser(ctx context.Context, id string) (obj *convo.User, err error)
	CreateUser(ctx context.Context, in convo.UserBasic) (obj *convo.User, err error)
	UpdateUser(ctx context.Context, id string, in convo.UserSet) error
	DeleteUser(ctx context.Context, id string) error

	ListThirdUser(ctx context.Context, spec *ConvoThirdUserSpec) (data convo.ThirdUsers, total int, err error)
	GetThirdUser(ctx context.Context, id string) (obj *convo.ThirdUser, err error)
	DeleteThirdUser(ctx context.Context, id string) error

	ListMemory(ctx context.Context, spec *ConvoMemorySpec) (data convo.Memories, total int, err error)
	GetMemory(ctx context.Context, id string) (obj *convo.Memory, err error)
	CreateMemory(ctx context.Context, in convo.MemoryBasic) (obj *convo.Memory, err error)
	UpdateMemory(ctx context.Context, id string, in convo.MemorySet) error
	DeleteMemory(ctx context.Context, id string) error

	ListUsageRecord(ctx context.Context, spec *ConvoUsageRecordSpec) (data convo.UsageRecords, total int, err error)
	GetUsageRecord(ctx context.Context, id string) (obj *convo.UsageRecord, err error)
	CreateUsageRecord(ctx context.Context, in convo.UsageRecordBasic) (obj *convo.UsageRecord, err error)
	DeleteUsageRecord(ctx context.Context, id string) error
}

type ConvoSessionSpec struct {
	PageSpec
	ModelSpec

	// 标题
	Title string `extensions:"x-order=A" form:"title" json:"title"`
	// 状态
	//  * `open` - 开启
	//  * `closed` - 关闭
	Status convo.SessionStatus `extensions:"x-order=B" form:"status" json:"status" swaggertype:"string"`
	// 频道
	Channel string `extensions:"x-order=C" form:"channel" json:"channel"`
	// 所有者编号 (多值使用逗号分隔)
	OwnerID string `extensions:"x-order=D" form:"owner" json:"owner,omitempty"`
}

func (spec *ConvoSessionSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftMatch(q, "title", spec.Title, false)
	q, _ = siftEqual(q, "status", spec.Status, false)
	q, _ = siftICE(q, "channel", spec.Channel, false)
	q, _ = siftOIDs(q, "owner_id", spec.OwnerID, false)

	return q
}

type ConvoMessageSpec struct {
	PageSpec
	ModelSpec

	// 会话编号
	SessionID string `extensions:"x-order=A" form:"session" json:"session"`
	// 角色
	Role string `extensions:"x-order=B" form:"role" json:"role"`
}

func (spec *ConvoMessageSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "session_id", spec.SessionID, false)
	q, _ = siftMatch(q, "role", spec.Role, false)

	return q
}

type ConvoUserSpec struct {
	PageSpec
	ModelSpec

	// 登录名 唯一
	Username string `extensions:"x-order=A" form:"username" json:"username"`
	// 昵称
	Nickname string `extensions:"x-order=B" form:"nickname" json:"nickname"`
	// 邮箱
	Email string `extensions:"x-order=C" form:"email" json:"email,omitempty"`
	// 电话
	Phone string `extensions:"x-order=D" form:"phone" json:"phone,omitempty"`
}

func (spec *ConvoUserSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftMatch(q, "username", spec.Username, false)
	q, _ = siftMatch(q, "nickname", spec.Nickname, false)
	q, _ = siftMatch(q, "email", spec.Email, false)
	q, _ = siftMatch(q, "phone", spec.Phone, false)

	return q
}
func (spec *ConvoUserSpec) CanSort(k string) bool {
	switch k {
	case "email":
		return true
	default:
		return spec.ModelSpec.CanSort(k)
	}
}

type ConvoThirdUserSpec struct {
	PageSpec
	ModelSpec

	// 所有人编号
	OwnerID string `extensions:"x-order=A" form:"ownerID" json:"ownerID"`
}

func (spec *ConvoThirdUserSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "owner_id", spec.OwnerID, false)

	return q
}

type ConvoMemorySpec struct {
	PageSpec
	ModelSpec

	// 所有人编号
	OwnerID string `extensions:"x-order=A" form:"ownerID" json:"ownerID"`
	// 关键点
	Key string `extensions:"x-order=B" form:"key" json:"key"`
	// 分类
	Cate string `extensions:"x-order=C" form:"cate" json:"cate"`
	// 分层  (working/short-term/long-term)
	Tier string `extensions:"x-order=D" form:"tier" json:"tier"`
	// 查全部（含内容）
	IsFull bool `extensions:"x-order=E" form:"full" json:"full"`
	// 只查询自己的
	IsOwner bool `extensions:"x-order=F" form:"own" json:"own"`
}

func (spec *ConvoMemorySpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "owner_id", spec.OwnerID, false)
	q, _ = siftMatch(q, "key", spec.Key, false)
	q, _ = siftMatch(q, "cate", spec.Cate, false)
	q, _ = siftMatch(q, "tier", spec.Tier, false)

	return q
}
func (spec *ConvoMemorySpec) CanSort(k string) bool {
	switch k {
	case "key":
		return true
	default:
		return spec.ModelSpec.CanSort(k)
	}
}

type ConvoUsageRecordSpec struct {
	PageSpec
	ModelSpec

	// 会话编号
	SessionID string `extensions:"x-order=A" form:"session" json:"session"`
}

func (spec *ConvoUsageRecordSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "session_id", spec.SessionID, false)

	return q
}

type convoStore struct {
	w *Wrap
}

func (s *convoStore) ListSession(ctx context.Context, spec *ConvoSessionSpec) (data convo.Sessions, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetSession(ctx context.Context, id string) (obj *convo.Session, err error) {
	obj = new(convo.Session)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *convoStore) UpdateSession(ctx context.Context, id string, in convo.SessionSet) error {
	exist := new(convo.Session)
	if err := dbGetWithPKID(ctx, s.w.db, exist, id); err != nil {
		return err
	}
	exist.SetIsUpdate(true)
	exist.SetWith(in)
	dbMetaUp(ctx, s.w.db, exist)
	return dbUpdate(ctx, s.w.db, exist)
}
func (s *convoStore) DeleteSession(ctx context.Context, id string) error {
	obj := new(convo.Session)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *convoStore) ListMessage(ctx context.Context, spec *ConvoMessageSpec) (data convo.Messages, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetMessage(ctx context.Context, id string) (obj *convo.Message, err error) {
	obj = new(convo.Message)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *convoStore) DeleteMessage(ctx context.Context, id string) error {
	obj := new(convo.Message)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *convoStore) ListUser(ctx context.Context, spec *ConvoUserSpec) (data convo.Users, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetUser(ctx context.Context, id string) (obj *convo.User, err error) {
	obj, err = GetUser(ctx, s.w.db, id)

	return
}
func (s *convoStore) CreateUser(ctx context.Context, in convo.UserBasic) (obj *convo.User, err error) {
	err = s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		obj = convo.NewUserWithBasic(in)
		if err = dbBeforeCreateUser(ctx, tx, obj); err != nil {
			return
		}
		if obj.Username == "" {
			err = ErrEmptyKey
			return
		}
		dbMetaUp(ctx, tx, obj)
		err = dbInsert(ctx, tx, obj, "username")
		if err == nil {
			err = dbAfterSaveUser(ctx, tx, obj)
		}
		return err
	})
	return
}
func (s *convoStore) UpdateUser(ctx context.Context, id string, in convo.UserSet) error {
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		exist := new(convo.User)
		if err = dbGetWithPKID(ctx, tx, exist, id); err != nil {
			return err
		}
		exist.SetIsUpdate(true)
		exist.SetWith(in)
		dbMetaUp(ctx, tx, exist)
		if err = dbUpdate(ctx, tx, exist); err != nil {
			return err
		}
		return dbAfterSaveUser(ctx, tx, exist)
	})
}
func (s *convoStore) DeleteUser(ctx context.Context, id string) error {
	obj := new(convo.User)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *convoStore) ListThirdUser(ctx context.Context, spec *ConvoThirdUserSpec) (data convo.ThirdUsers, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetThirdUser(ctx context.Context, id string) (obj *convo.ThirdUser, err error) {
	obj = new(convo.ThirdUser)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *convoStore) DeleteThirdUser(ctx context.Context, id string) error {
	obj := new(convo.ThirdUser)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *convoStore) ListMemory(ctx context.Context, spec *ConvoMemorySpec) (data convo.Memories, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetMemory(ctx context.Context, id string) (obj *convo.Memory, err error) {
	obj = new(convo.Memory)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)
	if err == nil {
		err = s.afterLoadMemory(ctx, obj)
	}
	return
}
func (s *convoStore) CreateMemory(ctx context.Context, in convo.MemoryBasic) (obj *convo.Memory, err error) {
	err = s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		obj = convo.NewMemoryWithBasic(in)
		if err = dbBeforeCreateMemory(ctx, tx, obj); err != nil {
			return
		}
		dbMetaUp(ctx, tx, obj)
		err = dbInsert(ctx, tx, obj)
		return err
	})
	if err == nil {
		err = s.afterCreatedMemory(ctx, obj)
	}
	return
}
func (s *convoStore) UpdateMemory(ctx context.Context, id string, in convo.MemorySet) error {
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		exist := new(convo.Memory)
		if err = dbGetWithPKID(ctx, tx, exist, id); err != nil {
			return err
		}
		exist.SetIsUpdate(true)
		exist.SetWith(in)
		if err = dbBeforeUpdateMemory(ctx, tx, exist); err != nil {
			return err
		}
		dbMetaUp(ctx, tx, exist)
		return dbUpdate(ctx, tx, exist)
	})
}
func (s *convoStore) DeleteMemory(ctx context.Context, id string) error {
	obj := new(convo.Memory)
	if err := dbGetWithPKID(ctx, s.w.db, obj, id); err != nil {
		if errorIs(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		if err = dbBeforeDeleteMemory(ctx, tx, obj); err != nil {
			return
		}
		err = dbDeleteM(ctx, tx, s.w.db.Schema(), s.w.db.SchemaCrap(), obj)
		return
	})
}

func (s *convoStore) ListUsageRecord(ctx context.Context, spec *ConvoUsageRecordSpec) (data convo.UsageRecords, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *convoStore) GetUsageRecord(ctx context.Context, id string) (obj *convo.UsageRecord, err error) {
	obj = new(convo.UsageRecord)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *convoStore) CreateUsageRecord(ctx context.Context, in convo.UsageRecordBasic) (obj *convo.UsageRecord, err error) {
	obj = convo.NewUsageRecordWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	return
}
func (s *convoStore) DeleteUsageRecord(ctx context.Context, id string) error {
	obj := new(convo.UsageRecord)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func GetUser(ctx context.Context, db ormDB, id string, cols ...string) (obj *convo.User, err error) {
	obj = new(convo.User)
	if err = dbGetWith(ctx, db, obj, "username", "ILIKE", id, cols...); err != nil && obj.SetID(id) {
		err = dbGetWithPK(ctx, db, obj, cols...)
	}
	return
}
