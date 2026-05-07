package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/stdlib"
	limitRedis "github.com/ulule/limiter/v3/drivers/store/redis"
	urlquerybinder "github.com/wgarunap/url-query-binder"

	staffio "github.com/liut/staffio-client"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/web/resp"
	"github.com/liut/morign/pkg/web/routes"
)

var handles = []handleIn{}

var queryBinder = urlquerybinder.NewQueryBinder()

type haFunc func(a *api) http.HandlerFunc

type handleIn struct {
	auth   bool
	method string
	path   string
	rid    string
	hafn   haFunc
}

func regHI(auth bool, method string, path string, rid string, hafn haFunc) {
	handles = append(handles, handleIn{auth, method, path, rid, hafn})
}

// nolint
type api struct {
	sto stores.Storage

	llm      llm.Client
	preset   aigc.Preset
	toolreg  *tools.Registry
	toolExec *ToolExecutor

	router chi.Router // 用于平台 HTTP 回调注册
}

func init() {
	queryBinder.SetTag("form")
	routes.Register("api", routes.StrapFunc(strap))
}

func strap(r chi.Router) {
	a := newapi(stores.Sgt())
	a.Strap(r)
}

func newapi(sto stores.Storage) *api {
	preset, _ := stores.LoadPreset()

	if len(settings.Current.BusPrefix) == 0 {
		settings.Current.BusPrefix = staffio.GetPrefix()
	}

	// 初始化 OAuth MCP 配置
	var opts = []tools.RegistryOption{
		tools.WithClientInfo(settings.Current.Name, settings.Version()),
	}

	toolreg := tools.NewRegistry(sto, opts...)
	toolreg.ApplyToolDescriptions(preset.Tools)

	// setup shell sandbox MCP
	if settings.Current.StrataMCPURL != "" {
		var mcpsrv = mcps.NewServerWithBasic(mcps.ServerBasic{
			TransType:  mcps.TransTypeStreamable,
			Name:       "strata",
			URL:        settings.Current.StrataMCPURL,
			HeaderCate: mcps.HeaderCateOwnerID + mcps.HeaderCateSessionID,
		})
		stores.PatchMCPServer(mcpsrv)
		if err := toolreg.AddServer(context.Background(), mcpsrv); err != nil {
			logger().Infow("add strata mcp server fail", "err", err)
		}
	}

	// 加载已激活的 MCP Servers
	if err := toolreg.LoadServers(context.Background(), sto); err != nil {
		logger().Warnw("failed to load MCP servers", "err", err)
	}

	// load LLM Client with interact config
	llmClient, err := stores.NewLLMClient(&settings.Current.Interact)
	if err != nil {
		logger().Fatalw("create llm interact client failed", "err", err)
	}

	return &api{
		sto:      sto,
		llm:      llmClient,
		preset:   preset,
		toolreg:  toolreg,
		toolExec: NewToolExecutor(toolreg),
	}
}

