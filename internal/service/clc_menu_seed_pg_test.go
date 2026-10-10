// clc_menu_seed_pg_test.go —— v1.3 菜单收敛(Q-116)的回归：侧栏可见叶必须由 MU application.menus
// 驱动(一级四叶)，退出侧栏的叶置不可见而非物理删除，且收敛不得覆盖运维自定义。
package service

import (
	"strings"
	"testing"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
)

// muMenuCodes MU 声明的菜单叶 code(menu-clc-cluster → clc-cluster，与本仓 sys_resource.code 约定同构)。
func muMenuCodes(t *testing.T, reg *ontology.Registry) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, raw := range reg.Menus {
		m, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("MU application.menus 行形态不符: %+v", raw)
		}
		code := strings.TrimPrefix(Str(m["menuId"]), "menu-")
		out[code] = Str(m["name"])
	}
	if len(out) == 0 {
		t.Fatal("MU application.menus 未装载(模型文件或注册表读取异常)")
	}
	return out
}

// assertV13Menus 断言库侧菜单已落在 v1.3 形态：四叶可见挂一级、名称随 MU、无多出可见叶。
func assertV13Menus(t *testing.T, reg *ontology.Registry) {
	t.Helper()
	want := muMenuCodes(t, reg)
	for code, name := range want {
		row := mustOne(t, `SELECT name, type, status, parent_id, path FROM sys_resource WHERE code = ?`, code)
		if row == nil {
			t.Fatalf("菜单叶 %s 未入库(MU application.menus)", code)
		}
		if Str(row["type"]) != "MENU" || Int(row["status"]) != 1 || toInt(row["parent_id"]) != 0 {
			t.Fatalf("%s 应是一级可见 MENU 叶: %+v", code, row)
		}
		if Str(row["name"]) != name {
			t.Fatalf("%s 名称应随 MU 登记为 %q,实际 %q", code, name, Str(row["name"]))
		}
		if !strings.HasPrefix(Str(row["path"]), "/clc/") {
			t.Fatalf("%s path 应指向本域前端路由,实际 %q", code, Str(row["path"]))
		}
	}
	// 侧栏可见 clc 叶数 == MU 声明数：多一个即失败(退出侧栏的叶与实现期多出叶都必须不可见)
	if n, wantN := countRows(t,
		`SELECT COUNT(*) FROM sys_resource WHERE type = 'MENU' AND status = 1 AND substr(code,1,4) = 'clc-'`),
		int64(len(want)); n != wantN {
		extra := mustList(t, `SELECT code, name, status, parent_id FROM sys_resource
			WHERE type = 'MENU' AND substr(code,1,4) = 'clc-' ORDER BY code`)
		t.Fatalf("可见 clc 菜单叶应为 MU 登记的 %d 个,实际 %d 个;全量: %+v", wantN, n, extra)
	}
}

func TestSeedMenusDerivedFromMuModel(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	if err := EnsureSeed(); err != nil {
		t.Fatalf("种子入库失败: %v", err)
	}
	if err := EnsureSeed(); err != nil {
		t.Fatalf("菜单种子二次执行不幂等: %v", err)
	}
	assertV13Menus(t, reg)
}

