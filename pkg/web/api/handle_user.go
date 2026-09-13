package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/render"
	staffio "github.com/liut/staffio-client"
	"github.com/marcsv/go-binder/binder"

	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/settings"
)

func init() {
	// 注册 session/verify 路由
	regHI(false, "GET", "/session", "", func(a *api) http.HandlerFunc {
		return a.handleSession
	})
	regHI(false, "POST", "/session", "", func(a *api) http.HandlerFunc {
		return a.handleSession
	})
	regHI(false, "POST", "/verify", "", func(a *api) http.HandlerFunc {
		return a.handleVerify
	})
	regHI(true, "GET", "/me", "", func(a *api) http.HandlerFunc {
		return handleMe
	})
}

type User = staffio.User
type O2User = staffio.O2User

// vars from staffio
var (
	UserFromContext = staffio.UserFromContext
	ContextWithUser = staffio.ContextWithUser
)

// staffio 认证相关
var (
	authLoginPath    string
	authLogoutPath   string
	authCallbackPath string

	tokenCN = "o_token" // from oauth2 provider
)

func init() {
	// 基于 APIPrefix 动态生成认证路径
	apiPrefix := settings.Current.APIPrefix
	if apiPrefix == "" {
		apiPrefix = "/api"
	}
	authLoginPath = apiPrefix + "/auth/login"
	authLogoutPath = apiPrefix + "/auth/logout"
	authCallbackPath = apiPrefix + "/auth/callback"

	staffio.SetAdminPath(settings.Current.WebAppPath)
}

func buildTokenCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     tokenCN,
		Value:    value,
		HttpOnly: true,
		Path:     settings.Current.CookiePath,
		MaxAge:   settings.Current.CookieMaxAge,
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	siteToken := r.Header.Get(settings.Current.SiteTokenKey)
	if len(siteToken) > 0 {
		_ = stores.DeleteUserToken(r.Context(), siteToken)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   tokenCN,
		Value:  "",
		MaxAge: -1,
		Path:   settings.Current.CookiePath,
	})
	staffio.Signout(w)
}

func (a *api) handleTokenGot(ctx context.Context, w http.ResponseWriter, it *staffio.InfoToken) {

	// TODO: use it.AccessToken directly
	ot := staffio.TokenFromContext(ctx)
	if ot != nil {
		logger().Info("got o2 token", "it", it, "ot", ot)
		http.SetCookie(w, buildTokenCookie(ot.AccessToken))
	} else {
		logger().Info("got info token", "it", it)
	}
	// http.SetCookie(w, buildTokenCookie(it.AccessToken))

	if user, uok := it.GetUser(); uok {
		a.storeUserWith(ctx, user, it.Meta, it.AccessToken)
		if err := stores.SaveUserWithToken(ctx, user, it.AccessToken); err != nil {
			logger().Info("save user to redis failed", "error", err, "uid", user.UID)
		}
	}
}

// storeUserWith saves user to database and associates token via Convo().SyncUserFromOAuth
func (a *api) storeUserWith(ctx context.Context, user stores.IUser, meta staffio.Meta, token string) {
	ctx = staffio.ContextWithUser(ctx, user)
	if wuid := meta.GetStr("wecomUID"); len(wuid) > 0 {
		ctx = stores.ContextWithWecomUID(ctx, wuid)
	}
	_ = a.sto.Convo().SyncUserFromOAuth(ctx, user)
	_ = stores.SaveTokenWithUser(ctx, user.GetOID(), token)
}

// @Tags 用户 认证
// @Summary 获取当前用户
// @Accept json
// @Produce json
// @Success 200 {object} Done{result=User}
// @Failure 401 {object} Failure "未登录"
// @Router /api/me [get]
func handleMe(w http.ResponseWriter, r *http.Request) {
	if !settings.Current.AuthRequired {
		apiOk(w, r, &User{})
		return
	}
	if user, ok := UserFromContext(r.Context()); ok {
		apiOk(w, r, user)
	} else {
		apiFail(w, r, 401, "not login")
	}
}

type respSession struct {
	Status string `json:"status"`
	Data   struct {
		Auth  bool   `json:"auth"`            // need auth
		User  *User  `json:"user,omitempty"`  // logined user
		URI   string `json:"uri,omitempty"`   // uri of auth
		Model string `json:"model,omitempty"` // for chatgpt-web
		Token string `json:"token,omitempty"` // token from oauth2 provider
		Keeper bool `json:"keeper"` // user is keeper
	} `json:"data"`
}

type oAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`

	Meta map[string]any `json:"meta,omitempty"`
}

func (a *oAccount) toUser() *O2User {
	if a != nil {
		user := new(O2User)
		user.OID = a.ID
		user.UID = a.Name
		user.Name = a.Nickname
		user.Avatar = a.Avatar
		user.Email = a.Email
		user.Phone = a.Phone
		return user
	}
	return nil
}

type oRes struct {
	Message string    `json:"message"`
	Status  int       `json:"status"`
	Result  *oAccount `json:"result"`
	Extra   *struct {
		Avatar string `json:"avatar"`
	} `json:"extra"`
}

func (or *oRes) getUser() *O2User {
	u := or.Result.toUser()
	if u != nil {
		if or.Extra != nil && len(or.Extra.Avatar) > 0 {
			u.Avatar = or.Extra.Avatar
		}
	}
	return u
}

// syncUserToCache syncs user to db and saves to Redis, then signs in
func (a *api) syncUserToCache(ctx context.Context, user *O2User, token string, w http.ResponseWriter) {
	if err := stores.SaveUserWithToken(ctx, user, token); err != nil {
		logger().Info("save user to redis failed", "error", err, "uid", user.UID)
	}
	user.Refresh()
	staffio.Signin(user, w)
}

// fillUserResponse populates user data into response
func fillUserResponse(res *respSession, user *User, token string) {
	user.Avatar = patchImageURI(user.Avatar, staffio.GetPrefix())
	res.Data.User = user
	res.Data.Keeper = stores.UserIsKeeper(user)
	if token != "" {
		res.Data.Token = token
	}
}

// @Tags 用户 认证
// @Summary 获取会话信息
// @Description 获取当前登录状态和用户信息 for github.com/liut/calisyn
// @Accept json
// @Produce json
// @Success 200 {object} Done{result=respSession}
// @Router /api/session [get]
func (a *api) handleSession(w http.ResponseWriter, r *http.Request) {
	var res respSession
	res.Status = "Success"

	if !settings.Current.AuthRequired {
		res.Data.Auth = len(settings.Current.AuthSecret) > 0
		render.JSON(w, r, &res)
		return
	}

	ctx := r.Context()
	var accessToken string
	if ct, err := r.Cookie(tokenCN); err == nil {
		accessToken = ct.Value
	}
	siteToken := r.Header.Get(settings.Current.SiteTokenKey)

	var user *User
	var err error
	if len(siteToken) > 0 {
		user, err = stores.LoadUserFromToken(ctx, siteToken)
	} else {
		user, err = staffio.UserFromRequest(r)
	}

	logger().Debug("handle session", "user", user, "err", err, "siteToken", siteToken)

	if err == nil {
		fillUserResponse(&res, user, accessToken)
		render.JSON(w, r, &res)
		return
	}

	// No valid session, try OAuth token
	if siteToken == "" {
		res.Data.Auth = true
		res.Data.URI = authLoginPath
		render.JSON(w, r, &res)
		return
	}

	// Try OAuth info endpoint
	if o2u := a.trySyncOAuthUser(ctx, accessToken, siteToken); o2u != nil {
		a.syncUserToCache(ctx, o2u, siteToken, w)
		fillUserResponse(&res, &o2u.User, "")
		render.JSON(w, r, &res)
		return
	}

	res.Data.Auth = true
	res.Data.URI = authLoginPath
	render.JSON(w, r, &res)
}

// trySyncOAuthUser attempts to get user from OAuth token, returns nil on failure
func (a *api) trySyncOAuthUser(ctx context.Context, accessToken, siteToken string) *O2User {
	// Try staffio OAuth endpoint first (uses accessToken from cookie)
	if accessToken != "" {
		it, err := staffio.RequestInfoToken(ctx, &staffio.O2Token{
			AccessToken: accessToken,
			TokenType:   "Bearer",
		})
		if err == nil {
			if user, ok := it.GetUser(); ok {
				a.storeUserWith(ctx, user, it.Meta, it.AccessToken)
				return user
			}
		}
		logger().Info("request infn fail", "err", err, "token", accessToken)
	}

	// Try custom OAuth me endpoint (uses siteToken from header)
	uriMe := settings.Current.SitePathMe
	if uriMe == "" || siteToken == "" {
		logger().Info("empty uri or token", "uriMe", uriMe, "token", siteToken)
		return nil
	}
	uriMe = staffio.FixURI(staffio.GetPrefix(), uriMe)

	var ores oRes
	if err := staffio.RequestWith(ctx, uriMe, &staffio.O2Token{
		AccessToken: siteToken,
		TokenType:   "Bearer",
	}, &ores); err != nil {
		logger().Info("request oauth me fail", "err", err, "uri", uriMe)
		return nil
	}
	if ores.Status > 0 {
		logger().Info("got account fail", "ores", &ores)
		return nil
	}
	logger().Info("got account ok", "acc", ores.Result)

	if user := ores.getUser(); user != nil {
		a.storeUserWith(ctx, user, ores.Result.Meta, siteToken)
		return user
	}

	return nil
}

type verifyReq struct {
	Token string `json:"token"`
}

// @Tags 用户 认证
// @Summary 验证Token
// @Description 验证用户Token for github.com/Chanzhaoyu/chatgpt-web
// @Accept json
// @Produce json
// @Param token body verifyReq true "验证Token"
// @Success 200 {object} Done{result=M}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "Token验证失败"
// @Router /api/verify [post]
func (a *api) handleVerify(w http.ResponseWriter, r *http.Request) {
	var param verifyReq
	if err := binder.BindBody(r, &param); err != nil {
		apiFail(w, r, 400, err)
		return
	}
	if param.Token != settings.Current.AuthSecret {
		apiFail(w, r, 401, "mismatch token")
	}
	// user := new(User)
	// if err := user.Decode(param.Token); err != nil {
	// 	apiFail(w, r, 401, err)
	// 	return
	// }

	render.JSON(w, r, M{"status": "Success"})
}

// patchImageURI 修复图片 URI
func patchImageURI(uri, prefix string) string {
	if uri == "" {
		return ""
	}
	if prefix == "" {
		return uri
	}
	// 如果已经是完整 URL，直接返回
	if len(uri) > 7 && (uri[:7] == "http://" || uri[:8] == "https://") {
		return uri
	}
	// 如果是相对路径，添加 prefix
	if strings.HasPrefix(uri, "/") {
		return prefix + uri
	}
	if !strings.HasPrefix(uri, "/images/") || strings.HasPrefix(uri, "avatar/") {
		return prefix + "/images/" + uri
	}
	return prefix + "/" + uri
}
