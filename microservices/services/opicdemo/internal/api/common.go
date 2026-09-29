// Package controller —— REST 控制器层(api/*.py 对应;统一 returnInfo 响应)。
package api

import (
	"os"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"

	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/service"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/auth"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/config"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/ontology"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/errcode"
)

// ---------- 响应助手 ----------

func okJSON(c *gin.Context, data any, msg string) {
	c.JSON(200, errcode.OK(data))
}

func failJSON(c *gin.Context, err error) {
	be := service.AsBizErr(err)
	c.JSON(200, errcode.New(be.Code, be.Msg, nil))
}

func authFail(c *gin.Context, code errcode.ErrCode, msg string) {
	c.JSON(401, errcode.New(code, msg, nil))
}

func bindBody(c *gin.Context) map[string]any {
	var body map[string]any
	_ = c.Bind(&body)
	if body == nil {
		body = map[string]any{}
	}
	return body
}

func pageOf(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 10
	}
	return page, size
}

func pathID(c *gin.Context, name string) int64 {
	id, _ := strconv.ParseInt(c.Param(name), 10, 64)
	return id
}

func userOf(c *gin.Context) service.UserInfo {
	return service.FromCtx(c.Request.Context())
}

// ---------- 认证(auth.py) ----------

// Login 本地账号登录(local 模式)。
func Login(c *gin.Context) {
	body := bindBody(c)
	username := trim(body["username"])
	password := trim(body["password"])
	if username == "" || password == "" {
		c.JSON(200, errcode.New(errcode.ErrLogin, "请输入用户名和密码", nil))
		return
	}
	user, errMsg := service.Login(username, password)
	if errMsg != "" {
		c.JSON(200, errcode.New(errcode.ErrLogin, errMsg, nil))
		return
	}
	token, err := auth.IssueLocalToken(service.Int(user["id"]), service.Str(user["username"]))
	if err != nil {
		failJSON(c, err)
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		failJSON(c, err)
		return
	}
	payload["token"] = token
	service.WriteAudit(nil, service.Int(user["id"]), service.Str(user["username"]), "LOGIN", "")
	okJSON(c, payload, "登录成功")
}

// ZitadelLoginURL zitadel 模式授权地址(PKCE verifier 由服务端暂存 state 映射)。
func ZitadelLoginURL(c *gin.Context) {
	state := auth.NewState()
	verifier := auth.NewState()
	pendingStates.set(state, verifier)
	raw, err := auth.AuthorizeURL(state, auth.PKCEChallenge(verifier))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, gin.H{"url": raw}, "")
}

// ZitadelCallback 授权回调:code 换 token(PKCE verifier 配对)→ userinfo → 本地用户 → 签发会话。
func ZitadelCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	verifier, _ := pendingStates.take(state)
	if code == "" || state == "" || verifier == "" {
		c.String(400, "state 校验失败")
		return
	}
	tr, err := auth.ExchangeCode(c.Request.Context(), code, verifier)
	if err != nil {
		c.JSON(200, errcode.New(errcode.ErrToken, err.Error(), nil))
		return
	}
	ui, err := auth.FetchUserinfo(c.Request.Context(), tr.AccessToken)
	if err != nil {
		c.JSON(200, errcode.New(errcode.ErrToken, err.Error(), nil))
		return
	}
	username := ui.PreferredUsername
	if username == "" {
		username = ui.Sub
	}
	uid := ensureUserFromZitadel(ui)
	if uid == 0 {
		c.JSON(200, errcode.New(errcode.ErrToken, "用户未注册且未开启自动 provisioning", nil))
		return
	}
	token, err := auth.IssueLocalToken(uid, username)
	if err != nil {
		failJSON(c, err)
		return
	}
	// 重定向回前端并携带令牌(#fragment,不经服务器日志;跳转基址由环境变量 OPICDEMO_FRONTEND_URL 承担)
	c.Header("Location", os.Getenv("OPICDEMO_FRONTEND_URL")+"/login#token="+token)
	c.String(302, "login ok")
}

// AuthMode 当前认证模式(前端登录页据此分流)。
func AuthMode(c *gin.Context) {
	mode := config.Config.Auth.Mode
	localAllowed := mode == "local" || config.Config.Auth.AllowLocalLogin
	okJSON(c, gin.H{"mode": mode, "allow_local_login": localAllowed}, "")
}

// Logout 退出。
func Logout(c *gin.Context) {
	u := userOf(c)
	service.WriteAudit(nil, u.ID, u.Username, "LOGOUT", "")
	okJSON(c, nil, "已退出登录")
}

// Info 当前用户信息(user/permissions/menus)。
func Info(c *gin.Context) {
	u := userOf(c)
	user, err := service.GetUser(u.ID)
	if err != nil || user == nil || service.Int(user["status"]) != 1 {
		authFail(c, errcode.ErrLogin, "账号不存在或已禁用")
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, payload, "")
}

// ---------- 元数据(meta.py) ----------

var customerStatuses = []string{"草稿", "待客户经理审批", "待部门总经理审批", "已通过", "已驳回"}

// MetaDictionaries 字典项。
func MetaDictionaries(c *gin.Context) {
	reg := ontology.Load()
	okJSON(c, map[string]any{
		"CUSTOMER_TYPE":  orEmpty(reg.GetDictionaryItems("DICT-CUSTOMER-TYPE", "CUSTOMER_TYPE")),
		"CUSTOMER_LEVEL": orEmpty(reg.GetDictionaryItems("DICT-CUSTOMER-LEVEL", "CUSTOMER_LEVEL")),
	}, "")
}

// MetaCustomerStatus 客户状态清单。
func MetaCustomerStatus(c *gin.Context) {
	okJSON(c, customerStatuses, "")
}

// MetaRules M3 规则清单。
func MetaRules(c *gin.Context) {
	reg := ontology.Load()
	var out []map[string]any
	for _, r := range reg.Rules {
		out = append(out, map[string]any{
			"id":          r["id"],
			"name":        orDefaultStr(r["name"], r["id"]),
			"description": r["description"],
			"expression":  trimSpaces(service.Str(r["expression"])),
			"rule_type":   r["ruleType"],
			"input_params": orEmptyList(r["inputParams"]),
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	okJSON(c, out, "")
}

// ---------- 工具 ----------

func trim(v any) string {
	return trimSpaces(service.Str(v))
}

func trimSpaces(s string) string {
	return trimBoth(s)
}

func trimBoth(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func orDefaultStr(v any, def any) any {
	if v == nil || service.Str(v) == "" {
		return def
	}
	return v
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func orEmptyList(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

var _ = time.Now
