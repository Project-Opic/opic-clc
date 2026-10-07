// clc_cluster.go —— AGG-CLC-01 集群行为实现(m2 FUNC-01/02/05/16/19/20/28/29)。
//
// 事务口径:前置校验 → 规则校验(INV-01~09/R-01/R-05/R-10/R-11/R-13) → 状态变更 →
// 快照留痕 → syncTriggers 联动(引用双写/参数派生) → 审计 + 事件旁路发布。
// 编号 CLU 由 genBizNo 同事务生成;销毁走 FLOW-PROC-APP-01 审批(保存草稿/提交双按钮)。
package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/infra"
	"gitcode.com/opic-ontology/opic-techbase/internal/service/engine"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

const (
	tblCluster     = "clc_cluster"
	flowDestroyApp = "FLOW-PROC-APP-01" // 集群销毁审批(m6,单级,CLC-ROLE-02 审批)
)

// clusterInlineRefs 内联引用列(R-01 双写:内联列 + 引用台账)。
var clusterInlineRefs = []string{"version_ref", "executor_ref", "registry_ref", "registry_project_ref"}

// GetCluster 集群详情(FUNC-20;REP-02:登记信息|引用摘要|健康归因|最近任务|kubeconfig 获取方式)。
func GetCluster(id int64) (map[string]any, error) {
	row, err := qOne(`SELECT * FROM clc_cluster WHERE id = ? AND flag = 1`, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NotFound("集群")
	}
	return clusterDetail(row)
}

// GetClusterByNo 按编号取集群。
func GetClusterByNo(no string) (map[string]any, error) {
	row, err := qOne(`SELECT * FROM clc_cluster WHERE cluster_no = ? AND flag = 1`, no)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NotFound("集群")
	}
	return clusterDetail(row)
}

// clusterDetail 引用摘要与最近任务聚合(账号-角色口径与台账一致)。
func clusterDetail(row map[string]any) (map[string]any, error) {
	no := Str(row["cluster_no"])
	if bindings, err := qList(`SELECT target_capability, target_kind, target_ref, purpose, binding_source
		FROM clc_binding WHERE cluster_ref = ? AND flag = 1 ORDER BY id`, no); err == nil {
		row["bindings"] = bindings
		row["binding_count"] = len(bindings)
	}
	if nodes, err := qCount(`SELECT COUNT(*) FROM clc_cluster_node WHERE cluster_ref = ? AND flag = 1`, no); err == nil {
		row["node_count"] = nodes
	}
	if latest, err := qOne(`SELECT task_no, task_kind, task_status, ended_at FROM clc_task
		WHERE cluster_ref = ? AND flag = 1 ORDER BY id DESC LIMIT 1`, no); err == nil {
		row["latest_task"] = latest
	}
	// INV-08:kubeconfig 只出存放位置标识,凭据内容任何接口不得出现
	row["kubeconfig_ref"] = Str(row["kubeconfig_ref"])
	return row, nil
}

