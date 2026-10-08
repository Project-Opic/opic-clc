// clc_task_perm_pg_test.go —— DEF-06 生命周期动作的权限分档(同一 handler 的第二道行为级校验)用例。
//
// 断言口径:m2 行为码 × m5 发牌集这张映射必须真的可执行,且其运行时来源是本体注册表
// (kind→行为由 m2 postconditions 派生 + 行为→权限码由 Behaviors[].requiredPermissions 反查,
// 见 clc_task.go lookupTaskKindPerm;Go 里没有码表副本)—— 有牌才放行、无牌一律拒绝,
// 且拒绝落在任何副作用之前(不建 clc_task 行、不展开步骤、不写审计、不发事件、不碰交付门面)。
// 牌与角色都取迁移 000006 落库的 sys_role / sys_permission / sys_role_permission(m5 的可执行副本):
// 用例不新建权限码、不新建行为,只在库内装配「谁持哪几张牌」的夹具。
// 报错断言:错误码 + 关键短词(中文判据字面量会润色,不抄长句)。
package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/infra"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
	"gitcode.com/opic-ontology/opic-techbase/pkg/eventbus"
)

// 用例专用用户:避开装置默认的 testUser(1)/tplUser(7),让「有牌/无牌」两条腿各自独立可证。
var (
	permAllUser      = UserInfo{ID: 9001, Username: "perm-admin"}        // CLC-ROLE-01:持有全部交付动作档
	permNoRoleUser   = UserInfo{ID: 9002, Username: "perm-nobody"}       // 不发任何角色:第二道门一律拒
	permPlatformUser = UserInfo{ID: 9003, Username: "perm-ops"}          // CLC-ROLE-02:m5 明确不持交付动作档
	permInstallUser  = UserInfo{ID: 9004, Username: "perm-install-only"} // 仅 PERM-FUNC-03:验证「按档」而非「按口」
)

// permBindRole 把用例用户绑到 m5 既有角色(迁移已备好角色行与发牌行,这里只补用户↔角色这条边)。
func permBindRole(t *testing.T, userID int64, roleCode string) {
	t.Helper()
	mustSQL(t, `
		INSERT INTO sys_user_role (user_id, role_id)
		SELECT ?, r.id FROM sys_role r
		WHERE r.code = ?
		  AND NOT EXISTS (SELECT 1 FROM sys_user_role ur WHERE ur.user_id = ? AND ur.role_id = r.id)`,
		userID, roleCode, userID)
}

// permRoleHoldingOnly 建一张测试期内专用角色、只发一张既有牌,并绑到用例用户。
// 用途:证明第二道门是真分档(进门不等于全放行)—— 只持 PERM-FUNC-03 的人能发起安装,
// 但发起不了升级/备份/启停。⚠ permCode 必须是 m5 既有 PERM-FUNC-nn,用例不新造码。
func permRoleHoldingOnly(t *testing.T, roleCode, roleName, permCode string, userID int64) {
	t.Helper()
	if n := countRows(t, `SELECT COUNT(*) FROM sys_permission WHERE code=?`, permCode); n != 1 {
		t.Fatalf("权限码 %s 未落库(不得新造牌): %d 行", permCode, n)
	}
	mustSQL(t, `
		INSERT INTO sys_role (code, name)
		SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM sys_role WHERE code = ?)`, roleCode, roleName, roleCode)
	mustSQL(t, `
		INSERT INTO sys_role_permission (role_id, permission_id)
		SELECT r.id, p.id FROM sys_role r, sys_permission p WHERE r.code = ? AND p.code = ?
		  AND NOT EXISTS (SELECT 1 FROM sys_role_permission rp WHERE rp.role_id = r.id AND rp.permission_id = p.id)`,
		roleCode, permCode)
	permBindRole(t, userID, roleCode)
}

// permWantDeny 断言拒绝腿:错误码沿用既有 ErrForbidden(不新造码),文案点名「无权限」+ 档位牌 + m2 行为码。
func permWantDeny(t *testing.T, err error, permCode, behavior string) string {
	t.Helper()
	if err == nil {
		t.Fatalf("无牌期望拒绝，实为放行(权限码 %s)", permCode)
	}
	be := AsBizErr(err)
	if be.Code != errcode.ErrForbidden {
		t.Fatalf("错误码应为 %s，实为 %s(%s)", errcode.ErrForbidden, be.Code, be.Msg)
	}
	for _, f := range []string{"无权限", permCode, behavior} {
		if !strings.Contains(be.Msg, f) {
			t.Fatalf("拒绝文案应含 %q，实际: %s", f, be.Msg)
		}
	}
	return be.Msg
}

