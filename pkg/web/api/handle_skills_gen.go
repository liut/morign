// This file is generated - Do Not Edit.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/services/stores"
	binder "github.com/marcsv/go-binder/binder"
)

func init() {
	regHI(true, "GET", "/admin/skills", "admin-skills-get", func(a *api) http.HandlerFunc {
		return a.getSkills
	})
	regHI(true, "GET", "/admin/skills/{id}", "admin-skills-id-get", func(a *api) http.HandlerFunc {
		return a.getSkill
	})
	regHI(true, "POST", "/admin/skills", "admin-skills-post", func(a *api) http.HandlerFunc {
		return a.postSkill
	})
	regHI(true, "PUT", "/admin/skills/{id}", "admin-skills-id-put", func(a *api) http.HandlerFunc {
		return a.putSkill
	})
	regHI(true, "DELETE", "/admin/skills/{id}", "admin-skills-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteSkill
	})
}

// @Tags 默认 文档生成
// @ID admin-skills-get
// @Summary 查询 技能 列表 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.SkillSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=skills.Skills}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/admin/skills [get]
func (a *api) getSkills(w http.ResponseWriter, r *http.Request) {
	var spec stores.SkillSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Skill().ListSkill(ctx, &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	if total == 0 {
		total = len(data)
	}
	success(w, r, dtResult(data, total))
}

// @Tags 默认 文档生成
// @ID admin-skills-id-get
// @Summary 获取 技能 详情 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=skills.Skill}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/admin/skills/{id} [get]
func (a *api) getSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	obj, err := a.sto.Skill().GetSkill(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID admin-skills-post
// @Summary 录入 技能 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  body   skills.SkillBasic  true   "Object"
// @Success 200 {object} Done{result=ResultID}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/admin/skills [post]
func (a *api) postSkill(w http.ResponseWriter, r *http.Request) {
	var in skills.SkillBasic
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	obj, err := a.sto.Skill().CreateSkill(r.Context(), in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, idResult(obj.ID))
}

// @Tags 默认 文档生成
// @ID admin-skills-id-put
// @Summary 更新 技能 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Param   query  body   skills.SkillSet  true   "Object"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/admin/skills/{id} [put]
func (a *api) putSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in skills.SkillSet
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	err := a.sto.Skill().UpdateSkill(r.Context(), id, in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @ID admin-skills-id-delete
// @Summary 删除 技能 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/admin/skills/{id} [delete]
func (a *api) deleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Skill().DeleteSkill(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}