// ListClusters FUNC-19 集群列表(REP-01:编号/名称/运行模式/状态/健康/节点数/最近任务)。
func ListClusters(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE c.flag = 1"
	var args []any
	if v := Str(p["status"]); v != "" {
		where += " AND c.status = ?"
		args = append(args, v)
	}
	if v := Str(p["run_mode"]); v != "" {
		where += " AND c.run_mode = ?"
		args = append(args, v)
	}
	if v := Str(p["health"]); v != "" {
		where += " AND c.health = ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (c.name LIKE ? OR c.cluster_no LIKE ? OR c.cluster_dns_domain LIKE ?)"
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_cluster c `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT c.*,
			COALESCE((SELECT COUNT(*) FROM clc_cluster_node n WHERE n.flag=1 AND n.cluster_ref = c.cluster_no),0) AS node_count,
			(SELECT t.task_no || ' ' || t.task_kind || ' ' || t.task_status FROM clc_task t
				WHERE t.flag=1 AND t.cluster_ref = c.cluster_no ORDER BY t.id DESC LIMIT 1) AS latest_task
		FROM clc_cluster c `+where+` ORDER BY c.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// CrossClusterList FUNC-28 跨中心集群查询(多用途;CLC-ROLE-05 服务身份;REP-10 精简列表)。
// INV-08:仅出非敏感列——编号/名称/状态/健康/运行模式/主控入口(不含凭据标识与凭据值)。
func CrossClusterList(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE flag = 1"
	var args []any
	if v := Str(p["status"]); v != "" {
		where += " AND status = ?"
		args = append(args, v)
	}
	if v := Str(p["cluster_no"]); v != "" {
		where += " AND cluster_no = ?"
		args = append(args, v)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_cluster `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`SELECT cluster_no, name, run_mode, status, health, master_endpoints, cluster_dns_domain
		FROM clc_cluster `+where+` ORDER BY cluster_no ASC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// CreateCluster FUNC-01 创建集群(四步向导提交)。
// 事务内:集群 + 节点 + 引用台账(双写 R-01) + 首快照(M6 FLOW-PROC-01 A02)。
func CreateCluster(d map[string]any, user UserInfo) (map[string]any, error) {
	if err := validateClusterInput(d); err != nil {
		return nil, err
	}
	refs := map[string]string{
		"version_ref": Str(d["version_ref"]), "executor_ref": Str(d["executor_ref"]),
		"registry_ref": Str(d["registry_ref"]), "registry_project_ref": Str(d["registry_project_ref"]),
	}
	runMode := Str(d["run_mode"])
	if runMode == "" {
		runMode = RunModeMCC
	}
	// REF-05 交付版本引用口径
	if err := checkVersionRef(refs["version_ref"]); err != nil {
		return nil, err
	}
	// INV-04/REF-01:LEGACY ⇔ 四类引用全空;平台禁用 legacy 时拒绝
	if err := checkRefConsistency(runMode, refs); err != nil {
		return nil, err
	}
	var created map[string]any
	err := tx(func(db *gorm.DB) error {
		// INV-01:集群名称在未销毁范围内唯一(不区分大小写,保存即校验)
		if dup, err := store.One(db, `SELECT id FROM clc_cluster WHERE flag=1 AND LOWER(name)=LOWER(?)`, Str(d["name"])); err != nil {
			return err
		} else if dup != nil {
			return bizRuleFail("INV-01", "集群名称「%s」已存在(未销毁范围内唯一)", Str(d["name"]))
		}
		no, err := genBizNo(db, NoPrefixCluster, tblCluster, "cluster_no")
		if err != nil {
			return err
		}
		if _, err := store.Exec(db, `
			INSERT INTO clc_cluster (cluster_no, name, description, cluster_dns_domain,
				version_ref, executor_ref, registry_ref, registry_project_ref, run_mode, status, health,
				created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			no, Str(d["name"]), d["description"], Str(d["cluster_dns_domain"]),
			refs["version_ref"], refs["executor_ref"], refs["registry_ref"], refs["registry_project_ref"],
			runMode, ClusterStatusDraft, HealthUnknown, user.ID, user.ID); err != nil {
			return err
		}
		// R-01 双写:内联列(已写) + 引用台账
		if err := createBindingsFromRefsTx(db, no, refs, user); err != nil {
			return err
		}
		// 四步向导:节点登记(可选,nodes:[{ip_address,node_role,asset_ref}])
		if err := createNodesTx(db, no, d["nodes"], user); err != nil {
			return err
		}
		// R-04 联动:引用派生参数(镜像源固定映射)
		if err := deriveParamsTx(db, no, refs, user); err != nil {
			return err
		}
		// M6 A02:首快照
		if err := createSnapshotTx(db, no, "创建集群(四步向导提交)", SnapshotTypeChange, user); err != nil {
			return err
		}
		var err2 error
		created, err2 = store.One(db, `SELECT * FROM clc_cluster WHERE cluster_no = ?`, no)
		return err2
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-01", fmt.Sprintf("创建集群 %s(%s):运行模式 %s,引用 %s",
		Str(created["cluster_no"]), Str(d["name"]), runMode, sortedSummary(refs)))
	publishClcEvent("clc.cluster.created", map[string]any{"cluster_no": Str(created["cluster_no"]), "name": Str(d["name"]), "run_mode": runMode})
	return GetCluster(Int(created["id"]))
}

