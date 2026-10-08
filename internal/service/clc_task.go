// clc_task.go —— AGG-CLC-06 集群任务行为实现(m2 FUNC-03/04/09~12/25)。
//
// 事务口径:前置校验(INV-15 PENDING 不得交付 / INV-33 销毁前置审批) → 规则校验(INV-28~32/R-09/R-13) →
// 任务创建(QUEUED) → 交付内核单步推进(clc_delivery.go FLOW-PROC-06) → 终态落点回写(R-09/R-13/R-14) → 审计/事件。
// 任务记录只增不改(INV-31):状态回写由系统流程承载;执行位置登记值绝不静默切换(INV-32)。
// 交付步骤与日志为 ClusterTask 聚合内子实体(INV-42/43):游标只增、落库前掩码,由内核承载。
package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ListTasks FUNC-25 任务台账查询(REP-07)。
func ListClusterTasks(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE t.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND t.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["task_kind"]); v != "" {
		where += " AND t.task_kind = ?"
		args = append(args, v)
	}
	if v := Str(p["task_status"]); v != "" {
		where += " AND t.task_status = ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (t.task_no LIKE ? OR t.submitted_by LIKE ?)"
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_task t `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT t.*, c.name AS cluster_name
		FROM clc_task t
		LEFT JOIN clc_cluster c ON c.cluster_no = t.cluster_ref AND c.flag = 1
		`+where+` ORDER BY t.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// createTaskTx 任务创建(事务内;落 QUEUED,执行位置登记值 INV-32)。
// QUEUED ⇒ 执行权由交付内核经槽位/互斥判定后授予(FLOW-PROC-06 A02);RUNNING 与 started_at 由内核写入。
// stepMode 在创建瞬间落定且此后只读(REF-17 / INV-50):缺省由创建入口决定,内核不回改本列;
// operationReason 只在需要事由的入口落值(高危派发 FUNC-46 建单即带;跳步 FUNC-45 事后累加)。
func createTaskTx(db *gorm.DB, clusterNo, kind, execPath, stepMode, operationReason string, user UserInfo) (map[string]any, error) {
	if err := checkTaskKind(kind); err != nil {
		return nil, err
	}
	if err := checkExecPathRegistered(execPath); err != nil {
		return nil, err
	}
	if stepMode == "" {
		stepMode = StepModeAuto
	}
	if err := checkStepMode(stepMode); err != nil {
		return nil, err
	}
	no, err := genBizNo(db, NoPrefixTask, "clc_task", "task_no")
	if err != nil {
		return nil, err
	}
	if _, err := store.Exec(db, `
		INSERT INTO clc_task (task_no, cluster_ref, task_kind, task_status, execution_path, step_mode, operation_reason,
			submitted_by, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		no, clusterNo, kind, TaskStatusQueued, execPath, stepMode, nilIfEmpty(clampRunes(operationReason, maxOperationReason)),
		user.Username, user.ID, user.ID); err != nil {
		return nil, err
	}
	return store.One(db, `SELECT * FROM clc_task WHERE task_no = ?`, no)
}

