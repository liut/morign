package api

import (
	"errors"
	"net/http"

	"github.com/cupogo/andvari/models/oid"
	"github.com/cupogo/querybind"
	"github.com/go-chi/chi/v5"
	binder "github.com/marcsv/go-binder/binder"

	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/services/stores"
)

var errSkillNeedLogin = errors.New("need login")

type skillCreateIn struct {
	skills.SkillBasic
	Files map[string]string `json:"files,omitempty"`
}

type skillUpdateIn struct {
	skills.SkillSet
	Files map[string]string `json:"files,omitempty"`
}

type skillDetail struct {
	skills.Skill
	Files skills.Files `json:"files"`
}

func init() {
	regHI(true, "GET", "/skills", "", func(a *api) http.HandlerFunc {
		return a.listSkills
	})
	regHI(true, "GET", "/skills/{name}", "", func(a *api) http.HandlerFunc {
		return a.getSkillByName
	})
	regHI(true, "POST", "/skills", "", func(a *api) http.HandlerFunc {
		return a.createSkill
	})
	regHI(true, "PUT", "/skills/{name}", "", func(a *api) http.HandlerFunc {
		return a.updateSkill
	})
	regHI(true, "DELETE", "/skills/{name}", "", func(a *api) http.HandlerFunc {
		return a.deleteSkillByName
	})
}

// listSkills 返回当前用户可见范围的技能元数据（不含全文）。
// @Tags 技能
// @Summary 查询 可见技能列表（元数据）
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param query query stores.SkillSpec true "Object"
// @Success 200 {object} Done{result=ResultData{data=skills.Skills}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/skills [get]
func (a *api) listSkills(w http.ResponseWriter, r *http.Request) {
	var spec stores.SkillSpec
	if err := querybind.Bind(&spec, r.URL.Query()); err != nil {
		fail(w, r, 400, err)
		return
	}
	data, total, err := a.sto.Skill().ListVisibleMetadata(r.Context(), &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}
	success(w, r, dtResult(data, total))
}

// getSkillByName 返回可见技能详情（含全文与资源文件清单，不含文件内容）。
// @Tags 技能
// @Summary 获取 技能详情
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param name path string true "技能名"
// @Success 200 {object} Done{result=skillDetail}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/skills/{name} [get]
func (a *api) getSkillByName(w http.ResponseWriter, r *http.Request) {
	obj, err := a.sto.Skill().LoadForName(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		fail(w, r, 404, err)
		return
	}
	files, err := a.sto.Skill().ListFileNames(r.Context(), obj.Name)
	if err != nil {
		fail(w, r, 503, err)
		return
	}
	success(w, r, skillDetail{Skill: *obj, Files: files})
}

// createSkill 创建当前用户自己的技能：owner 强制、Channel 默认私有（不投放）、
// 校验 name 格式与 SKILL.md frontmatter、name 全局唯一。
// @Tags 技能
// @Summary 创建 自己的技能
// @Accept json,mpfd
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param query body skillCreateIn true "Object"
// @Success 200 {object} Done{result=ResultID}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/skills [post]
func (a *api) createSkill(w http.ResponseWriter, r *http.Request) {
	user, ok := stores.UserFromContext(r.Context())
	if !ok {
		fail(w, r, 401, errSkillNeedLogin)
		return
	}
	var in skillCreateIn
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}
	if !skills.ValidName(in.Name) {
		fail(w, r, 400, skills.ErrInvalidName)
		return
	}
	if !skills.ValidDescription(in.Description) {
		fail(w, r, 400, skills.ErrDescriptionLong)
		return
	}
	if err := skills.ValidateFrontmatter(in.Content, in.Name, in.Description); err != nil {
		fail(w, r, 400, err)
		return
	}
	in.Owner = oid.Cast(user.OID)
	obj, err := a.sto.Skill().CreateSkillWithFiles(r.Context(), in.SkillBasic, in.Files)
	if err != nil {
		if errors.Is(err, stores.ErrDuplicate) {
			fail(w, r, 400, errors.New("skill name already exists"))
			return
		}
		fail(w, r, 503, err)
		return
	}
	success(w, r, idResult(obj.ID))
}

// updateSkill 仅允许更新自己的技能；name 不可变更。
// @Tags 技能
// @Summary 更新 自己的技能
// @Accept json,mpfd
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param name path string true "技能名"
// @Param query body skillUpdateIn true "Object"
// @Success 200 {object} Done{result=ResultID}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/skills/{name} [put]
func (a *api) updateSkill(w http.ResponseWriter, r *http.Request) {
	user, ok := stores.UserFromContext(r.Context())
	if !ok {
		fail(w, r, 401, errSkillNeedLogin)
		return
	}
	name := chi.URLParam(r, "name")
	var in skillUpdateIn
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}
	obj, err := a.sto.Skill().GetSkill(r.Context(), name)
	if err != nil {
		fail(w, r, 404, err)
		return
	}
	if obj.Owner != oid.Cast(user.OID) {
		fail(w, r, 403, errors.New("permission denied"))
		return
	}
	if in.Name != nil && (*in.Name != name || !skills.ValidName(*in.Name)) {
		fail(w, r, 400, skills.ErrInvalidName)
		return
	}
	if in.Description != nil && !skills.ValidDescription(*in.Description) {
		fail(w, r, 400, skills.ErrDescriptionLong)
		return
	}
	if in.Content != nil {
		desc := obj.Description
		if in.Description != nil {
			desc = *in.Description
		}
		if err := skills.ValidateFrontmatter(*in.Content, name, desc); err != nil {
			fail(w, r, 400, err)
			return
		}
	}
	if err := a.sto.Skill().UpdateSkillWithFiles(r.Context(), obj.ID.String(), in.SkillSet, in.Files); err != nil {
		fail(w, r, 503, err)
		return
	}
	success(w, r, idResult(obj.ID))
}

// deleteSkillByName 仅允许删除自己的技能。
// @Tags 技能
// @Summary 删除 自己的技能
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param name path string true "技能名"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/skills/{name} [delete]
func (a *api) deleteSkillByName(w http.ResponseWriter, r *http.Request) {
	user, ok := stores.UserFromContext(r.Context())
	if !ok {
		fail(w, r, 401, errSkillNeedLogin)
		return
	}
	obj, err := a.sto.Skill().GetSkill(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		fail(w, r, 404, err)
		return
	}
	if obj.Owner != oid.Cast(user.OID) {
		fail(w, r, 403, errors.New("permission denied"))
		return
	}
	if err := a.sto.Skill().DeleteSkill(r.Context(), obj.ID.String()); err != nil {
		fail(w, r, 503, err)
		return
	}
	success(w, r, "ok")
}