// permTiers task_kind → (m5 权限码, m2 行为码):models/m2-behavior-model.yaml + models/m5-actor-model.yaml
// 的当前口径(测试判据;运行时映射由注册表派生,模型漂移时 TestTaskKindPermCodeDerivedFromOntology 必须红)。
var permTiers = []struct {
	kind     string
	perm     string
	behavior string
}{
	{TaskKindInstall, "PERM-FUNC-03", "FUNC-03"},
	{TaskKindRetryInstall, "PERM-FUNC-04", "FUNC-04"},
	{TaskKindUpgrade, "PERM-FUNC-09", "FUNC-09"},
	{TaskKindBackup, "PERM-FUNC-10", "FUNC-10"},
	{TaskKindRestore, "PERM-FUNC-11", "FUNC-11"},
	{TaskKindStart, "PERM-FUNC-12", "FUNC-12"},
	{TaskKindStop, "PERM-FUNC-12", "FUNC-12"},
	{TaskKindCertRenew, "PERM-FUNC-46", "FUNC-46"},
}

// TestCheckTaskKindPermissionTiers 逐档验收第二道门:映射表与库内发牌集一致;全档角色放行;
// 无角色 / CLC-ROLE-02(平台运维,m5 不持交付动作档)一律拒;只持 PERM-FUNC-03 的角色仅 INSTALL 放行
// —— 分档真实存在,不是「进了这个口就什么都行」。
func TestCheckTaskKindPermissionTiers(t *testing.T) {
	requireDB(t)
	permBindRole(t, permAllUser.ID, "CLC-ROLE-01")
	permBindRole(t, permPlatformUser.ID, "CLC-ROLE-02")
	permRoleHoldingOnly(t, "PERMTEST-ROLE-01", "DEF-06 验收:仅发起安装", "PERM-FUNC-03", permInstallUser.ID)

	for _, tier := range permTiers {
		tier := tier
		t.Run(tier.kind, func(t *testing.T) {
			// ① 映射表本身:权限码 / 行为码与本档一致(行为码即审计用的那一枚)。
			code, ok := TaskKindPermCode(tier.kind)
			if !ok || code != tier.perm {
				t.Fatalf("task_kind %s 应映射到 %s，实际 %q(ok=%v)", tier.kind, tier.perm, code, ok)
			}
			if got := behaviorOf(tier.kind); got != tier.behavior {
				t.Fatalf("task_kind %s 的 m2 行为码应为 %s，实际 %s", tier.kind, tier.behavior, got)
			}
			// ② 这张牌必须真实落库(不新造码)，且由 CLC-ROLE-01 持有 —— 即 m5 发牌集的可执行副本。
			if n := countRows(t, `
				SELECT COUNT(*) FROM sys_role_permission rp
				JOIN sys_role r ON r.id = rp.role_id
				JOIN sys_permission p ON p.id = rp.permission_id
				WHERE r.code = 'CLC-ROLE-01' AND p.code = ? AND p.status = 1`, tier.perm); n != 1 {
				t.Fatalf("m5 发牌集里 CLC-ROLE-01 应持有 %s: 实际 %d 行", tier.perm, n)
			}
			// ③ 有牌 ⇒ 放行(放行后才能走到既有服务入口)。
			if err := CheckTaskKindPermission(permAllUser, tier.kind); err != nil {
				t.Fatalf("全档角色发起 %s 应放行: %v", tier.kind, err)
			}
			// ④ 无角色 ⇒ 拒(无默许通道)。
			permWantDeny(t, CheckTaskKindPermission(permNoRoleUser, tier.kind), tier.perm, tier.behavior)
			// ⑤ CLC-ROLE-02 平台运维:m5 未发任何交付动作档 ⇒ 逐档拒。
			permWantDeny(t, CheckTaskKindPermission(permPlatformUser, tier.kind), tier.perm, tier.behavior)
			// ⑥ 只持 PERM-FUNC-03 的角色:仅 INSTALL 放行,其余按档拒。
			err := CheckTaskKindPermission(permInstallUser, tier.kind)
			if tier.kind == TaskKindInstall {
				if err != nil {
					t.Fatalf("持 PERM-FUNC-03 应可发起安装: %v", err)
				}
				return
			}
			permWantDeny(t, err, tier.perm, tier.behavior)
		})
	}
}