// applyTaskOperationReasonTx 事由累加写任务头(R-24「代价留在数据上」的落点;后写不覆盖先写)。
// step_mode 列不参与任何 UPDATE(INV-50 只读),本函数只动 operation_reason。
func applyTaskOperationReasonTx(db *gorm.DB, taskNo, reason string, user UserInfo) error {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	cur, err := store.One(db, `SELECT operation_reason FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil || cur == nil {
		return err
	}
	joined := appendReasonText(Str(cur["operation_reason"]), reason, maxOperationReason)
	_, err = store.Exec(db, `UPDATE clc_task SET operation_reason=?, updated_by=?,
		updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE task_no=? AND flag=1`,
		joined, user.ID, taskNo)
	return err
}

// finishTaskTx 任务终态回写(事务内;INV-29 终态口径;R-09 INSTALL/RETRY_INSTALL 失败写回集群失败步骤索引)。
func finishTaskTx(db *gorm.DB, task map[string]any, status string, failStep *int, summary string, user UserInfo) error {
	kind := Str(task["task_kind"])
	failReason := ""
	if status == TaskStatusFailed {
		// INV-28:STALLED(失联挂起)与 FAILED(确定失败)分列,恢复路径不同
		failReason = TaskFailStepFailed
	}
	if err := checkTaskFinal(kind, status, failReason, summary, failStep); err != nil {
		return err
	}
	endedAt := "to_char(now(),'YYYY-MM-DD HH24:MI:SS')"
	setFailStep := "NULL"
	var args []any
	if failStep != nil {
		setFailStep = "?"
		args = append(args, *failStep)
	}
	args = append(args, failReason, summary, status, user.ID, Int(task["id"]))
	if _, err := store.Exec(db, fmt.Sprintf(`
		UPDATE clc_task SET fail_step_index=%s, fail_reason=?, result_summary=?, task_status=?, ended_at=%s,
			updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`, setFailStep, endedAt),
		args...); err != nil {
		return err
	}
	// R-09:INSTALL/RETRY_INSTALL 失败写回集群(失败步骤索引同步;STALLED ⇒ 降级归因 EXECUTOR_LOST)
	if status == TaskStatusFailed && (kind == TaskKindInstall || kind == TaskKindRetryInstall) && failStep != nil {
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET status=?, install_step_index=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_no=? AND flag=1`, ClusterStatusInstallFailed, *failStep, Str(task["cluster_ref"])); err != nil {
			return err
		}
	}
	if status == TaskStatusStalled {
		// R-09:STALLED ⇒ 降级归因 EXECUTOR_LOST(执行机失联)
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET health=?, degraded_reason=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_no=? AND flag=1 AND health <> ?`, HealthDegraded, DegradedExecutorLost, Str(task["cluster_ref"]), HealthDegraded); err != nil {
			return err
		}
	}
	return nil
}

// StartInstall FUNC-03 发起安装(前置 DRAFT 或 INSTALL_FAILED;执行交交付内核 FLOW-PROC-06)。
func StartInstall(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindInstall, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if s := Str(c["status"]); s != ClusterStatusDraft && s != ClusterStatusInstallFailed {
			return Fail(errcode.ErrState, "发起安装的前置状态为 DRAFT 或 INSTALL_FAILED(续跑),当前 %s", s)
		}
		return setClusterStatusTx(db, c, ClusterStatusInstalling, user)
	})
}

// setClusterStatusTx 集群运行状态回写(交付动作前置台账变更;终态由内核成功/失败落点回写)。
func setClusterStatusTx(db *gorm.DB, c map[string]any, status string, user UserInfo) error {
	if err := checkLifecycleTransition(Str(c["status"]), status); err != nil {
		return err
	}
	_, err := store.Exec(db, `UPDATE clc_cluster SET status=?, updated_by=?,
		updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
		status, user.ID, Int(c["id"]))
	return err
}

