// This file is generated - Do Not Edit.

package stores

import (
	"context"

	"github.com/liut/morign/pkg/models/skills"
)

// type File = skills.File
// type Skill = skills.Skill

func init() {
	RegisterModel((*skills.Skill)(nil), (*skills.File)(nil))
}

type SkillStore interface {
	SkillStoreX

	ListSkill(ctx context.Context, spec *SkillSpec) (data skills.Skills, total int, err error)
	GetSkill(ctx context.Context, id string) (obj *skills.Skill, err error)
	CreateSkill(ctx context.Context, in skills.SkillBasic) (obj *skills.Skill, err error)
	UpdateSkill(ctx context.Context, id string, in skills.SkillSet) error
	DeleteSkill(ctx context.Context, id string) error

	ListFile(ctx context.Context, spec *FileSpec) (data skills.Files, total int, err error)
}

type SkillSpec struct {
	PageSpec
	ModelSpec

	// 技能名 全局唯一（小写字母数字连字符）
	Name string `extensions:"x-order=A" form:"name" json:"name"`
	// 可用频道 位掩码（0=未投放，仅创建者可见） (支持混合解码)
	//  * `web`
	//  * `wecom` - 企业微信
	//  * `feishu` - 飞书
	Channel string `extensions:"x-order=B" form:"channel" json:"channel" swaggertype:"string"`
	// 创建者 uid
	Owner string `extensions:"x-order=C" form:"owner" json:"owner"`
	// 仅查看可见的
	VisibleOnly bool `extensions:"x-order=D" form:"visible" json:"visible"`
}

func (spec *SkillSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftMatch(q, "name", spec.Name, false)
	if len(spec.Channel) > 0 {
		var v skills.Channel
		if err := v.Decode(spec.Channel); err == nil {
			q = q.Where("?TableAlias.channel = ?", v)
		}
	}
	q, _ = siftOID(q, "owner", spec.Owner, false)

	return q
}

type FileSpec struct {
	PageSpec
	ModelSpec

	// 所属技能 id
	SkillID string `extensions:"x-order=A" form:"skillId" json:"skillId"`
	// 资源文件相对路径 技能内唯一
	Path string `extensions:"x-order=B" form:"path" json:"path"`
}

func (spec *FileSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "skill_id", spec.SkillID, false)
	q, _ = siftEqual(q, "path", spec.Path, false)

	return q
}

type skillStore struct {
	w *Wrap
}

func (s *skillStore) ListSkill(ctx context.Context, spec *SkillSpec) (data skills.Skills, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *skillStore) GetSkill(ctx context.Context, id string) (obj *skills.Skill, err error) {
	obj = new(skills.Skill)
	if err = dbGetWith(ctx, s.w.db, obj, "name", "=", id); err != nil && obj.SetID(id) {
		err = dbGetWithPK(ctx, s.w.db, obj)
	}

	return
}
func (s *skillStore) CreateSkill(ctx context.Context, in skills.SkillBasic) (obj *skills.Skill, err error) {
	err = s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		obj = skills.NewSkillWithBasic(in)
		if err = dbBeforeCreateSkill(ctx, tx, obj); err != nil {
			return
		}
		if obj.Name == "" {
			err = ErrEmptyKey
			return
		}
		dbMetaUp(ctx, tx, obj)
		err = dbInsert(ctx, tx, obj, "name")
		return err
	})
	return
}
func (s *skillStore) UpdateSkill(ctx context.Context, id string, in skills.SkillSet) error {
	exist := new(skills.Skill)
	if err := dbGetWithPKID(ctx, s.w.db, exist, id); err != nil {
		return err
	}
	exist.SetIsUpdate(true)
	exist.SetWith(in)
	dbMetaUp(ctx, s.w.db, exist)
	return dbUpdate(ctx, s.w.db, exist)
}
func (s *skillStore) DeleteSkill(ctx context.Context, id string) error {
	obj := new(skills.Skill)
	if err := dbGetWithPKID(ctx, s.w.db, obj, id); err != nil {
		if errorIs(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		if err = dbBeforeDeleteSkill(ctx, tx, obj); err != nil {
			return
		}
		err = dbDeleteM(ctx, tx, s.w.db.Schema(), s.w.db.SchemaCrap(), obj)
		return
	})
}

func (s *skillStore) ListFile(ctx context.Context, spec *FileSpec) (data skills.Files, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