// Strap 注册路由到 chi.Router
func (a *api) Strap(router chi.Router) {

	// init MCP for OAuth2
	if settings.Current.OAuthPathMCP != "" {
		sb := mcps.ServerBasic{
			TransType:  mcps.TransTypeStreamable,
			Name:       settings.Current.GetOAuthName(),
			URL:        staffio.GetPrefix() + settings.Current.OAuthPathMCP,
			HeaderCate: mcps.HeaderCateAuthorization,
		}
		mcpsrv := &mcps.Server{ServerBasic: sb}
		stores.PatchMCPServer(mcpsrv)
		if err := a.toolreg.AddServer(context.Background(), mcpsrv); err != nil {
			logger().Infow("add oauth mcp server fail", "err", err)
		}
	}

	// OAuth2 state store
	staffio.RegisterStateStore(a.sto.State())

	// setup routes
	a.router = router

	// staffio 认证路由
	router.Get(authLoginPath, staffio.LoginHandler)
	router.Get(authLogoutPath, handleLogout)
	router.Method(http.MethodGet, authCallbackPath, (&staffio.CodeCallback{
		OnTokenGot: a.handleTokenGot,
	}).Handler())

	// 限流器初始化
	rate, err := limiter.NewRateFromFormatted(settings.Current.AskRateLimit)
	if err != nil {
		logger().Fatalw("settings failed", "err", err)
	}
	store, err := limitRedis.NewStoreWithOptions(stores.SgtRC(), limiter.StoreOptions{
		Prefix: "chat-lr-",
	})
	if err != nil {
		logger().Fatalw("limiter with redis option failed", "err", err)
	}
	replLK := strings.NewReplacer("/", "")
	instance := limiter.New(store, rate)
	middleware := stdlib.NewMiddleware(instance,
		stdlib.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			logger().Warnw("failed on", "uri", r.RequestURI, "err", err)
		}),
		stdlib.WithKeyGetter(func(r *http.Request) string {
			return fmt.Sprintf("%s:%s",
				limiter.GetIPWithMask(r, limiter.Options{
					TrustForwardHeader: true,
				}).String(),
				replLK.Replace(r.RequestURI))
		}),
	)

	router.Route(settings.Current.APIPrefix, func(r chi.Router) {
		r.Use(OAuthTokenMiddleware(nil)) // OAuth token 注入 context

		r.Get("/ping", ping)

		// 遍历 handles 注册路由
		pr := r.With(routes.AuthMw(false))
		for _, hi := range handles {
			if hi.auth {
				if len(hi.rid) > 0 {
					pr.With(a.authPerm(hi.rid)).Method(hi.method, hi.path, hi.hafn(a))
				} else {
					pr.Method(hi.method, hi.path, hi.hafn(a))
				}
			} else {
				r.Method(hi.method, hi.path, hi.hafn(a))
			}
		}
		// 限流
		limited := pr.With(middleware.Handler)
		limited.Post("/chat", a.postChat)
		limited.Post("/chat-{suffix}", a.postChat)
	})

	// 初始化平台适配器（HTTP webhook 回调等）
	if err := InitChannels(a.router, &a.preset, a.sto, a.llm, a.toolreg); err != nil {
		logger().Warnw("init channels failed", "err", err)
	}

}

func (a *api) authPerm(permID string) func(next http.Handler) http.Handler {
	// TODO:
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !stores.IsKeeper(r.Context()) {
				w.WriteHeader(403)
				logger().Infow("no permission", "id", permID,
					"method", r.Method, "uri", r.RequestURI)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// @Summary API health check
// @Description API health check
// @Produce plain
// @Success 200 {string} pong
// @Router /api/m1/ping [get]
func ping(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong")) //nolint
}

// success 成功响应
func success(w http.ResponseWriter, r *http.Request, result any) {
	resp.Ok(w, r, result)
}

// fail 失败响应
func fail(w http.ResponseWriter, r *http.Request, code int, args ...any) {
	resp.Fail(w, r, code, args...)
}

// dtResult 构建分页结果
func dtResult(data any, total int) *resp.ResultData {
	return &resp.ResultData{
		Data:  data,
		Total: total,
	}
}

// apiFail 失败响应
func apiFail(w http.ResponseWriter, r *http.Request, status int, err any) {
	res := render.M{
		"status": status,
		"error":  err,
	}
	switch ret := err.(type) {
	case error:
		res["message"] = ret.Error()
	case fmt.Stringer:
		res["message"] = ret.String()
	case string, *string, []byte:
		res["message"] = ret
	}
	render.JSON(w, r, res)
}

// apiOk 成功响应
func apiOk(w http.ResponseWriter, r *http.Request, args ...any) {
	res := &ResultData{}
	if len(args) > 0 && args[0] != nil {
		res.Data = args[0]
		if len(args) > 1 {
			if c, ok := args[1].(int); ok {
				res.Total = c
			}
		}
	}

	render.JSON(w, r, res)
}

// nolint
func idResult(id any) *resp.ResultID {
	return &resp.ResultID{ID: id}
}

type Done = resp.Done
type Failure = resp.Failure
type ResultData = resp.ResultData
type ResultID = resp.ResultID

type M = render.M