// installSuccessLandingTx 安装类成功落点(R-13 / m2 B-AUTO-09,交付内核末步结论后调用):
// 主控入口与凭据存放标识登记(INV-07;内容不入库 INV-08)⇒ 集群 RUNNING + 续跑索引清空 ⇒ 任务 SUCCEEDED。
// masterOverride/kubeRefOverride 为运行期覆盖值(空则按集群 DNS 与 vault 路径派生)。
func installSuccessLandingTx(db *gorm.DB, c, task map[string]any, masterOverride, kubeRefOverride, auditAction string, user UserInfo) error {
	if c == nil {
		return finishTaskTx(db, task, TaskStatusSucceeded, nil, "交付动作完成(集群台账缺失,无成功落点)", user)
	}
	master := masterOverride
	if master == "" {
		domain := Str(c["cluster_dns_domain"])
		if domain == "" {
			domain = "cluster.local"
		}
		master = "https://api." + domain + ":6443"
	}
	kubeRef := kubeRefOverride
	if kubeRef == "" {
		kubeRef = "vault://opic/clc/" + Str(c["cluster_no"]) + "/kubeconfig"
	}
	if err := checkKubeconfigRef(kubeRef); err != nil {
		return err
	}
	if _, err := store.Exec(db, `
		UPDATE clc_cluster SET status=?, master_endpoints=?, kubeconfig_ref=?, install_step_index=NULL,
			updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
		ClusterStatusRunning, master, kubeRef, user.ID, Int(c["id"])); err != nil {
		return err
	}
	// 四步向导登记的节点随集群就绪加入(INV-14):安装成功即回填 K8s 节点名并置 ADDED
	nodeNote := ""
	nodesAdded, err := markNodesAddedTx(db, Str(c["cluster_no"]), nil, "", user)
	if err != nil {
		return err
	}
	if nodesAdded > 0 {
		nodeNote = fmt.Sprintf(";登记节点 %d 个已加入", nodesAdded)
	}
	if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, fmt.Sprintf("安装成功:主控入口 %s(凭据标识 %s)%s", master, kubeRef, nodeNote), user); err != nil {
		return err
	}
	WriteAudit(db, user.ID, user.Username, auditAction, fmt.Sprintf("集群 %s 安装成功落点(R-13):主控入口已登记,状态 → RUNNING,登记节点加入 %d 个",
		Str(c["cluster_no"]), nodesAdded))
	publishClcEvent("clc.cluster.installed", map[string]any{"cluster_no": Str(c["cluster_no"]), "master_endpoints": master})
	return nil
}

// RetryInstall FUNC-04 安装续跑(INV-03:从失败步骤索引继续、不重跑已成功步骤;展开走 R-17 SKIPPED)。
func RetryInstall(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindRetryInstall, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusInstallFailed {
			return bizRuleFail("INV-03", "安装续跑的前置状态为 INSTALL_FAILED,当前 %s", Str(c["status"]))
		}
		// INV-03:INSTALL_FAILED 必须携带失败步骤索引(nil 判定;步骤 0 合法;数值列经 Str 会为空,勿用 Str 判空)
		if c["install_step_index"] == nil {
			return bizRuleFail("INV-03", "INSTALL_FAILED 必须携带失败步骤索引(集群行 install_step_index 为空,不可续跑)")
		}
		return setClusterStatusTx(db, c, ClusterStatusInstalling, user)
	})
}

// runClusterAction 交付动作统一骨架:前置校验 → 任务创建(QUEUED)+ 领域前置落点(回调) → 审计 →
// 交付内核执行(FLOW-PROC-06 单步推进;终态由成功/失败落点回写,不在本事务内伪造)。
func runClusterAction(clusterID int64, kind string, d map[string]any, user UserInfo,
	apply func(db *gorm.DB, c map[string]any, task map[string]any) error) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if Str(c["status"]) == ClusterStatusDestroyed {
		return nil, bizRuleFail("INV-02", "集群已销毁(终态),不可执行交付动作")
	}
	clusterNo := Str(c["cluster_no"])
	execPath := Str(d["execution_path"])
	if execPath == "" {
		execPath = execPathOf(c) // O-IGN 侧 Q-71:未显式选链路时按集群登记事实推导(绑定执行机 ⇒ 远程)
	}
	// PENDING 节点不得作为交付目标(INV-15):动作声明节点时校验
	if nodeNo := Str(d["node_no"]); nodeNo != "" {
		if n, err := qOne(`SELECT node_status FROM clc_cluster_node WHERE node_no=? AND cluster_ref=? AND flag=1`, nodeNo, clusterNo); err != nil {
			return nil, err
		} else if n != nil && Str(n["node_status"]) == NodeStatusPending {
			return nil, bizRuleFail("INV-15", "PENDING(待加入)节点 %s 不得作为交付动作的目标", nodeNo)
		}
	}
	var taskNo string
	// REF-17:步进模式缺省按创建入口取 —— 本骨架是各入口的公共下游,非向导入口(编排屏
	// business/clc.go ClcTaskAction 的 INSTALL/RETRY_INSTALL/UPGRADE/BACKUP/RESTORE/START/STOP、
	// 审批回调 SyncClusterDestroyFromFlow、定时看护)都不写 d["step_mode"] ⇒ 缺省 AUTO,
	// v1.1 连跑行为逐字不变;向导入口(clc_wizard.go 的 WizardStepRetry)在进入本口之前
	// 已把显式取值(STEP)写回 d["step_mode"],由紧随其后的 resolveStepMode 优先采纳。
	stepMode, err := resolveStepMode(Str(d["step_mode"]), StepModeAuto)
	if err != nil {
		return nil, err
	}
	err = tx(func(db *gorm.DB) error {
		task, err := createTaskTx(db, clusterNo, kind, execPath, stepMode, Str(d["operation_reason"]), user)
		if err != nil {
			return err
		}
		taskNo = Str(task["task_no"])
		if err := apply(db, c, task); err != nil {
			return err
		}
		// INV-45 先展开后执行 + m2 FUNC-44 前置「步骤名与序次全部读 FUNC-34 数据行」:
		// 步骤行须在创建事务内即落库,STEP 任务停在排队态(等人派发)时向导步进台才能渲染清单;
		// 生命周期台单步动作(FUNC-46)同样在此展开一行(R-17)。执行协程内的同名展开
		// (clc_delivery.go execute 的 INV-45 兜底)对已展开任务幂等返回,不产生第二行集(INV-37)。
		resumeFrom := 0
		if kind == TaskKindRetryInstall && c["install_step_index"] != nil {
			resumeFrom = int(paramInt(c["install_step_index"]))
		}
		return expandStepsTx(db, taskNo, kind, clusterNo, resumeFrom, clusterActionArgs(d).NodeIPs, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, behaviorOf(kind), fmt.Sprintf("集群 %s 交付动作 %s(%s 链路)", clusterNo, kind, execPath))
	// 事务提交后交内核:槽位占满则保持 QUEUED,由 janitor 按序推进(R-18/REF-14)
	enqueueTask(taskNo, clusterActionArgs(d))
	return GetCluster(clusterID)
}

// clusterActionArgs 交付动作请求 → 内核运行期参数(凭据只登记存放标识 INV-08;不入库明文)。
func clusterActionArgs(d map[string]any) DeliveryArgs {
	args := DeliveryArgs{
		MasterEndpoints: Str(d["master_endpoints"]),
		KubeconfigRef:   Str(d["kubeconfig_ref"]),
		VersionRef:      Str(d["version_ref"]),
		FailStep:        int(Int(d["fail_step_index"])),
	}
	if ip := Str(d["node_ip"]); ip != "" {
		args.NodeIPs = []string{ip}
	}
	return args
}

// behaviorOf 任务类别 → m2 行为编号(审计回溯用)。
func behaviorOf(kind string) string {
	switch kind {
	case TaskKindRetryInstall:
		return "FUNC-04"
	case TaskKindUpgrade:
		return "FUNC-09"
	case TaskKindBackup:
		return "FUNC-10"
	case TaskKindRestore:
		return "FUNC-11"
	case TaskKindStart, TaskKindStop:
		return "FUNC-12"
	case TaskKindCertRenew:
		// m2 FUNC-46:控制面证书续期是生命周期台的显式登记项(既有 CLC_TASK_KIND 着陆,不新增类别值)
		return "FUNC-46"
	}
	return "FUNC-03"
}

// ---------- DEF-06:task_kind → 行为级权限分档(从运行时本体注册表派生,不在 Go 里抄码表) ----------
//
// 本段运行时不携带任何「kind→权限码」字面量表 —— 解析链两级都读注册表(ontology.Load()):
//
//	① kind→所属行为(FUNC-nn):由 m2 行为的 postconditions 派生 —— field == "ClusterTask.taskKind"
//	   的落点(setValue 中命中 CLC_TASK_KIND 字典码的 token)登记在 taskKindBehaviorIndex;
//	   模型当前显式着陆 RETRY_INSTALL/UPGRADE/BACKUP/RESTORE/START/STOP(以及被 FUNC-31 着陆的 DESTROY)。
//	   INSTALL(FUNC-03 本体)与 CERT_RENEW(FUNC-46 本体)在 m2 没有 taskKind 后置条件行 ——
//	   模型未显式绑定,由 behaviorOf(审计留痕的既有同一映射,即本 handler switch 各分支实际实现的行为)兜底;
//	② 行为→权限码:注册表 Behaviors[FUNC-nn]["requiredPermissions"](m2 每行为自带,逐条 PERM-FUNC-nn)反查,
//	   并对 m5 侧校验:该码必须已注册在 Permissions 表且 targetRef 指回该行为 —— 链条任一环断裂 ⇒ 解析不出,
//	   CheckTaskKindPermission 一律拒绝(fail-closed),不放行。
//
// 下表是模型的当前口径(运行时以注册表派生结果为准;TestTaskKindPermCodeDerivedFromOntology 把它钉成断言,
// models/ 漂移时该用例必须红,而不是靠本注释):
//
//	task_kind        m2 行为    权限码           m2 行为名              m5 发牌角色
//	INSTALL          FUNC-03    PERM-FUNC-03     发起安装                CLC-ROLE-01
//	RETRY_INSTALL    FUNC-04    PERM-FUNC-04     安装续跑                CLC-ROLE-01
//	UPGRADE          FUNC-09    PERM-FUNC-09     发起升级                CLC-ROLE-01
//	BACKUP           FUNC-10    PERM-FUNC-10     发起备份                CLC-ROLE-01
//	RESTORE          FUNC-11    PERM-FUNC-11     发起恢复                CLC-ROLE-01
//	START / STOP     FUNC-12    PERM-FUNC-12     集群启停                CLC-ROLE-01
//	CERT_RENEW       FUNC-46    PERM-FUNC-46     生命周期台动作派发       CLC-ROLE-01
//	ADD_NODE         FUNC-06    PERM-FUNC-06     添加节点                CLC-ROLE-01(专用口 routes.go:70,不经本门)
//	DEL_NODE         FUNC-07    PERM-FUNC-07     移除节点                CLC-ROLE-01(专用口 routes.go:71,不经本门)
//	DESTROY          FUNC-05    PERM-FUNC-05     集群销毁·提交申请       CLC-ROLE-01(唯一经审批流 PROC-APP-01,任何派发口都不直派)
//
// m5 发牌集摘要(与上表配套的「谁持有哪一档」):
//
//	CLC-ROLE-01 集群管理员:PERM-FUNC-01~27、29、33~40、42~50 —— 独占上表全部交付动作档;
//	CLC-ROLE-02 平台运维管理员:PERM-FUNC-19~29、30~32、34~37、41、47 —— 不含 03/04/09/10/11/12/46,
//	  即「平台侧运维」能管引用/参数/模板,但不得对集群发起任何交付动作;
//	CLC-ROLE-03 集群审计员 / CLC-ROLE-04 集群只读用户:只读档,不含任何交付动作档;
//	CLC-ROLE-05 下游能力中心:仅 PERM-FUNC-28。
//
// ADD_NODE/DEL_NODE/DESTROY 三行刻意不进本门(受理集见 taskActionGatedKind):它们各有专用口
// (专用口已有路由级门),而 DESTROY 在派发口由既有 INV-33 分支拒绝 —— 本门不得改写它的既有报错口径。
// 表外 task_kind ⇒ CheckTaskKindPermission 一律拒绝(无默许通道)。
var taskKindBehaviorRe = regexp.MustCompile(`[A-Z][A-Z0-9_]*`)

var (
	taskKindBehaviorOnce sync.Once
	// taskKindBehaviorIdx kind→FUNC-nn:纯 m2 postconditions 派生结果(不含兜底行为)。
	taskKindBehaviorIdx map[string]string
)

// taskKindBehaviorIndex 从注册表惰性派生「任务类别→显式着陆该类别的行为」。
// token 只认 CLC_TASK_KIND 字典码(过滤「INV」「REF」等伴随 token);行为按编号升序遍历,
// 同一类别被多个行为显式着陆时取首个(当前模型无此类竞争,排序仅为派生结果可复现)。
func taskKindBehaviorIndex() map[string]string {
	taskKindBehaviorOnce.Do(func() {
		reg := ontology.Load()
		kindSet := map[string]bool{}
		for _, it := range reg.GetDictionaryItems("DICT-CLC_TASK_KIND", "CLC_TASK_KIND") {
			kindSet[it["code"]] = true
		}
		ids := make([]string, 0, len(reg.Behaviors))
		for id := range reg.Behaviors {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		idx := map[string]string{}
		for _, id := range ids {
			pcs, _ := reg.Behaviors[id]["postconditions"].([]any)
			for _, pc := range pcs {
				pm, ok := pc.(map[string]any)
				if !ok || Str(pm["field"]) != "ClusterTask.taskKind" {
					continue
				}
				for _, tok := range taskKindBehaviorRe.FindAllString(Str(pm["setValue"]), -1) {
					if !kindSet[tok] {
						continue
					}
					if _, dup := idx[tok]; !dup {
						idx[tok] = id
					}
				}
			}
		}
		taskKindBehaviorIdx = idx
	})
	return taskKindBehaviorIdx
}

// taskActionGatedKind 本门受理集 = /task handler 七个 switch 分支 ∪ 生命周期台直派动作集
// (clc_wizard.go lifecycleActionKinds)。这是「哪些类别归行为级分档门管」的作用域声明,
// 不含任何权限语义;权限码一律从注册表反查,不在此处出现。
func taskActionGatedKind(kind string) bool {
	switch kind {
	case TaskKindInstall, TaskKindRetryInstall:
		return true
	default:
		return lifecycleActionKinds[kind]
	}
}

// lookupTaskKindPerm 解析 kind 的行为级权限码:kind→行为(postconditions 派生优先,
// INSTALL/CERT_RENEW 由 behaviorOf 兜底)→注册表 requiredPermissions→m5 注册校验。
// ok=false = 解析链断裂(行为未注册 / requiredPermissions 缺失或与 m5 不同源),调用方必须拒绝。
func lookupTaskKindPerm(kind string) (permCode, behaviorID string, ok bool) {
	behaviorID, derived := taskKindBehaviorIndex()[kind]
	if !derived {
		behaviorID = behaviorOf(kind) // 兜底:模型未显式着陆 taskKind 的入口行为本体(FUNC-03 / FUNC-46)
	}
	reg := ontology.Load()
	b := reg.Behaviors[behaviorID]
	if b == nil {
		return "", "", false
	}
	list, _ := b["requiredPermissions"].([]any)
	if len(list) != 1 {
		return "", "", false // m2 口径:交付动作行为逐条一码;多码/缺码说明模型漂移,拒绝而非猜
	}
	code := Str(list[0])
	if p := reg.Permissions[code]; p == nil || Str(p["targetRef"]) != behaviorID {
		return "", "", false // 与 m5 permissions 注册表对不上(码不存在或 targetRef 不指回该行为)
	}
	return code, behaviorID, true
}

// TaskKindPermCode 查 task_kind 所属的行为级权限码;ok=false 表示该类别不在交付动作派发门内
// (专用口类别 ADD_NODE/DEL_NODE 与审批流唯一类别 DESTROY,以及一切字典外取值),
// 或注册表解析链断裂(fail-closed)。
func TaskKindPermCode(kind string) (string, bool) {
	k := strings.ToUpper(strings.TrimSpace(kind))
	if !taskActionGatedKind(k) {
		return "", false
	}
	code, _, ok := lookupTaskKindPerm(k)
	return code, ok
}

// CheckTaskKindPermission DEF-06 第二道门:同一 handler 内按 task_kind 复查「这一档动作」自己的权限。
//
// 为什么需要第二道:路由级 middleware.perm(...) 是「一道门管一族动作」的粗粒度门
// (routes.go:84 task-action 挂 PERM-FUNC-03、routes.go:116 lifecycle-action 挂 PERM-FUNC-46),
// 只保证「调用者能进这个口」,不等于他能发起这一档具体动作 —— 持 PERM-FUNC-46 的人不该
// 顺手发起 UPGRADE(PERM-FUNC-09),持 PERM-FUNC-03 的人不该顺手 STOP(PERM-FUNC-12)。
// m2/m5 把每一档动作单独发牌,本函数就是那份发牌集的执行力;调用点必须落在任何副作用之前
// (不建 clc_task 行、不写审计、不发事件、不碰交付门面)。
//
// 报错口径:沿用既有权限门的 errcode(errcode.ErrForbidden)与「无权限…」措辞,不新造码。
// 授权查询失败(库不可用等)同样拒绝 —— 判定不确定时不放行(fail-closed)。
func CheckTaskKindPermission(user UserInfo, kind string) error {
	k := strings.ToUpper(strings.TrimSpace(kind))
	if !taskActionGatedKind(k) {
		return Fail(errcode.ErrForbidden,
			"无权限发起交付动作「%s」:该类别不在 M2 交付动作派发档位表内(表外类别一律拒绝,无默许通道)", kind)
	}
	code, behaviorID, ok := lookupTaskKindPerm(k)
	if !ok {
		// 映射缺失(注册表里该行为不存在 / requiredPermissions 与 m5 注册不同源)⇒ 拒绝而非放行(fail-closed)。
		return Fail(errcode.ErrForbidden,
			"无权限发起交付动作「%s」:本体注册表中无法确认该动作的行为级授权(行为 %s 的发牌口径与 m5 注册表对不上),按拒绝处理", kind, behaviorOf(k))
	}
	granted, err := HasPermission(nil, user.ID, code)
	if err != nil {
		return Fail(errcode.ErrForbidden, "无权限发起交付动作「%s」:授权校验未完成(%v),按拒绝处理", kind, err)
	}
	if !granted {
		return Fail(errcode.ErrForbidden,
			"无权限执行该操作:交付动作「%s」是 %s 档(M2 行为级授权),需持有 %s;当前用户未获发该牌(M5 发牌集:交付动作档仅 CLC-ROLE-01 集群管理员持有)",
			k, behaviorID, code)
	}
	return nil
}

// StartUpgrade FUNC-09 发起升级(前置 RUNNING;版本引用变更与 CHANGE 快照在成功落点 B-AUTO-09 后回写)。
func StartUpgrade(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindUpgrade, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起升级的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		return nil
	})
}

// StartBackup FUNC-10 发起备份(前置 RUNNING)。
func StartBackup(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindBackup, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起备份的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		return nil
	})
}

// StartRestore FUNC-11 发起恢复(前置 RUNNING;恢复后 CHANGE 快照在成功落点回写)。
func StartRestore(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindRestore, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起恢复的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		return nil
	})
}

// StartStop FUNC-12 集群启停(START/STOP 任务;状态保持 RUNNING,健康留痕)。
func StartStop(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	kind := Str(d["task_kind"])
	if kind != TaskKindStart && kind != TaskKindStop {
		return nil, Fail(errcode.ErrParam, "集群启停任务类别只能取 START 或 STOP")
	}
	return runClusterAction(clusterID, kind, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "集群启停的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		return nil
	})
}

// ---------- 交付动作成功落点(FLOW-PROC-06 末步结论后由内核调用) ----------
//
// 业务入口只做前置校验与 QUEUED 排队,领域终态一律由内核触达:创建事务内不再伪造 SUCCEEDED。
func init() {
	taskSuccessHooks[TaskKindUpgrade] = upgradeSuccessLanding
	taskSuccessHooks[TaskKindBackup] = simpleLanding("备份完成:交付工具汇总判定(备份产物按存放标识登记,本域不读取内容)")
	taskSuccessHooks[TaskKindRestore] = restoreSuccessLanding
	taskSuccessHooks[TaskKindStart] = simpleLanding("集群启动动作完成(交付工具判定;运行状态由探活刷新)")
	taskSuccessHooks[TaskKindStop] = simpleLanding("集群停止动作完成(交付工具判定;运行状态由探活刷新)")
}

// simpleLanding 无额外领域落点的交付动作:仅任务成功终局。
func simpleLanding(summary string) taskSuccessHook {
	return func(db *gorm.DB, task, _ map[string]any, _ DeliveryArgs, user UserInfo) error {
		return finishTaskTx(db, task, TaskStatusSucceeded, nil, summary, user)
	}
}

// upgradeSuccessLanding FUNC-09 成功落点:版本引用变更(仅当声明目标版本)+ CHANGE 快照留痕。
func upgradeSuccessLanding(db *gorm.DB, task, cluster map[string]any, args DeliveryArgs, user UserInfo) error {
	target, clusterNo, oldVer := trimEmpty(args.VersionRef), "", ""
	if cluster != nil {
		clusterNo, oldVer = Str(cluster["cluster_no"]), Str(cluster["version_ref"])
	}
	summary, snap := "升级完成:原地升级(交付版本引用未变更)", "升级:原地升级"
	if target != "" {
		summary, snap = fmt.Sprintf("升级完成:版本 %s → %s", oldVer, target), "升级:版本引用变更"
	}
	if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, summary, user); err != nil {
		return err
	}
	if cluster == nil {
		return nil
	}
	if target != "" {
		if _, err := store.Exec(db, `UPDATE clc_cluster SET version_ref=?, updated_by=?,
			updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			target, user.ID, Int(cluster["id"])); err != nil {
			return err
		}
	}
	return createSnapshotTx(db, clusterNo, snap, SnapshotTypeChange, user)
}

// restoreSuccessLanding FUNC-11 成功落点:恢复动作留 CHANGE 快照(回滚另有 FUNC-27 快照回退口径)。
func restoreSuccessLanding(db *gorm.DB, task, cluster map[string]any, _ DeliveryArgs, user UserInfo) error {
	if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, "恢复完成:集群状态恢复至备份点(备份产物由交付工具定位)", user); err != nil {
		return err
	}
	if cluster == nil {
		return nil
	}
	return createSnapshotTx(db, Str(cluster["cluster_no"]), "恢复后快照(RESTORE 落点)", SnapshotTypeChange, user)
}
