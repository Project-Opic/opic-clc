// Package middleware —— hertz 中间件:认证(local/zitadel 双模式)+ 权限 + CORS。
package middleware

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang-jwt/jwt/v5"

	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/apiserver/service"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/auth"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/config"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/pkg/errcode"
)

// LoginRequired 认证中间件。
//
// 令牌兼容顺序:
//  1. 本会话 HS256 令牌(/auth/login 或 zitadel 模式 /auth/callback 签发);
//  2. zitadel 模式下,ZITADEL 原生 access token(RS256/JWKS 验签 + 角色映射 + 自动 provisioning)。
func LoginRequired() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		authHeader := string(c.GetHeader("Authorization"))
		if !strings.HasPrefix(authHeader, "Bearer ") {
			abort(c, errcode.ErrUnAuth, "未登录或登录已失效")
			return
		}
		tokenStr := strings.TrimSpace(authHeader[len("Bearer "):])

		// ① 本会话令牌
		if claims, err := auth.ParseLocalToken(tokenStr); err == nil {
			ctx = context.WithValue(ctx, service.CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, service.CtxUsername, claims.Username)
			c.Next(ctx)
			return
		}

		// ② zitadel 原生令牌(仅 zitadel 模式)
		if config.Config.Auth.Mode != "zitadel" {
			abort(c, errcode.ErrUnAuth, "登录已过期，请重新登录")
			return
		}
		claims, err := auth.ValidateZitadelToken(ctx, tokenStr)
		if err != nil {
			abort(c, errcode.ErrToken, "无效的访问令牌")
			return
		}
		sub, _ := claims["sub"].(string)
		username, _ := claims["preferred_username"].(string)
		if username == "" {
			username = sub
		}
		uid := ensureZitadelUser(ctx, sub, username, extractClaimRoles(claims))
		if uid == 0 {
			abort(c, errcode.ErrToken, "用户未注册且未开启自动 provisioning")
			return
		}
		ctx = context.WithValue(ctx, service.CtxUserID, uid)
		ctx = context.WithValue(ctx, service.CtxUsername, username)
		c.Next(ctx)
	}
}

// ensureZitadelUser 按 ZITADEL 身份取/建本地用户并映射角色(claim 角色 + 默认角色)。
// 已存在用户且本次声明携带角色时,重新同步角色(角色变更在下次登录生效)。
func ensureZitadelUser(ctx context.Context, sub, username string, claimRoles []string) int64 {
	user, err := service.GetUserByUsername(username)
	if err == nil && user != nil {
		uid := service.Int(user["id"])
		if len(claimRoles) > 0 {
			_ = service.SyncUserRolesByCodes(uid, claimRoles)
		}
		return uid
	}
	cfg := config.Config.Auth
	if !cfg.AutoProvision {
		return 0
	}
	data := map[string]any{
		"username":   username,
		"password":   randomPlaceholder(),
		"real_name":  username,
		"actor_type": "HUMAN",
		"status":     1,
	}
	uid, err := service.CreateUser(data)
	if err != nil {
		return 0
	}
	var roleIDs []any
	seen := map[string]bool{}
	// ① ZITADEL 角色声明 → 本地同名角色
	for _, code := range claimRoles {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		if role, err := service.GetRoleByCode(code); err == nil && role != nil {
			roleIDs = append(roleIDs, service.Int(role["id"]))
		}
	}
	// ② 默认角色兜底(仅新用户)
	if len(roleIDs) == 0 {
		for _, code := range strings.Split(cfg.DefaultRoleCodes, ",") {
			code = strings.TrimSpace(code)
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true
			if role, err := service.GetRoleByCode(code); err == nil && role != nil {
				roleIDs = append(roleIDs, service.Int(role["id"]))
			}
		}
	}
	if len(roleIDs) > 0 {
		_ = service.AssignRoles(uid, roleIDs)
	}
	return uid
}

func randomPlaceholder() string {
	// zitadel 模式下本地密码无意义,置随机占位(不可登录明文)
	return "zitadel-managed:" + auth.NewState()
}

// extractClaimRoles 从 token claims 提取角色声明(兼容 ZITADEL 嵌套 map 形态)。
func extractClaimRoles(claims jwt.MapClaims) []string {
	claim := config.Config.Auth.RoleClaim
	v, ok := claims[claim]
	if !ok {
		return nil
	}
	switch roles := v.(type) {
	case []any:
		out := make([]string, 0, len(roles))
		for _, r := range roles {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return auth.NormalizeRoleKeys(out)
	case []string:
		return auth.NormalizeRoleKeys(roles)
	case string:
		return auth.NormalizeRoleKeys(strings.Split(roles, ","))
	case map[string]any:
		out := make([]string, 0, len(roles))
		for k := range roles {
			out = append(out, k)
		}
		return auth.NormalizeRoleKeys(out)
	}
	return nil
}

func abort(c *app.RequestContext, code errcode.ErrCode, msg string) {
	c.AbortWithStatusJSON(401, errcode.New(code, msg, nil))
}

// RequirePermission 权限中间件(403)。
func RequirePermission(code string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		uid := service.FromCtx(ctx).ID
		ok, err := service.HasPermission(nil, uid, code)
		if err != nil || !ok {
			c.AbortWithStatusJSON(403, errcode.New(errcode.ErrForbidden, "无权限执行该操作", nil))
			return
		}
		c.Next(ctx)
	}
}

// CORS 跨域中间件(开发期前端 5173 直连)。
func CORS() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "43200")
		if string(c.Method()) == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next(ctx)
	}
}