// seedLegacyMenusV12 把库摆回重组前的形态：两个目录 + 叶挂目录下 + 旧名称 + 退出侧栏的叶仍可见。
func seedLegacyMenusV12(t *testing.T) {
	t.Helper()
	mustOneID := func(code string) int64 {
		row := mustOne(t, `SELECT id FROM sys_resource WHERE code = ?`, code)
		if row == nil {
			t.Fatalf("前置缺失: %s", code)
		}
		return toInt(row["id"])
	}
	upsertDir := func(code, name string, sort int) {
		if _, err := qExec(`INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, sort_order, status)
			VALUES (0, ?, ?, '', 'DIRECTORY', '', NULL, 'Cluster', ?, 1)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, type = EXCLUDED.type, status = 1, parent_id = 0`,
			name, code, sort); err != nil {
			t.Fatalf("造旧目录 %s 失败: %v", code, err)
		}
	}
	upsertLeaf := func(dirCode, code, name, perm, path string, sort int) {
		if _, err := qExec(`INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, sort_order, status)
			VALUES (?, ?, ?, ?, 'MENU', ?, NULL, 'Database', ?, 1)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, permission_code = EXCLUDED.permission_code,
				type = EXCLUDED.type, path = EXCLUDED.path, status = 1, parent_id = EXCLUDED.parent_id`,
			mustOneID(dirCode), name, code, perm, path, sort); err != nil {
			t.Fatalf("造旧叶 %s 失败: %v", code, err)
		}
	}
	upsertDir("menu-l1-01", "集群管理", 10)
	upsertDir("menu-l1-02", "运行观测", 20)
	upsertLeaf("menu-l1-01", "clc-cluster", "集群台账", "PERM-FUNC-19", "/clc/cluster", 11)
	upsertLeaf("menu-l1-01", "clc-template", "集群模板", "PERM-FUNC-36", "/clc/template", 12)
	upsertLeaf("menu-l1-01", "clc-orch", "交付编排", "PERM-FUNC-25", "/clc/orch", 13)
	upsertLeaf("menu-l1-01", "clc-node", "节点管理", "PERM-FUNC-21", "/clc/node", 12)
	upsertLeaf("menu-l1-01", "clc-wizard", "装机向导", "PERM-FUNC-34", "/clc/wizard", 13)
	upsertLeaf("menu-l1-01", "clc-ref", "引用与参数", "PERM-FUNC-22", "/clc/ref", 14)
	upsertLeaf("menu-l1-01", "clc-snap", "快照与回滚", "PERM-FUNC-24", "/clc/snap", 15)
	upsertLeaf("menu-l1-01", "clc-health", "健康探活", "PERM-FUNC-26", "/clc/health", 16)
	upsertLeaf("menu-l1-02", "clc-ops", "运行观测", "PERM-FUNC-29", "/clc/ops", 21)
	upsertLeaf("menu-l1-02", "clc-params", "平台参数", "", "/clc/params", 22)
}

func TestMenusV13ConvergenceFromLegacyShape(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	if err := EnsureSeed(); err != nil {
		t.Fatalf("基线种子失败: %v", err)
	}
	seedLegacyMenusV12(t)
	if err := EnsureSeed(); err != nil {
		t.Fatalf("存量库收敛失败: %v", err)
	}
	assertV13Menus(t, reg)

	// 名称随 MU：集群台账→集群、交付编排→交付任务(旧名称不得留在侧栏)
	for code, old := range map[string]string{"clc-cluster": "集群台账", "clc-orch": "交付编排"} {
		if n := countRows(t, `SELECT COUNT(*) FROM sys_resource WHERE code = ? AND status = 1 AND name = ?`, code, old); n != 0 {
			t.Fatalf("%s 仍以旧名 %q 可见(Q-116 更名未落地)", code, old)
		}
	}
	// 退出侧栏的六叶：置不可见但不物理删除(旧深链兼容一版期间地址仍可追溯)
	for _, code := range menusV13Retired {
		row := mustOne(t, `SELECT status FROM sys_resource WHERE code = ?`, code)
		if row == nil {
			t.Fatalf("%s 被物理删除了(MU menuNote:置不可见而非删除)", code)
		}
		if Int(row["status"]) == 1 {
			t.Fatalf("%s 仍可见(应退出侧栏)", code)
		}
	}
	// 空目录置不可见
	for _, code := range menusV13Dirs {
		row := mustOne(t, `SELECT status FROM sys_resource WHERE code = ?`, code)
		if row == nil {
			continue
		}
		if Int(row["status"]) == 1 {
			t.Fatalf("目录 %s 已无可见子项却仍可见", code)
		}
	}
}

