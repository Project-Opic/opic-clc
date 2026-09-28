package controller

import (
	"sync"

	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/auth"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/config"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/apiserver/service"
)

// ---------- OIDC state→verifier 暂存(PKCE) ----------

type pendingStore struct {
	mu   sync.Mutex
	data map[string]string
}

var pendingStates = &pendingStore{data: map[string]string{}}

func (p *pendingStore) set(state, verifier string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 简单容量保护:超 1000 清最旧(实现从简,底座不引入额外依赖)
	if len(p.data) > 1000 {
		p.data = map[string]string{}
	}
	p.data[state] = verifier
}

func (p *pendingStore) take(state string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	verifier, ok := p.data[state]
	delete(p.data, state)
	return verifier, ok
}

// ---------- ZITADEL 用户映射 ----------

// ensureUserFromZitadel 按 userinfo 取/建本地用户(claim 角色 + 默认角色)。
// 已存在用户且本次声明携带角色时,重新同步角色(ZITADEL 角色变更在下次登录生效)。
func ensureUserFromZitadel(ui *auth.UserInfo) int64 {
	username := ui.PreferredUsername
	if username == "" {
		username = ui.Sub
	}
	claimRoles := auth.RoleClaims(ui)
	if user, err := service.GetUserByUsername(username); err == nil && user != nil {
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
	uid, err := service.CreateUser(map[string]any{
		"username":   username,
		"password":   "zitadel-managed:" + auth.NewState(),
		"real_name":  orDefaultStr(ui.Name, username),
		"actor_type": "HUMAN",
		"status":     1,
	})
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	var roleIDs []any
	appendRole := func(code string) {
		code = trimSpaces(code)
		if code == "" || seen[code] {
			return
		}
		seen[code] = true
		if role, err := service.GetRoleByCode(code); err == nil && role != nil {
			roleIDs = append(roleIDs, service.Int(role["id"]))
		}
	}
	for _, code := range claimRoles {
		appendRole(code)
	}
	// 默认角色兜底(仅新用户且声明未携带角色)
	if len(roleIDs) == 0 {
		for _, code := range splitCSV(cfg.DefaultRoleCodes) {
			appendRole(code)
		}
	}
	if len(roleIDs) > 0 {
		_ = service.AssignRoles(uid, roleIDs)
	}
	return uid
}

func splitCSV(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
