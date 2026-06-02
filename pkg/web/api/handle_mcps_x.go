package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/mcps"
)

func init() {
	regHI(true, "PUT", "/mcp/servers/{id}/activate", "mcp-servers-id-activate", func(a *api) http.HandlerFunc {
		return a.putMCPServerActivate
	})
	regHI(true, "PUT", "/mcp/servers/{id}/deactivate", "mcp-servers-id-deactivate", func(a *api) http.HandlerFunc {
		return a.putMCPServerDeactivate
	})
}

// @Tags MCP
// @ID mcp-servers-id-activate
// @Summary 激活服务器 🔑
// @Description 将 MCP Server 添加到工具注册表
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param id path string true "编号"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers/{id}/activate [put]
func (a *api) putMCPServerActivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// 获取 Server 对象
	server, err := a.sto.MCP().GetServer(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}
	if server == nil {
		fail(w, r, 404, "server not found")
		return
	}

	// 先更新 IsActive 为 true
	isActive := true
	status := mcps.StatusConnecting
	if err := a.sto.MCP().UpdateServer(r.Context(), id, mcps.ServerSet{
		IsActive: &isActive,
		Status:   &status,
	}); err != nil {
		fail(w, r, 503, err)
		return
	}

	// 再调用 AddServer 添加到工具注册表
	if err := a.toolreg.AddServer(r.Context(), &server.ServerBasic); err != nil {
		fail(w, r, 503, err)
		return
	}

	// 更新 Server 状态为 connected
	if server.Status != mcps.StatusConnected {
		status := mcps.StatusConnected
		if err := a.sto.MCP().UpdateServer(r.Context(), id, mcps.ServerSet{
			Status: &status,
		}); err != nil {
			fail(w, r, 503, err)
			return
		}
	}

	success(w, r, "ok")
}

// @Tags MCP
// @ID mcp-servers-id-deactivate
// @Summary 停用服务器 🔑
// @Description 从工具注册表中移除 MCP Server
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param id path string true "编号"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/mcp/servers/{id}/deactivate [put]
func (a *api) putMCPServerDeactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// 获取 Server 对象
	server, err := a.sto.MCP().GetServer(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}
	if server == nil {
		fail(w, r, 404, "server not found")
		return
	}

	// 从工具注册表中移除
	if err := a.toolreg.RemoveServer(server.Name); err != nil {
		fail(w, r, 503, err)
		return
	}

	// 更新 IsActive 为 false
	isActive := false
	// 更新 Server 状态为 disconnected
	status := mcps.StatusDisconnected
	if err := a.sto.MCP().UpdateServer(r.Context(), id, mcps.ServerSet{
		IsActive: &isActive,
		Status:   &status,
	}); err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}
