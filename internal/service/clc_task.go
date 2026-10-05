// clc_task.go —— AGG-CLC-06 集群任务行为实现(m2 FUNC-03/04/09~12/25)。
//
// 事务口径:前置校验(INV-15 PENDING 不得交付 / INV-33 销毁前置审批) → 规则校验(INV-28~32/R-09/R-13) →
// 任务创建(QUEUED→RUNNING) → 本地链路模拟执行 → 终态回写(R-09 失败写回集群) → 审计/事件。
// 任务记录只增不改(INV-31):状态回写由系统流程承载;执行位置登记值绝不静默切换(INV-32)。
// REF-12:交付动作日志复用平台统一任务通道回放,不在本域另建日志存储。
package service

import (
	"fmt"

	"gorm.io/gorm"

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

// createTaskTx 任务创建(事务内;QUEUED → RUNNING;登记执行位置 INV-32)。
func createTaskTx(db *gorm.DB, clusterNo, kind, execPath string, user UserInfo) (map[string]any, error) {
	if err := checkTaskKind(kind); err != nil {
		return nil, err
	}
	no, err := genBizNo(db, NoPrefixTask, "clc_task", "task_no")
	if err != nil {
		return nil, err
	}
	if _, err := store.Exec(db, `
		INSERT INTO clc_task (task_no, cluster_ref, task_kind, task_status, execution_path, started_at, submitted_by, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, to_char(now(),'YYYY-MM-DD HH24:MI:SS'), ?, ?, ?)`,
		no, clusterNo, kind, TaskStatusRunning, execPath, user.Username, user.ID, user.ID); err != nil {
		return nil, err
	}
	return store.One(db, `SELECT * FROM clc_task WHERE task_no = ?`, no)
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

// StartInstall FUNC-03 发起安装(创建 INSTALL 任务;前置 DRAFT;成功落点 R-13)。
func StartInstall(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindInstall, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if s := Str(c["status"]); s != ClusterStatusDraft && s != ClusterStatusInstallFailed {
			return Fail(errcode.ErrState, "发起安装的前置状态为 DRAFT 或 INSTALL_FAILED(续跑),当前 %s", s)
		}
		if _, err := store.Exec(db, `UPDATE clc_cluster SET status=?, updated_by=?,
			updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			ClusterStatusInstalling, user.ID, Int(c["id"])); err != nil {
			return err
		}
		// 模拟失败注入(冒烟验证 INV-30/R-09 写回)
		if failIdx := Int(d["fail_step_index"]); failIdx > 0 {
			fi := int(failIdx)
			return finishTaskTx(db, task, TaskStatusFailed, &fi, fmt.Sprintf("安装在第 %d 步失败(注入)", failIdx), user)
		}
		// R-13 安装成功落点:kubeconfig 存放标识登记(内容不入库 INV-08)+ 主控入口登记 + 状态置运行中 + 事件
		master := Str(d["master_endpoints"])
		if master == "" {
			domain := Str(c["cluster_dns_domain"])
			if domain == "" {
				domain = "cluster.local"
			}
			master = "https://api." + domain + ":6443"
		}
		kubeRef := Str(d["kubeconfig_ref"])
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
		if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, fmt.Sprintf("安装成功:主控入口 %s(凭据标识 %s)", master, kubeRef), user); err != nil {
			return err
		}
		WriteAudit(db, user.ID, user.Username, "FUNC-03", fmt.Sprintf("集群 %s 安装成功落点(R-13):主控入口已登记,状态 → RUNNING", Str(c["cluster_no"])))
		publishClcEvent("clc.cluster.installed", map[string]any{"cluster_no": Str(c["cluster_no"]), "master_endpoints": master})
		return nil
	})
}

// RetryInstall FUNC-04 安装续跑(INV-03:从失败步骤索引继续、不重跑已成功步骤)。
func RetryInstall(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if Str(c["status"]) != ClusterStatusInstallFailed {
		return nil, bizRuleFail("INV-03", "安装续跑的前置状态为 INSTALL_FAILED,当前 %s", Str(c["status"]))
	}
	// INV-03:INSTALL_FAILED 必须携带失败步骤索引(nil 判定;步骤 0 合法;数值列经 Str 会为空,勿用 Str 判空)
	if c["install_step_index"] == nil {
		return nil, bizRuleFail("INV-03", "INSTALL_FAILED 必须携带失败步骤索引(集群行 install_step_index 为空,不可续跑)")
	}
	return StartInstall(clusterID, d, user)
}

// runClusterAction 交付动作统一骨架:前置校验 → 任务创建 → 领域落点(回调) → 审计。
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
		execPath = ExecPathLocal
	}
	// PENDING 节点不得作为交付目标(INV-15):动作声明节点时校验
	if nodeNo := Str(d["node_no"]); nodeNo != "" {
		if n, err := qOne(`SELECT node_status FROM clc_cluster_node WHERE node_no=? AND cluster_ref=? AND flag=1`, nodeNo, clusterNo); err != nil {
			return nil, err
		} else if n != nil && Str(n["node_status"]) == NodeStatusPending {
			return nil, bizRuleFail("INV-15", "PENDING(待加入)节点 %s 不得作为交付动作的目标", nodeNo)
		}
	}
	err = tx(func(db *gorm.DB) error {
		task, err := createTaskTx(db, clusterNo, kind, execPath, user)
		if err != nil {
			return err
		}
		return apply(db, c, task)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, behaviorOf(kind), fmt.Sprintf("集群 %s 交付动作 %s(%s 链路)", clusterNo, kind, execPath))
	return GetCluster(clusterID)
}

// behaviorOf 任务类别 → m2 行为编号(审计回溯用)。
func behaviorOf(kind string) string {
	switch kind {
	case TaskKindUpgrade:
		return "FUNC-09"
	case TaskKindBackup:
		return "FUNC-10"
	case TaskKindRestore:
		return "FUNC-11"
	case TaskKindStart, TaskKindStop:
		return "FUNC-12"
	}
	return "FUNC-03"
}

// StartUpgrade FUNC-09 发起升级(前置 RUNNING;成功留 CHANGE 快照)。
func StartUpgrade(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindUpgrade, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起升级的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, fmt.Sprintf("升级完成:版本 %s → %s", Str(c["version_ref"]), Str(d["version_ref"])), user); err != nil {
			return err
		}
		if v := Str(d["version_ref"]); v != "" {
			if _, err := store.Exec(db, `UPDATE clc_cluster SET version_ref=? WHERE id=? AND flag=1`, v, Int(c["id"])); err != nil {
				return err
			}
		}
			summary := "升级:原地升级"
		if Str(d["version_ref"]) != "" {
			summary = "升级:版本引用变更"
		}
		return createSnapshotTx(db, Str(c["cluster_no"]), summary, SnapshotTypeChange, user)
	})
}

// StartBackup FUNC-10 发起备份(前置 RUNNING)。
func StartBackup(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindBackup, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起备份的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		return finishTaskTx(db, task, TaskStatusSucceeded, nil, "备份完成(平台任务通道回放,REF-12)", user)
	})
}

// StartRestore FUNC-11 发起恢复(前置 RUNNING;恢复动作留 CHANGE 快照)。
func StartRestore(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindRestore, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if Str(c["status"]) != ClusterStatusRunning {
			return Fail(errcode.ErrState, "发起恢复的前置状态为 RUNNING,当前 %s", Str(c["status"]))
		}
		if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, "恢复完成(备份点回放,REF-12)", user); err != nil {
			return err
		}
		return createSnapshotTx(db, Str(c["cluster_no"]), "恢复后快照(RESTORE 落点)", SnapshotTypeChange, user)
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
		return finishTaskTx(db, task, TaskStatusSucceeded, nil, "集群启停动作完成("+kind+")", user)
	})
}