// UpdateCluster FUNC-02 集群信息修改(INV-01/04/05/07/09;编号只读)。
func UpdateCluster(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	old, err := GetCluster(id)
	if err != nil {
		return nil, err
	}
	if no := Str(d["cluster_no"]); no != "" && no != Str(old["cluster_no"]) {
		return nil, bizRuleFail("INV-09", "集群编号只读且不可变更")
	}
	if err := validateClusterInput(d); err != nil {
		return nil, err
	}
	status := Str(old["status"])
	if status == ClusterStatusDestroyed {
		// INV-09:DESTROYED 冻结全部登记字段
		return nil, bizRuleFail("INV-09", "集群已销毁(DESTROYED 终态),登记字段全部冻结")
	}
	refs := map[string]string{
		"version_ref": Str(d["version_ref"]), "executor_ref": Str(d["executor_ref"]),
		"registry_ref": Str(d["registry_ref"]), "registry_project_ref": Str(d["registry_project_ref"]),
	}
	// 空值继承旧值(表单只提交变更字段)
	for k, v := range refs {
		if v == "" {
			refs[k] = Str(old[k])
		}
	}
	runMode := Str(d["run_mode"])
	if runMode == "" {
		runMode = Str(old["run_mode"])
	}
	if err := checkRefConsistency(runMode, refs); err != nil {
		return nil, err
	}
	if err := checkKubeconfigRef(Str(d["kubeconfig_ref"])); err != nil {
		return nil, err
	}
	if err := checkRunningFootprint(status, Str(d["master_endpoints"]), Str(d["kubeconfig_ref"])); err != nil {
		return nil, err
	}
	err = tx(func(db *gorm.DB) error {
		if dup, err := store.One(db, `SELECT id FROM clc_cluster WHERE flag=1 AND LOWER(name)=LOWER(?) AND id <> ?`, Str(d["name"]), id); err != nil {
			return err
		} else if dup != nil {
			return bizRuleFail("INV-01", "集群名称「%s」已存在", Str(d["name"]))
		}
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET name=?, description=?, cluster_dns_domain=?, version_ref=?, executor_ref=?,
				registry_ref=?, registry_project_ref=?, run_mode=?, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			Str(d["name"]), d["description"], Str(d["cluster_dns_domain"]),
			refs["version_ref"], refs["executor_ref"], refs["registry_ref"], refs["registry_project_ref"],
			runMode, user.ID, id); err != nil {
			return err
		}
		// R-01 双写:引用台账同步(以台账为准的全量权威,这里保证两处一致)
		if err := syncBindingsFromRefsTx(db, Str(old["cluster_no"]), refs, user); err != nil {
			return err
		}
		if err := deriveParamsTx(db, Str(old["cluster_no"]), refs, user); err != nil {
			return err
		}
		return createSnapshotTx(db, Str(old["cluster_no"]), "集群信息修改", SnapshotTypeChange, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-02", fmt.Sprintf("修改集群 #%d %s", id, Str(d["name"])))
	return GetCluster(id)
}

// createBindingsFromRefsTx R-01:内联引用 → 引用台账写入(创建口径;已有同组合则跳过)。
func createBindingsFromRefsTx(db *gorm.DB, clusterNo string, refs map[string]string, user UserInfo) error {
	pairs := []struct{ cap, kind, ref, purpose string }{}
	if refs["executor_ref"] != "" {
		pairs = append(pairs, struct{ cap, kind, ref, purpose string }{CapabilityIgnition, TargetKindExecutor, refs["executor_ref"], "EXECUTOR_RUNTIME"})
	}
	if refs["version_ref"] != "" {
		pairs = append(pairs, struct{ cap, kind, ref, purpose string }{CapabilityIgnition, TargetKindVersion, refs["version_ref"], "VERSION_BASE"})
	}
	if refs["registry_ref"] != "" {
		pairs = append(pairs, struct{ cap, kind, ref, purpose string }{CapabilityRegistry, TargetKindInstance, refs["registry_ref"], "IMAGE_MIRROR"})
	}
	if refs["registry_project_ref"] != "" {
		pairs = append(pairs, struct{ cap, kind, ref, purpose string }{CapabilityRegistry, TargetKindProject, refs["registry_project_ref"], "HARBOR_CLIENT"})
	}
	for _, p := range pairs {
		// R-01 双写幂等:组合已在台账内即视为已登记(FUNC-02 修改会重提交同一组引用)，
		// 不能走 upsertBindingTx 的 INV-16 唯一性拦截，否则集群信息维护必失败。
		dup, err := store.One(db, `SELECT id FROM clc_binding WHERE cluster_ref=? AND flag=1
			AND LOWER(target_capability)=LOWER(?) AND LOWER(target_kind)=LOWER(?) AND LOWER(purpose)=LOWER(?) AND LOWER(target_ref)=LOWER(?)`,
			clusterNo, p.cap, p.kind, p.purpose, p.ref)
		if err != nil {
			return err
		}
		if dup != nil {
			continue
		}
		if err := upsertBindingTx(db, clusterNo, p.cap, p.kind, p.ref, p.purpose, BindingSourceManual, "", user); err != nil {
			return err
		}
	}
	return nil
}

// syncBindingsFromRefsTx R-01:引用修改后与台账对齐(删除不再引用的 MANUAL 行 + 补登新引用)。
func syncBindingsFromRefsTx(db *gorm.DB, clusterNo string, refs map[string]string, user UserInfo) error {
	if err := createBindingsFromRefsTx(db, clusterNo, refs, user); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, v := range refs {
		if v != "" {
			keep[v] = true
		}
	}
	rows, err := store.List(db, `SELECT id, target_ref FROM clc_binding WHERE cluster_ref=? AND flag=1 AND binding_source=?`,
		clusterNo, BindingSourceManual)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if !keep[Str(r["target_ref"])] {
			if _, err := store.Exec(db, `UPDATE clc_binding SET flag=0, updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=?`,
				user.ID, Int(r["id"])); err != nil {
				return err
			}
		}
	}
	return nil
}

// DestroyImpact FUNC-05 影响面聚合(R-10:下游引用方聚合,强制展示;引用台账 + 节点 + 证书清单)。
func DestroyImpact(clusterNo string) (map[string]any, error) {
	nodes, err := qList(`SELECT node_no, node_role, ip_address FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status <> ?`,
		clusterNo, NodeStatusRemoved)
	if err != nil {
		return nil, err
	}
	bindings, err := qList(`SELECT binding_no, target_capability, target_kind, target_ref, purpose
		FROM clc_binding WHERE cluster_ref=? AND flag=1`, clusterNo)
	if err != nil {
		return nil, err
	}
	certs := []map[string]any{}
	for _, b := range bindings {
		if Str(b["target_capability"]) == CapabilityCert {
			certs = append(certs, b)
		}
	}
	// 下游引用方:其他集群引用台账中指向本域资源无法反查(跨域权威在他域),
	// 本域口径=本集群引用台账的全部对外引用 + 证书(销毁链需吊销)。
	return map[string]any{
		"cluster_no":    clusterNo,
		"nodes":         nodes,
		"node_count":    len(nodes),
		"bindings":      bindings,
		"binding_count": len(bindings),
		"cert_refs":     certs,
		"cert_count":    len(certs),
		"destroy_orch":  "交付工具卸载 → 吊销本域申请的基础设施证书 → 级联清理引用台账 → 发布销毁事件 → 终态(不删他域资源,R-07)",
	}, nil
}

// SaveDestroyDraft FUNC-05 保存销毁申请草稿(审批页「保存草稿」按钮;状态不变)。
func SaveDestroyDraft(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(id)
	if err != nil {
		return nil, err
	}
	if Str(c["status"]) == ClusterStatusDestroyed {
		return nil, bizRuleFail("INV-02", "集群已销毁,不可再次提交销毁申请")
	}
	if _, err := qExec(`UPDATE clc_cluster SET destroy_draft=?, destroy_reason=?,
		updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
		MarshalJSONVal(d), Str(d["reason"]), id); err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-05", fmt.Sprintf("保存集群 %s 销毁申请草稿", Str(c["cluster_no"])))
	return GetCluster(id)
}

// SubmitDestroy FUNC-05 提交销毁申请(R-10 强制不免审批;生成 FLOW-PROC-APP-01 实例,状态不变)。
func SubmitDestroy(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(id)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	status := Str(c["status"])
	if status == ClusterStatusDestroyed {
		return nil, bizRuleFail("INV-02", "集群已销毁(终态),不可重复申请")
	}
	if status == ClusterStatusDestroying {
		// DESTROYING 只有在途才拦:上次销毁任务已失败/取消时允许重新申请(R-10 仍需再次审批)
		if no, ok := destroyInFlight(clusterNo); ok {
			return nil, Fail(errcode.ErrState, "集群正在销毁中:销毁任务 %s 排队/执行中,请等待结论", no)
		}
	}
	reason := Str(d["reason"])
	if strings.TrimSpace(reason) == "" {
		return nil, Fail(errcode.ErrParam, "销毁是破坏性动作,强制原因必填(MU 屏 el17 强制原因)")
	}
	var impact map[string]any
	err = tx(func(db *gorm.DB) error {
		pending, err := store.One(db, `
			SELECT i.id FROM flow_instance i JOIN flow_definition df ON df.id = i.def_id
			WHERE df.code=? AND i.business_key=? AND i.status='RUNNING'`, flowDestroyApp, clusterNo)
		if err != nil {
			return err
		}
		if pending != nil {
			return Fail(errcode.ErrState, "该集群已有进行中的销毁审批,请等待审批完成")
		}
		impact, err = DestroyImpact(clusterNo)
		if err != nil {
			return err
		}
		definition, err := store.One(db, `SELECT * FROM flow_definition WHERE code=? AND status=1`, flowDestroyApp)
		if err != nil {
			return err
		}
		if definition == nil {
			return Fail(errcode.ErrFlowNotPublished, "集群销毁审批流程未发布")
		}
		eng := engine.NewWithInfra(db, infra.EB(), infra.WF(), infra.SLAHours(), infra.TemporalDriver())
		if _, err := eng.Start(Int(definition["id"]), clusterNo,
			[]any{"AGG-CLC-01"}, map[string]any{"cluster_id": id, "impact": impact}, user.ID); err != nil {
			return err
		}
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET destroy_reason=?, destroy_draft=NULL, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			reason, user.ID, id); err != nil {
			return err
		}
		WriteAudit(db, user.ID, user.Username, "FUNC-05", fmt.Sprintf(
			"提交集群 %s 销毁申请:影响面 节点 %d / 引用 %d / 证书 %d;原因:%s",
			clusterNo, Int(impact["node_count"]), Int(impact["binding_count"]), Int(impact["cert_count"]), reason))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetCluster(id)
}

// destroyInFlight 集群在途(QUEUED/RUNNING)DESTROY 任务(R-10 重复提交保护的唯一依据)。
// DESTROYING 且无在途 ⇒ 上次销毁已出失败/取消结论,可重新申请销毁(仍需审批)。
func destroyInFlight(clusterNo string) (string, bool) {
	row, err := qOne(`
		SELECT task_no FROM clc_task WHERE cluster_ref=? AND flag=1 AND task_kind=?
			AND task_status IN (?, ?) ORDER BY id DESC`,
		clusterNo, TaskKindDestroy, TaskStatusQueued, TaskStatusRunning)
	if err != nil || row == nil {
		return "", false
	}
	return Str(row["task_no"]), true
}

// execPathOf 系统创建任务(审批回调,无人在场选链路)的执行位置 = 集群登记事实:绑定了点火中心
// 执行机 ⇒ 远程链路(O-IGN 侧用户裁定 Q-71:全生命周期动作一律经交付门面单步下发,销毁也不例外);
// 未绑定 ⇒ 本地链路(创建向导「不使用执行机」的同源登记,不是失联兜底)。
func execPathOf(cluster map[string]any) string {
	if strings.TrimSpace(Str(cluster["executor_ref"])) != "" {
		return ExecPathRemoteExecutor
	}
	return ExecPathLocal
}

// execPathByClusterNo 只有集群编号时的同源推导(节点动作:从节点行取所属集群)。
func execPathByClusterNo(clusterNo string) string {
	row, err := qOne(`SELECT executor_ref FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
	if err != nil || row == nil {
		return ExecPathLocal
	}
	return execPathOf(row)
}

// SyncClusterDestroyFromFlow 审批结果 → 销毁链(FLOW-PROC-APP-01 A07 → PROC-05)。
// 通过:INV-33 审批通过结论为前置 ⇒ 创建 DESTROY 任务(QUEUED)+ 集群置 DESTROYING;
// R-07 销毁编排由交付内核承载(卸载成功后进 destroySuccessLanding:级联清理 → DESTROYED);
// 本回调在审批事务内,不抢执行权,排队由后台看护按序推进(R-18)。
// 驳回:状态不变,意见留痕。
func SyncClusterDestroyFromFlow(db *gorm.DB, businessKey string, flowStatus string) error {
	c, err := store.One(db, `SELECT * FROM clc_cluster WHERE cluster_no = ? AND flag = 1`, businessKey)
	if err != nil || c == nil {
		return err
	}
	switch flowStatus {
	case "APPROVED":
		if Str(c["status"]) == ClusterStatusDestroying {
			// 上次销毁已出失败结论:同一份审批通过结论可再次排队(INV-33);在途时不重复创建(R-10)
			if no, ok := destroyInFlight(businessKey); ok {
				return Fail(errcode.ErrState, "销毁任务 %s 排队/执行中,不重复创建", no)
			}
		} else if err := checkLifecycleTransition(Str(c["status"]), ClusterStatusDestroying); err != nil {
			return err
		}
		user := UserInfo{ID: 0, Username: "system"}
		if _, err := createTaskTx(db, businessKey, TaskKindDestroy, execPathOf(c), user); err != nil {
			return err
		}
		if Str(c["status"]) != ClusterStatusDestroying {
			if _, err := store.Exec(db, `
				UPDATE clc_cluster SET status=?, updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
				WHERE id=? AND flag=1`, ClusterStatusDestroying, user.ID, Int(c["id"])); err != nil {
				return err
			}
		}
		WriteAudit(db, 0, "system", "FUNC-31", fmt.Sprintf(
			"销毁审批通过:集群 %s 状态 → DESTROYING,DESTROY 任务已排队(INV-33 审批前置为唯一依据);销毁编排由交付内核执行(R-07)", businessKey))
	case "REJECTED":
		WriteAudit(db, 0, "system", "FUNC-32", fmt.Sprintf("销毁审批驳回:集群 %s 状态不变,意见留痕", businessKey))
	}
	return nil
}

// DESTROY 成功落点(FLOW-PROC-05 / R-07):交付工具卸载有结论后级联清理本域台账 → 终态 DESTROYED。
// 不删他域资源(R-07);节点台账随集群终局保留行(INV-13 口径:终局不物理删)。
func init() {
	taskSuccessHooks[TaskKindDestroy] = destroySuccessLanding
}

func destroySuccessLanding(db *gorm.DB, task, cluster map[string]any, _ DeliveryArgs, user UserInfo) error {
	clusterNo := Str(task["cluster_ref"])
	if err := finishTaskTx(db, task, TaskStatusSucceeded, nil,
		"销毁编排完成:交付工具卸载有结论,级联清理本域台账(R-07)", user); err != nil {
		return err
	}
	if cluster == nil {
		return nil
	}
	bindings, err := store.Count(db, `SELECT COUNT(*) FROM clc_binding WHERE cluster_ref=? AND flag=1`, clusterNo)
	if err != nil {
		return err
	}
	if _, err := store.Exec(db, `
		UPDATE clc_binding SET flag=0, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE cluster_ref=? AND flag=1`, clusterNo); err != nil {
		return err
	}
	if _, err := store.Exec(db, `
		UPDATE clc_param SET flag=0, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE cluster_ref=? AND flag=1`, clusterNo); err != nil {
		return err
	}
	if err := checkLifecycleTransition(Str(cluster["status"]), ClusterStatusDestroyed); err != nil {
		return err
	}
	if _, err := store.Exec(db, `
		UPDATE clc_cluster SET status=?, health=?, destroyed_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'),
			updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=?`,
		ClusterStatusDestroyed, HealthUnknown, user.ID, Int(cluster["id"])); err != nil {
		return err
	}
	WriteAudit(db, user.ID, user.Username, "FUNC-31", fmt.Sprintf(
		"集群 %s 销毁落点(R-07):状态 → DESTROYED(终态保留行);级联清理引用台账 %d 项与参数上下文;发布销毁事件;不删他域资源",
		clusterNo, bindings))
	publishClcEvent("clc.cluster.destroyed", map[string]any{
		"cluster_no": clusterNo, "destroyed_at": nowText(),
	})
	return nil
}

// ExplicitRecover FUNC-16 显式恢复(R-11:仅 DEGRADED 可调用;重绑定/确认双路径)。
// 重绑定:new_ref 非空 → 更新指定能力引用(双写)+ 派生参数 + 清除降级;
// 确认:资源已恢复 → 直接清除降级并留痕。
func ExplicitRecover(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(id)
	if err != nil {
		return nil, err
	}
	if Str(c["health"]) != HealthDegraded {
		return nil, bizRuleFail("R-11", "仅 DEGRADED(已降级)集群可调用显式恢复;当前健康 %s", Str(c["health"]))
	}
	clusterNo := Str(c["cluster_no"])
	mode := Str(d["mode"])
	if mode != "rebind" && mode != "confirm" {
		return nil, Fail(errcode.ErrParam, "恢复方式只能取 rebind(重绑定)或 confirm(确认)")
	}
	err = tx(func(db *gorm.DB) error {
		if mode == "rebind" {
			newRef := Str(d["new_target_ref"])
			capability := Str(d["target_capability"])
			kind := Str(d["target_kind"])
			if newRef == "" || capability == "" || kind == "" {
				return Fail(errcode.ErrParam, "重绑定恢复必须提供新资源标识与目标能力/资源类型")
			}
			if err := checkBindingCombo(capability, kind); err != nil {
				return err
			}
			// R-11:新引用必须存在且可用 —— 本域落地口径:非空 + 能力/类型组合合法 + 不与现存活引用重复
			if dup, err := store.One(db, `SELECT id FROM clc_binding WHERE cluster_ref=? AND flag=1
				AND LOWER(target_capability)=LOWER(?) AND LOWER(target_kind)=LOWER(?) AND LOWER(target_ref)=LOWER(?)`,
				clusterNo, capability, kind, newRef); err != nil {
				return err
			} else if dup != nil {
				return bizRuleFail("R-11", "新引用与现有引用重复(组合唯一),请检查资源标识")
			}
			if err := upsertBindingTx(db, clusterNo, capability, kind, newRef, Str(d["purpose"]), BindingSourceManual, "FUNC-16 显式恢复重绑定", user); err != nil {
				return err
			}
			// R-01 双写:内联列同步
			if err := writeInlineRefTx(db, clusterNo, capability, kind, newRef); err != nil {
				return err
			}
			// R-04:引用变更 → 派生参数 + 快照
			if err := deriveParamsTx(db, clusterNo, inlineRefsOf(db, clusterNo), user); err != nil {
				return err
			}
		}
		// 清除降级:健康转健康并留痕(INV-06:DEGRADED 只能被显式恢复清除)
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET health=?, degraded_reason=NULL, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			HealthHealthy, user.ID, id); err != nil {
			return err
		}
		return createSnapshotTx(db, clusterNo, "显式恢复:"+map[string]string{"rebind": "重绑定引用", "confirm": "确认恢复"}[mode], SnapshotTypeChange, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-16", fmt.Sprintf("集群 %s 显式恢复(%s):降级原因清除,健康 → HEALTHY", clusterNo, mode))
	publishClcEvent("clc.cluster.recovered", map[string]any{"cluster_no": clusterNo, "mode": mode})
	return GetCluster(id)
}

// writeInlineRefTx R-01:按能力/类型把引用写回集群内联列。
func writeInlineRefTx(db *gorm.DB, clusterNo, capability, kind, ref string) error {
	var col string
	switch {
	case capability == CapabilityIgnition && kind == TargetKindExecutor:
		col = "executor_ref"
	case capability == CapabilityIgnition && kind == TargetKindVersion:
		col = "version_ref"
	case capability == CapabilityRegistry && kind == TargetKindInstance:
		col = "registry_ref"
	case capability == CapabilityRegistry && kind == TargetKindProject:
		col = "registry_project_ref"
	default:
		return nil // CERT/ASSET 引用仅入台账,无内联列
	}
	_, err := store.Exec(db, fmt.Sprintf(`UPDATE clc_cluster SET %s=? WHERE cluster_no=? AND flag=1`, col), ref, clusterNo)
	return err
}

// inlineRefsOf 读当前内联引用(事务内)。
func inlineRefsOf(db *gorm.DB, clusterNo string) map[string]string {
	row, err := store.One(db, `SELECT version_ref, executor_ref, registry_ref, registry_project_ref FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
	if err != nil || row == nil {
		return map[string]string{}
	}
	return map[string]string{
		"version_ref": Str(row["version_ref"]), "executor_ref": Str(row["executor_ref"]),
		"registry_ref": Str(row["registry_ref"]), "registry_project_ref": Str(row["registry_project_ref"]),
	}
}

// StatisticsOverview FUNC-29 统计总览(REP-11;运行观测页)。
func StatisticsOverview() (map[string]any, error) {
	out := map[string]any{}
	c, err := qOne(`
		SELECT COUNT(*) AS total,
			SUM(CASE WHEN status = 'RUNNING' THEN 1 ELSE 0 END) AS running,
			SUM(CASE WHEN health = 'DEGRADED' THEN 1 ELSE 0 END) AS degraded,
			SUM(CASE WHEN health = 'UNREACHABLE' THEN 1 ELSE 0 END) AS unreachable,
			SUM(CASE WHEN status = 'DESTROYED' THEN 1 ELSE 0 END) AS destroyed
		FROM clc_cluster WHERE flag = 1`)
	if err != nil {
		return nil, err
	}
	for k, v := range c {
		out[k] = Int(v)
	}
	if p, err := qOne(`SELECT COUNT(*) AS n FROM clc_param WHERE flag=1 AND sync_pending = 1`); err == nil {
		out["sync_pending"] = Int(p["n"])
	}
	if pr, err := qOne(`
		SELECT COUNT(*) AS total, SUM(CASE WHEN probe_result = 'FAILED' THEN 1 ELSE 0 END) AS failed
		FROM (SELECT probe_result FROM clc_probe WHERE flag = 1 ORDER BY id DESC LIMIT 100) recent`); err == nil {
		total, failed := Int(pr["total"]), Int(pr["failed"])
		out["probe_total"] = total
		out["probe_failed"] = failed
		if total > 0 {
			out["probe_fail_rate"] = fmt.Sprintf("%.1f%%", float64(failed)/float64(total)*100)
		} else {
			out["probe_fail_rate"] = "-"
		}
	}
	// 挂起吊销证书:销毁链中吊销失败挂起的 DESTROY 任务(残留显式可见,R-07)
	if t, err := qCount(`SELECT COUNT(*) FROM clc_task WHERE flag=1 AND task_kind=? AND task_status IN (?, ?)`,
		TaskKindDestroy, TaskStatusRunning, TaskStatusFailed); err == nil {
		out["revocation_pending"] = t
	}
	if s, err := qCount(`SELECT COUNT(*) FROM clc_snapshot WHERE flag = 1`); err == nil {
		out["snapshot_total"] = s
	}
	return out, nil
}
