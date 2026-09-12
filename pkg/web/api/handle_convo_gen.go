// This file is generated - Do Not Edit.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/stores"
)

func init() {
	regHI(true, "GET", "/convo/sessions", "", func(a *api) http.HandlerFunc {
		return a.getConvoSessions
	})
	regHI(true, "GET", "/convo/sessions/{id}", "", func(a *api) http.HandlerFunc {
		return a.getConvoSession
	})
	regHI(true, "DELETE", "/convo/sessions/{id}", "convo-sessions-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteConvoSession
	})
	regHI(true, "GET", "/convo/messages", "", func(a *api) http.HandlerFunc {
		return a.getConvoMessages
	})
	regHI(true, "GET", "/convo/messages/{id}", "", func(a *api) http.HandlerFunc {
		return a.getConvoMessage
	})
	regHI(true, "DELETE", "/convo/messages/{id}", "convo-messages-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteConvoMessage
	})
	regHI(true, "GET", "/convo/users", "", func(a *api) http.HandlerFunc {
		return a.getConvoUsers
	})
	regHI(true, "GET", "/convo/users/{id}", "", func(a *api) http.HandlerFunc {
		return a.getConvoUser
	})
	regHI(true, "DELETE", "/convo/users/{id}", "convo-users-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteConvoUser
	})
	regHI(true, "GET", "/convo/usagerecords", "", func(a *api) http.HandlerFunc {
		return a.getConvoUsageRecords
	})
	regHI(true, "GET", "/convo/usagerecords/{id}", "", func(a *api) http.HandlerFunc {
		return a.getConvoUsageRecord
	})
	regHI(true, "DELETE", "/convo/usagerecords/{id}", "convo-usagerecords-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteConvoUsageRecord
	})
}

// @Tags 默认 文档生成
// @Summary 查询 会话 列表
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.ConvoSessionSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=convo.Sessions}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/sessions [get]
func (a *api) getConvoSessions(w http.ResponseWriter, r *http.Request) {
	var spec stores.ConvoSessionSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Convo().ListSession(ctx, &spec)
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
// @Summary 获取 会话 详情
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=convo.Session}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/sessions/{id} [get]
func (a *api) getConvoSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *convo.Session
	var err error
	obj, err = a.sto.Convo().GetSession(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID convo-sessions-id-delete
// @Summary 删除 会话 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/sessions/{id} [delete]
func (a *api) deleteConvoSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Convo().DeleteSession(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @Summary 查询 消息 列表
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.ConvoMessageSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=convo.Messages}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/messages [get]
func (a *api) getConvoMessages(w http.ResponseWriter, r *http.Request) {
	var spec stores.ConvoMessageSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Convo().ListMessage(ctx, &spec)
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
// @Summary 获取 消息 详情
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=convo.Message}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/messages/{id} [get]
func (a *api) getConvoMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *convo.Message
	var err error
	obj, err = a.sto.Convo().GetMessage(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID convo-messages-id-delete
// @Summary 删除 消息 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/messages/{id} [delete]
func (a *api) deleteConvoMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Convo().DeleteMessage(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @Description <sortable>id,created,updated,email</sortable>
// @Summary 查询 用户 列表
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.ConvoUserSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=convo.Users}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/users [get]
func (a *api) getConvoUsers(w http.ResponseWriter, r *http.Request) {
	var spec stores.ConvoUserSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Convo().ListUser(ctx, &spec)
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
// @Summary 获取 用户 详情
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=convo.User}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/users/{id} [get]
func (a *api) getConvoUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *convo.User
	var err error
	obj, err = a.sto.Convo().GetUser(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID convo-users-id-delete
// @Summary 删除 用户 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/users/{id} [delete]
func (a *api) deleteConvoUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Convo().DeleteUser(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @Summary 查询 使用情况 列表
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.ConvoUsageRecordSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=convo.UsageRecords}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/usagerecords [get]
func (a *api) getConvoUsageRecords(w http.ResponseWriter, r *http.Request) {
	var spec stores.ConvoUsageRecordSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Convo().ListUsageRecord(ctx, &spec)
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
// @Summary 获取 使用情况 详情
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=convo.UsageRecord}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/usagerecords/{id} [get]
func (a *api) getConvoUsageRecord(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *convo.UsageRecord
	var err error
	obj, err = a.sto.Convo().GetUsageRecord(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID convo-usagerecords-id-delete
// @Summary 删除 使用情况 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/convo/usagerecords/{id} [delete]
func (a *api) deleteConvoUsageRecord(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Convo().DeleteUsageRecord(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}