// TestTaskKindPermCodeDerivedFromOntology DEF-06 判据一:kind→权限码映射必须来自运行时本体注册表,
// 不是 Go 字面量表。三段断言:
//
//	(a) 逐 kind(七个受理档 + CERT_RENEW)解析出的权限码 = PERM-FUNC-nn,且该码已注册在注册表
//	    Permissions 表、targetRef 指回对应 FUNC(m5 侧);行为自身 requiredPermissions 含该码(m2 侧,
//	    即码的取值来源就是模型字段);
//	(b) 模型 postconditions 显式着陆的六个 kind(RETRY_INSTALL/UPGRADE/BACKUP/RESTORE/START/STOP)
//	    必须出自 taskKindBehaviorIndex 的派生表 —— 证明 kind→行为这一跳也在读模型而非抄表;
//	(c) INSTALL/CERT_RENEW 无 taskKind 后置条件行(模型现状),由入口行为本体兜底(behaviorOf),
//	    并断言它们确实不在派生表里 —— 模型若日后补上显式着陆,兜底自动让位,派生表断言即失效位点。
func TestTaskKindPermCodeDerivedFromOntology(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	if len(reg.Behaviors) == 0 || len(reg.Permissions) == 0 {
		t.Fatal("本体注册表未加载(Behaviors/Permissions 为空)—— models_dir 未指向本仓 models/?")
	}
	for _, tier := range permTiers {
		tier := tier
		t.Run(tier.kind, func(t *testing.T) {
			code, behaviorID, ok := lookupTaskKindPerm(tier.kind)
			if !ok {
				t.Fatalf("注册表解析不到 %s 的行为级权限码(fail-closed 生效,但模型口径应有码)", tier.kind)
			}
			if code != tier.perm || behaviorID != tier.behavior {
				t.Fatalf("%s 应解析为 %s/%s,实际 %s/%s", tier.kind, tier.behavior, tier.perm, behaviorID, code)
			}
			// m5 侧:码在注册表 Permissions 里存在,targetRef 指回该行为。
			p := reg.Permissions[tier.perm]
			if p == nil {
				t.Fatalf("注册表 Permissions 缺 %s(m5 permissions)", tier.perm)
			}
			if got := Str(p["targetRef"]); got != tier.behavior {
				t.Fatalf("%s 的 targetRef 应指向 %s,实际 %s", tier.perm, tier.behavior, got)
			}
			// m2 侧:权限码取行为自带 requiredPermissions —— 这就是运行时取数处,Go 无码表。
			b := reg.Behaviors[tier.behavior]
			if b == nil {
				t.Fatalf("注册表 Behaviors 缺 %s", tier.behavior)
			}
			list, _ := b["requiredPermissions"].([]any)
			found := false
			for _, v := range list {
				if Str(v) == tier.perm {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s 的 requiredPermissions 应含 %s(m2 字段名 requiredPermissions)", tier.behavior, tier.perm)
			}
		})
	}
	idx := taskKindBehaviorIndex()
	for kind, fn := range map[string]string{
		TaskKindRetryInstall: "FUNC-04", TaskKindUpgrade: "FUNC-09", TaskKindBackup: "FUNC-10",
		TaskKindRestore: "FUNC-11", TaskKindStart: "FUNC-12", TaskKindStop: "FUNC-12",
	} {
		if got := idx[kind]; got != fn {
			t.Fatalf("postconditions 派生表:kind %s 应落在 %s,实际 %q", kind, fn, got)
		}
	}
	if got := idx[TaskKindDestroy]; got != "FUNC-31" {
		t.Fatalf("DESTROY 由 FUNC-31 显式着陆(仅作派生完备性断言;本门受理集刻意不含它,见 taskActionGatedKind),实际 %q", got)
	}
	if _, ok := idx[TaskKindInstall]; ok {
		t.Fatal("模型现状:FUNC-03 未写 taskKind 后置条件;若已出现请同步改走派生表并更新本断言")
	}
	if _, ok := idx[TaskKindCertRenew]; ok {
		t.Fatal("模型现状:CERT_RENEW 无显式 taskKind 着陆(归 FUNC-46 本体);若已出现请同步更新本断言")
	}
}

// TestCheckTaskKindPermissionOutOfTable 表外类别一律拒(即使全档角色):ADD_NODE/DEL_NODE 各有专用口
// (routes.go:70/71 的路由级门),DESTROY 唯一经审批流(FUNC-05),字典外取值更不是默许通道。
func TestCheckTaskKindPermissionOutOfTable(t *testing.T) {
	requireDB(t)
	permBindRole(t, permAllUser.ID, "CLC-ROLE-01")
	for _, kind := range []string{TaskKindAddNode, TaskKindDelNode, TaskKindDestroy, "FOO_BAR", ""} {
		kind := kind
		t.Run("表外-"+kind, func(t *testing.T) {
			if _, ok := TaskKindPermCode(kind); ok {
				t.Fatalf("类别 %s 不应出现在交付动作派发档位表内", kind)
			}
			be := AsBizErr(CheckTaskKindPermission(permAllUser, kind))
			if be.Code != errcode.ErrForbidden {
				t.Fatalf("表外类别应沿用 %s 拒绝，实际 %s(%s)", errcode.ErrForbidden, be.Code, be.Msg)
			}
			for _, f := range []string{"无权限", "档位表"} {
				if !strings.Contains(be.Msg, f) {
					t.Fatalf("拒绝文案应含 %q，实际: %s", f, be.Msg)
				}
			}
		})
	}
}

// permTap 进程内事件总线的捕获口(按内容片段过滤)。
type permTap struct {
	mu    sync.Mutex
	items []string
}

func (p *permTap) add(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, s)
}

