// This file is generated - Do Not Edit.

package stores

import (
	"context"

	"github.com/liut/morign/pkg/models/capability"
)

// type CapCapability = capability.Capability
// type CapCapabilityVector = capability.CapabilityVector
// type SwaggerParam = capability.SwaggerParam
// type SwaggerResponse = capability.SwaggerResponse
// type SwaggerSchema = capability.SwaggerSchema

func init() {
	RegisterModel((*capability.Capability)(nil), (*capability.CapabilityVector)(nil))
}

type CapabilityStore interface {
	CapabilityStoreX

	ListCapability(ctx context.Context, spec *CapCapabilitySpec) (data capability.Capabilities, total int, err error)
	GetCapability(ctx context.Context, id string) (obj *capability.Capability, err error)
	CreateCapability(ctx context.Context, in capability.CapabilityBasic) (obj *capability.Capability, err error)
	UpdateCapability(ctx context.Context, id string, in capability.CapabilitySet) error
	DeleteCapability(ctx context.Context, id string) error

	ListCapabilityVector(ctx context.Context, spec *CapCapabilityVectorSpec) (data capability.CapabilityVectors, total int, err error)
	GetCapabilityVector(ctx context.Context, id string) (obj *capability.CapabilityVector, err error)
	CreateCapabilityVector(ctx context.Context, in capability.CapabilityVectorBasic) (obj *capability.CapabilityVector, err error)
}

type CapCapabilitySpec struct {
	PageSpec
	ModelSpec

	// operationId（可为空，公开接口无此字段）
	OperationID string `extensions:"x-order=A" form:"operationID" json:"operationID"`
	// API 路径，如 /api/accounts/{id}
	Endpoint string `extensions:"x-order=B" form:"endpoint" json:"endpoint"`
	// HTTP 方法 GET/POST/PUT/DELETE 等
	Method string `extensions:"x-order=C" form:"method" json:"method"`
}

func (spec *CapCapabilitySpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftEqual(q, "operation_id", spec.OperationID, false)
	q, _ = siftMatch(q, "endpoint", spec.Endpoint, false)
	q, _ = siftEqual(q, "method", spec.Method, false)

	return q
}

type CapCapabilityVectorSpec struct {
	PageSpec
	ModelSpec

	// 关联的 Capability ID
	CapID string `extensions:"x-order=A" form:"capID" json:"capID"`
}

func (spec *CapCapabilityVectorSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "cap_id", spec.CapID, false)

	return q
}

type capabilityStore struct {
	w *Wrap
}

func (s *capabilityStore) ListCapability(ctx context.Context, spec *CapCapabilitySpec) (data capability.Capabilities, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	if err == nil {
		err = s.afterListCapability(ctx, spec, data)
	}
	return
}
func (s *capabilityStore) GetCapability(ctx context.Context, id string) (obj *capability.Capability, err error) {
	obj = new(capability.Capability)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)
	if err == nil {
		err = s.afterLoadCapability(ctx, obj)
	}
	return
}
func (s *capabilityStore) CreateCapability(ctx context.Context, in capability.CapabilityBasic) (obj *capability.Capability, err error) {
	obj = capability.NewCapabilityWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	if err == nil {
		err = s.afterCreatedCapability(ctx, obj)
	}
	return
}
func (s *capabilityStore) UpdateCapability(ctx context.Context, id string, in capability.CapabilitySet) error {
	exist := new(capability.Capability)
	if err := dbGetWithPKID(ctx, s.w.db, exist, id); err != nil {
		return err
	}
	exist.SetIsUpdate(true)
	exist.SetWith(in)
	dbMetaUp(ctx, s.w.db, exist)
	if err := dbUpdate(ctx, s.w.db, exist); err != nil {
		return err
	}
	return s.afterUpdatedCapability(ctx, exist)
}
func (s *capabilityStore) DeleteCapability(ctx context.Context, id string) error {
	obj := new(capability.Capability)
	if err := dbGetWithPKID(ctx, s.w.db, obj, id); err != nil {
		if errorIs(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		if err = dbBeforeDeleteCapability(ctx, tx, obj); err != nil {
			return
		}
		err = dbDeleteM(ctx, tx, s.w.db.Schema(), s.w.db.SchemaCrap(), obj)
		return
	})
}

func (s *capabilityStore) ListCapabilityVector(ctx context.Context, spec *CapCapabilityVectorSpec) (data capability.CapabilityVectors, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *capabilityStore) GetCapabilityVector(ctx context.Context, id string) (obj *capability.CapabilityVector, err error) {
	obj = new(capability.CapabilityVector)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *capabilityStore) CreateCapabilityVector(ctx context.Context, in capability.CapabilityVectorBasic) (obj *capability.CapabilityVector, err error) {
	obj = capability.NewCapabilityVectorWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	return
}
