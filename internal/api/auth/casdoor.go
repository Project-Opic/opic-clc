// Casdoor OIDC —— 用户工作台 SSO（OPIC-SSO-01）。
//
// 与 zitadel.go 同构（复用 internal/pkg/auth 的泛化 OIDC），配置来自
// config.UserAuth（issuer=casdoor、独立 client_id/secret 与 callback）。
// 会话仍签发本中心本地 JWT（uid 绑定）——能力中心之间账号/会话隔离，
// 自然人多中心账号的收敛在 user-portal 门户解决。
package auth

import (
	"net/http"
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/auth"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"

	"github.com/gin-gonic/gin"
)

func userConf() auth.ScopeConf {
	c := config.Config.UserAuth
	return auth.ScopeConf{Issuer: c.Issuer, ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURL}
}

// CasdoorLoginURL 返回工作台 Casdoor SSO 授权端点（PKCE）。
func CasdoorLoginURL(c *gin.Context) {
	state := auth.NewState()
	verifier := auth.NewState()
	pendingStates.set(state, verifier)
	raw, err := auth.AuthorizeURLWith(c.Request.Context(), userConf(), state, auth.PKCEChallenge(verifier))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, errcode.OK(gin.H{"url": raw}))
}

// CasdoorCallback 授权回调：code 换 token → userinfo → 本地用户 → 签发会话。
func CasdoorCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	verifier, _ := pendingStates.take(state)
	if code == "" || state == "" || verifier == "" {
		c.String(http.StatusBadRequest, "state 校验失败")
		return
	}
	tr, err := auth.ExchangeCodeWith(c.Request.Context(), userConf(), code, verifier)
	if err != nil {
		c.JSON(http.StatusOK, errcode.New(errcode.ErrToken, err.Error(), nil))
		return
	}
	ui, err := auth.FetchUserinfoWith(c.Request.Context(), userConf(), tr.AccessToken)
	if err != nil {
		c.JSON(http.StatusOK, errcode.New(errcode.ErrToken, err.Error(), nil))
		return
	}
	username := ui.PreferredUsername
	if username == "" {
		username = ui.Sub
	}
	uid := ensureUserFromOIDC(ui, config.Config.UserAuth.RoleClaim,
		config.Config.UserAuth.AutoProvision, config.Config.UserAuth.DefaultRoleCodes)
	if uid == 0 {
		c.JSON(http.StatusOK, errcode.New(errcode.ErrToken, "用户未注册且未开启自动 provisioning", nil))
		return
	}
	tk, err := auth.IssueLocalToken(uid, username)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	base := strings.TrimRight(config.Config.Frontend.BaseURL, "/")
	c.Redirect(http.StatusFound, base+"/login#token="+tk)
}