// TestMenusV13ConvergenceRespectsOperatorEdits 收敛只动种子口径：运维自定义的叶与挂载位不得被覆盖。
func TestMenusV13ConvergenceRespectsOperatorEdits(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	if err := EnsureSeed(); err != nil {
		t.Fatalf("基线种子失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = qExec(`DELETE FROM sys_resource WHERE code IN ('menu-ops-custom','clc-ops-custom')`)
		_, _ = qExec(`UPDATE sys_resource SET parent_id = 0 WHERE code = 'clc-template'`)
		if err := EnsureSeed(); err != nil {
			t.Errorf("清理后种子失败: %v", err)
		}
	})

	// ① 目录里存在运维自定义可见叶 ⇒ 目录保持可见(收敛不隐它还在用的分组)
	seedLegacyMenusV12(t)
	dir := mustOne(t, `SELECT id, status FROM sys_resource WHERE code = 'menu-l1-01'`)
	if dir == nil {
		t.Fatal("前置缺失: menu-l1-01")
	}
	if _, err := qExec(`INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, sort_order, status)
		VALUES (?, '运维自定义报表', 'clc-ops-custom', '', 'MENU', '/clc/ops', NULL, 'FundView', 90, 1)
		ON CONFLICT (code) DO UPDATE SET parent_id = EXCLUDED.parent_id, status = 1`,
		toInt(dir["id"])); err != nil {
		t.Fatalf("造运维自定义叶失败: %v", err)
	}
	if err := EnsureSeed(); err != nil {
		t.Fatalf("含自定义叶的收敛失败: %v", err)
	}
	if row := mustOne(t, `SELECT status FROM sys_resource WHERE code = 'menu-l1-01'`); row == nil || Int(row["status"]) != 1 {
		t.Fatalf("目录仍挂着运维自定义可见叶,不应被隐: %+v", row)
	}
	if _, err := qExec(`DELETE FROM sys_resource WHERE code = 'clc-ops-custom'`); err != nil {
		t.Fatalf("清理自定义叶失败: %v", err)
	}
	if err := EnsureSeed(); err != nil {
		t.Fatalf("自定义叶移除后的收敛失败: %v", err)
	}
	if row := mustOne(t, `SELECT status FROM sys_resource WHERE code = 'menu-l1-01'`); row == nil || Int(row["status"]) != 0 {
		t.Fatalf("目录已无可见子项,应置不可见: %+v", row)
	}

	// ② 运维把叶挪到自建分组 ⇒ 收敛不把它强拉到一级，但名称仍跟 MU
	if _, err := qExec(`INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, sort_order, status)
		VALUES (0, '运维分组', 'menu-ops-custom', '', 'DIRECTORY', '', NULL, 'Cluster', 80, 1)
		ON CONFLICT (code) DO UPDATE SET status = 1`); err != nil {
		t.Fatalf("造运维分组失败: %v", err)
	}
	custom := mustOne(t, `SELECT id FROM sys_resource WHERE code = 'menu-ops-custom'`)
	if _, err := qExec(`UPDATE sys_resource SET parent_id = ? WHERE code = 'clc-template'`, toInt(custom["id"])); err != nil {
		t.Fatalf("把模板叶挪进运维分组失败: %v", err)
	}
	if err := EnsureSeed(); err != nil {
		t.Fatalf("自定义挂载位下的收敛失败: %v", err)
	}
	row := mustOne(t, `SELECT parent_id, name FROM sys_resource WHERE code = 'clc-template'`)
	if toInt(row["parent_id"]) != toInt(custom["id"]) {
		t.Fatalf("运维自定义挂载位被覆盖: %+v", row)
	}
	if Str(row["name"]) != muMenuCodes(t, reg)["clc-template"] {
		t.Fatalf("名称应跟 MU,实际 %q", Str(row["name"]))
	}
}
