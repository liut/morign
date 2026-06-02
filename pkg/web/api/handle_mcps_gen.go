// This file is generated - Do Not Edit.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/stores"
	binder "github.com/marcsv/go-binder/binder"
)

func init() {
	regHI(true, "GET", "/mcp/servers", "mcp-servers-get", func(a *api) http.HandlerFunc {
		return a.getMCPServers
	})
	regHI(true, "GET", "/mcp/servers/:id", "mcp-servers-id-get", func(a *api) http.HandlerFunc {
		return a.getMCPServer
	})
	regHI(true, "POST", "/mcp/servers", "mcp-servers-post", func(a *api) http.HandlerFunc {
		return a.postMCPServer
	})
	regHI(true, "PUT", "/mcp/servers/:id", "mcp-servers-id-put", func(a *api) http.HandlerFunc {
		return a.putMCPServer
	})
	regHI(true, "DELETE", "/mcp/servers/:id", "mcp-servers-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteMCPServer
	})
}

// @Tags 默认 文档生成
// @ID mcp-servers-get
// @Summary 查询 服务器 列表 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.MCPServerSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=mcps.Servers}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers [get]
func (a *api) getMCPServers(w http.ResponseWriter, r *http.Request) {
	var spec stores.MCPServerSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.MCP().ListServer(ctx, &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, dtResult(data, total))
}

// @Tags 默认 文档生成
// @ID mcp-servers-id-get
// @Summary 获取 服务器 详情 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=mcps.Server}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers/{id} [get]
func (a *api) getMCPServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	obj, err := a.sto.MCP().GetServer(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID mcp-servers-post
// @Summary 录入 服务器 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  body   mcps.ServerBasic  true   "Object"
// @Success 200 {object} Done{result=ResultID}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers [post]
func (a *api) postMCPServer(w http.ResponseWriter, r *http.Request) {
	var in mcps.ServerBasic
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	obj, err := a.sto.MCP().CreateServer(r.Context(), in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, idResult(obj.ID))
}

// @Tags 默认 文档生成
// @ID mcp-servers-id-put
// @Summary 更新 服务器 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Param   query  body   mcps.ServerSet  true   "Object"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers/{id} [put]
func (a *api) putMCPServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in mcps.ServerSet
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	err := a.sto.MCP().UpdateServer(r.Context(), id, in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @ID mcp-servers-id-delete
// @Summary 删除 服务器 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers/{id} [delete]
func (a *api) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.MCP().DeleteServer(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}