func (p *permTap) matching(fragment string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, s := range p.items {
		if strings.Contains(s, fragment) {
			out = append(out, s)
		}
	}
	return out
}

// permEventTap 「不发事件」的可观测口:装配进程内总线(与生产同一 publishClcEvent 代码路径),
// 通配订阅本中心业务主题。inproc 驱动零依赖,不可用时直接失败而非跳过 —— 跳过会伪装成绿。
func permEventTap(t *testing.T) *permTap {
	t.Helper()
	prev := config.Config.CenterCode
	config.Config.CenterCode = "permtap"
	infra.Init()
	eb := infra.EB()
	if eb == nil {
		config.Config.CenterCode = prev
		t.Fatalf("事件总线未装配(降级 nil 时「无事件」断言不可观测)")
	}
	tap := &permTap{}
	sub, err := eb.Subscribe(context.Background(), "opic.permtap.business.>", "",
		func(_ context.Context, m eventbus.Message) error {
			tap.add(m.Subject + " " + string(m.Payload))
			return nil
		})
	if err != nil {
		config.Config.CenterCode = prev
		t.Fatalf("订阅业务事件主题失败: %v", err)
	}
	t.Cleanup(func() {
		_ = sub.Unsubscribe()
		config.Config.CenterCode = prev
	})
	return tap
}

// permDispatch 派发一次并只取错误(用例断言的是「拒绝 / 放行」这一件事,返回值另有专查)。
func permDispatch(clusterID int64, d map[string]any, user UserInfo) error {
	_, err := DispatchLifecycleAction(clusterID, d, user)
	return err
}

