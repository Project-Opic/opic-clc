// clc_seed_parity_pg_test.go —— m5 声明的权限码 ↔ goose 种子迁移落库行，差集必须为空。
// 与姊妹仓 ign 同判据（DEF-06 同族）：运行期 syncM5Authorization 按模型补齐缺行，
// "漏一行种子"只有纯迁移建库那条路径才暴露，本用例把它钉成失效位点。
package service

import (
	"sort"
	"testing"
)

func TestGooseSeedCoversEveryM5Permission(t *testing.T) {
	requireDB(t)

	// 测试装置只跑 migrations/，不跑 EnsureSeed ⇒ 这里的行完全来自种子迁移。
	inDB := map[string]bool{}
	for _, r := range mustList(t, `SELECT code FROM sys_permission WHERE code LIKE ?`, "PERM-FUNC-%") {
		inDB[Str(r["code"])] = true
	}

	model := buildSeedPermissions()
	modelSet := make(map[string]bool, len(model))
	var missing []string
	for _, p := range model {
		modelSet[p.Code] = true
		if !inDB[p.Code] {
			missing = append(missing, p.Code)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("m5 发牌在 goose 种子迁移里缺行: %v；种子由 tools/gen_m5_seed.rb models 生成，禁止手改", missing)
	}

	var stale []string
	for code := range inDB {
		if !modelSet[code] {
			stale = append(stale, code)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("种子里有 m5 未声明的业务权限码(模型改判后种子未重导): %v", stale)
	}
}

func TestGooseSeedCoversEveryM5RoleBinding(t *testing.T) {
	requireDB(t)

	bindings := buildSeedRolePermissions()
	if len(bindings) == 0 {
		t.Fatal("m5 角色全集为空，本体注册表未加载")
	}
	var missing []string
	for roleCode, codes := range bindings {
		for _, pc := range codes {
			if countRows(t,
				`SELECT count(*) FROM sys_role_permission rp
				 JOIN sys_role r ON r.id = rp.role_id
				 JOIN sys_permission p ON p.id = rp.permission_id
				 WHERE r.code = ? AND p.code = ?`, roleCode, pc) == 0 {
				missing = append(missing, roleCode+"→"+pc)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("m5 角色绑定在 goose 种子迁移里缺行: %v", missing)
	}
}
