// Package auth —— 认证控制器(登录/登出/当前用户/认证模式;契约与旧版一致)。
package auth

import (
	"time"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/auth"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
	"gitcode.com/opic-ontology/opic-techbase/pkg/token"
)

// Login 本地账号登录(local 模式)。
func Login(c *gin.Context) {
	body := common.BindJSON(c)
	username := common.Trim(body["username"])
	password := common.Trim(body["password"])
	if username == "" || password == "" {
		c.JSON(200, errcode.New(errcode.ErrLogin, "请输入用户名和密码", nil))
		return
	}
	user, errMsg := service.Login(username, password)
	if errMsg != "" {
		c.JSON(200, errcode.New(errcode.ErrLogin, errMsg, nil))
		return
	}
	tk, err := auth.IssueLocalToken(service.Int(user["id"]), service.Str(user["username"]))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	payload["token"] = tk
	service.WriteAudit(nil, service.Int(user["id"]), service.Str(user["username"]), "LOGIN", "")
	common.OKJSON(c, payload, "登录成功")
}

// Logout 退出(吊销当前会话令牌;Redis 未启用时仅审计,行为与旧版一致)。
func Logout(c *gin.Context) {
	u := common.UserOf(c)
	if claims, err := auth.ParseLocalToken(common.BearerToken(c)); err == nil && claims.ID != "" && claims.ExpiresAt != nil {
		if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
			_ = token.Revoke(claims.ID, ttl)
		}
	}
	service.WriteAudit(nil, u.ID, u.Username, "LOGOUT", "")
	common.OKJSON(c, nil, "已退出登录")
}

// Info 当前用户信息(user/permissions/menus)。
func Info(c *gin.Context) {
	u := common.UserOf(c)
	user, err := service.GetUser(u.ID)
	if err != nil || user == nil || service.Int(user["status"]) != 1 {
		common.AuthFail(c, errcode.ErrLogin, "账号不存在或已禁用")
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, payload, "")
}

// AuthMode 当前认证模式(前端登录页据此分流)。
func AuthMode(c *gin.Context) {
	mode := authMode()
	localAllowed := mode == "local" || allowLocalLogin()
	c.JSON(200, errcode.OK(gin.H{"mode": mode, "allow_local_login": localAllowed}))
}