// TestDispatchLifecycleActionPermissionGate 派发口的闭合与零副作用:
// DispatchLifecycleAction 就是 routes.go:116 lifecycle-action 的服务层入口(也承接编排动作直派),
// 无牌请求必须在任何副作用之前拒绝 —— 本集群的 clc_task 行 / 步骤行 / 审计行 / 事件全部零增量;
// 有牌才走到既有服务入口建单排队。同集群在途 RUNNING 孤儿任务承担互斥夹具(INV-48/R-18),
// 使放行腿停在 QUEUED、不需要交付工具(与既有派发用例同一手法)。
func TestDispatchLifecycleActionPermissionGate(t *testing.T) {
	requireDB(t)
	permBindRole(t, permAllUser.ID, "CLC-ROLE-01")
	permRoleHoldingOnly(t, "PERMTEST-ROLE-01", "DEF-06 验收:仅发起安装", "PERM-FUNC-03", permInstallUser.ID)
	tap := permEventTap(t)
	// REF-16:LOCAL 链路要求部署配置登记交付工具路径 —— 放行腿的创建校验必须齐备
	// (任务停在 QUEUED,工具本身不会被调用,这里只是把链路前置条件补齐)。
	useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	clusterID := Int(mustClusterRow(t, clusterNo)["id"])
	seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusRunning, ExecPathLocal) // 在途孤儿:占住互斥,新单只排队

	// 基线:夹具自身的一条在途任务(无步骤行);三条拒绝腿之后必须逐字回到这条基线。
	taskBase := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo)
	auditBase := countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE detail LIKE ?`, "%"+clusterNo+"%")
	stepBaseSQL := `
		SELECT COUNT(*) FROM clc_task_step s
		JOIN clc_task t ON t.task_no = s.task_ref
		WHERE t.cluster_ref = ? AND s.flag = 1`
	stepBase := countRows(t, stepBaseSQL, clusterNo)

	// ① 完全无牌 ⇒ 拒。
	permWantDeny(t, permDispatch(clusterID, map[string]any{"task_kind": TaskKindBackup}, permNoRoleUser),
		"PERM-FUNC-10", "FUNC-10")
	// ② 持「仅发起安装」牌 ⇒ 同一口按档拒 BACKUP(证明不是「进门即放行」)。
	permWantDeny(t, permDispatch(clusterID, map[string]any{"task_kind": TaskKindBackup}, permInstallUser),
		"PERM-FUNC-10", "FUNC-10")
	// ③ 同上发起 STOP(高危 + 事由 + 二次确认齐备也不得放行):分档缺失即拒,高危前置轮不到。
	permWantDeny(t, permDispatch(clusterID, map[string]any{
		"task_kind": TaskKindStop, "operation_reason": "停机维护窗口", "confirmed": true,
	}, permInstallUser), "PERM-FUNC-12", "FUNC-12")

	// 拒绝腿零副作用:任务行、审计行、事件三项都不得因拒绝而增加。
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo); n != taskBase {
		t.Fatalf("无牌派发不得建任务行: 基线 %d，实际 %d", taskBase, n)
	}
	if n := countRows(t, stepBaseSQL, clusterNo); n != stepBase {
		t.Fatalf("无牌派发不得展开步骤行: 基线 %d，实际 %d", stepBase, n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE detail LIKE ?`, "%"+clusterNo+"%"); n != auditBase {
		t.Fatalf("无牌派发不得写审计: 基线 %d，实际 %d", auditBase, n)
	}
	if got := tap.matching(clusterNo); len(got) != 0 {
		t.Fatalf("无牌派发不得发事件: %v", got)
	}

	// ④ 既有分支口径不被本门改写:字典外、派发集外、DESTROY 仍是原来的判据字面量。
	tplWantErr(t, permDispatch(clusterID, map[string]any{"task_kind": "FOO_BAR"}, permNoRoleUser), "INV-28")
	tplWantErr(t, permDispatch(clusterID, map[string]any{"task_kind": TaskKindInstall}, permNoRoleUser),
		"生命周期台派发集")
	tplWantErr(t, permDispatch(clusterID, map[string]any{"task_kind": TaskKindDestroy}, permNoRoleUser), "INV-33")
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo); n != taskBase {
		t.Fatalf("上述三条拒绝同样不得有副作用: 基线 %d，实际 %d", taskBase, n)
	}

	// ⑤ 有牌 ⇒ 放行到既有服务入口:建单 QUEUED(互斥承担排队)+ 留痕。
	if _, err := DispatchLifecycleAction(clusterID, map[string]any{"task_kind": TaskKindBackup}, permAllUser); err != nil {
		t.Fatalf("有牌派发应放行: %v", err)
	}
	taskNo := lastTaskNo(t, clusterNo)
	if s := taskStatus(t, taskNo); s != TaskStatusQueued {
		t.Fatalf("同集群有在途任务时放行腿应排队 QUEUED(INV-48/R-18)，实际 %s", s)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo); n != taskBase+1 {
		t.Fatalf("放行应恰好新建一条任务行: 基线 %d，实际 %d", taskBase, n)
	}
	if n := countRows(t, stepBaseSQL, clusterNo); n <= stepBase {
		t.Fatalf("放行腿须在创建事务内展开步骤行(R-17/INV-45): 基线 %d，实际 %d", stepBase, n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE detail LIKE ?`, "%"+clusterNo+"%"); n <= auditBase {
		t.Fatalf("放行腿须按既有口径留痕: 基线 %d，实际 %d", auditBase, n)
	}
	// 放行腿的任务提交人就是持牌用户(第二道门放行后才走到服务层)。
	row := mustOne(t, `SELECT submitted_by, task_kind FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if Str(row["task_kind"]) != TaskKindBackup || Str(row["submitted_by"]) != permAllUser.Username {
		t.Fatalf("放行腿建单归属不符: %+v", row)
	}
}
