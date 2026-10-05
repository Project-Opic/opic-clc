// Package business —— 集群域(clc)控制器。
//
// 接口一律 OPIC 信封(common.OKJSON/FailJSON);
// 写接口经 middleware.RequirePermission("PERM-FUNC-XX") 挂载 m5 行为级权限;
// 审计统一走 service.WriteAudit(无返回值,直接调用,勿用 _ =)。
package business

import (
	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ---------- 集群(AGG-CLC-01) ----------

// ClcClusterList GET /api/clc/cluster (FUNC-19)
func ClcClusterList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListClusters(page, size, map[string]any{
		"status": c.Query("status"), "run_mode": c.Query("run_mode"),
		"health": c.Query("health"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcClusterGet GET /api/clc/cluster/:id (FUNC-20)
func ClcClusterGet(c *gin.Context) {
	row, err := service.GetCluster(common.PathID(c, "id"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "")
}

// ClcClusterCreate POST /api/clc/cluster (FUNC-01;四步向导提交)
func ClcClusterCreate(c *gin.Context) {
	row, err := service.CreateCluster(common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "创建成功(集群+节点+引用+首快照)")
}

// ClcClusterUpdate PUT /api/clc/cluster/:id (FUNC-02)
func ClcClusterUpdate(c *gin.Context) {
	row, err := service.UpdateCluster(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "修改成功")
}

// ClcDestroyImpact GET /api/clc/cluster/:id/destroy-impact (FUNC-05 影响面)
func ClcDestroyImpact(c *gin.Context) {
	row, err := service.GetCluster(common.PathID(c, "id"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	impact, err := service.DestroyImpact(service.Str(row["cluster_no"]))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, impact, "")
}

// ClcDestroyDraft PUT /api/clc/cluster/:id/destroy-draft (FUNC-05 保存草稿)
func ClcDestroyDraft(c *gin.Context) {
	row, err := service.SaveDestroyDraft(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "草稿已保存")
}

// ClcDestroySubmit POST /api/clc/cluster/:id/destroy-submit (FUNC-05 提交申请)
func ClcDestroySubmit(c *gin.Context) {
	row, err := service.SubmitDestroy(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "销毁申请已提交(审批通过前状态不变,R-10 强制不免审批)")
}

// ClcRecover POST /api/clc/cluster/:id/recover (FUNC-16 显式恢复)
func ClcRecover(c *gin.Context) {
	row, err := service.ExplicitRecover(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "显式恢复完成(降级清除)")
}

// ClcStatistics GET /api/clc/statistics (FUNC-29)
func ClcStatistics(c *gin.Context) {
	out, err := service.StatisticsOverview()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "")
}

// ClcCrossClusters GET /api/clc/cross/clusters (FUNC-28;CLC-ROLE-05 服务身份)
func ClcCrossClusters(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.CrossClusterList(page, size, map[string]any{
		"status": c.Query("status"), "cluster_no": c.Query("cluster_no"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ---------- 节点(AGG-CLC-02) ----------

// ClcNodeList GET /api/clc/node (FUNC-21)
func ClcNodeList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListNodes(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "node_role": c.Query("node_role"),
		"node_status": c.Query("node_status"), "node_health": c.Query("node_health"),
		"keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcNodeAdd POST /api/clc/cluster/:id/node (FUNC-06)
func ClcNodeAdd(c *gin.Context) {
	row, err := service.AddNode(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "节点已添加")
}

// ClcNodeRemove POST /api/clc/node/:id/remove (FUNC-07)
func ClcNodeRemove(c *gin.Context) {
	row, err := service.RemoveNode(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "节点已移除(终局保留行)")
}

// ClcNodeRebind POST /api/clc/node/:id/rebind (FUNC-08)
func ClcNodeRebind(c *gin.Context) {
	row, err := service.RebindNode(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "节点已重绑定")
}

// ---------- 引用与参数(AGG-CLC-03/04) ----------

// ClcBindingList GET /api/clc/binding (FUNC-22)
func ClcBindingList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListBindings(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "target_capability": c.Query("target_capability"),
		"purpose": c.Query("purpose"), "binding_source": c.Query("binding_source"),
		"keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcBindingAdjust POST /api/clc/cluster/:id/binding (FUNC-13)
func ClcBindingAdjust(c *gin.Context) {
	row, err := service.AdjustBinding(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "引用已调整(双写台账与内联列)")
}

// ClcParamList GET /api/clc/param (FUNC-23)
func ClcParamList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListParams(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "param_source": c.Query("param_source"),
		"sync_pending": c.Query("sync_pending"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcParamOverride POST /api/clc/cluster/:id/param (FUNC-14)
func ClcParamOverride(c *gin.Context) {
	row, err := service.OverrideParam(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "参数已覆写(留痕)")
}

// ClcParamSync POST /api/clc/cluster/:id/param/sync (FUNC-15)
func ClcParamSync(c *gin.Context) {
	row, err := service.ConfirmParamSync(common.PathID(c, "id"), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "参数同步已确认执行")
}

// ---------- 快照(AGG-CLC-05) ----------

// ClcSnapshotList GET /api/clc/snapshot (FUNC-24)
func ClcSnapshotList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListSnapshots(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "snapshot_type": c.Query("snapshot_type"),
		"keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcSnapshotCreate POST /api/clc/cluster/:id/snapshot (FUNC-17)
func ClcSnapshotCreate(c *gin.Context) {
	row, err := service.CreateSnapshot(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "快照已创建")
}

// ClcSnapshotRollback POST /api/clc/snapshot/:id/rollback (FUNC-18)
func ClcSnapshotRollback(c *gin.Context) {
	row, err := service.RollbackSnapshot(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "回滚完成(已产生新快照)")
}

// ---------- 任务(AGG-CLC-06) ----------

// ClcTaskList GET /api/clc/task (FUNC-25)
func ClcTaskList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListTasks(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "task_kind": c.Query("task_kind"),
		"task_status": c.Query("task_status"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcTaskAction POST /api/clc/cluster/:id/task (FUNC-03/04/09/10/11/12 交付动作)
func ClcTaskAction(c *gin.Context) {
	body := common.BindJSON(c)
	clusterID := common.PathID(c, "id")
	kind := common.Trim(body["task_kind"])
	var (
		row map[string]any
		err error
	)
	switch kind {
	case "INSTALL":
		row, err = service.StartInstall(clusterID, body, common.UserOf(c))
	case "RETRY_INSTALL":
		row, err = service.RetryInstall(clusterID, body, common.UserOf(c))
	case "UPGRADE":
		row, err = service.StartUpgrade(clusterID, body, common.UserOf(c))
	case "BACKUP":
		row, err = service.StartBackup(clusterID, body, common.UserOf(c))
	case "RESTORE":
		row, err = service.StartRestore(clusterID, body, common.UserOf(c))
	case "START", "STOP":
		row, err = service.StartStop(clusterID, body, common.UserOf(c))
	default:
		common.FailJSON(c, service.Fail(errcode.ErrParam, "任务类别「%s」不支持(可选 INSTALL/RETRY_INSTALL/UPGRADE/BACKUP/RESTORE/START/STOP)", kind))
		return
	}
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "交付动作已执行")
}

// ---------- 探活(AGG-CLC-07) ----------

// ClcProbeList GET /api/clc/probe (FUNC-26)
func ClcProbeList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListProbes(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "probe_result": c.Query("probe_result"),
		"from": c.Query("from"), "to": c.Query("to"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcProbeRun POST /api/clc/cluster/:id/probe (FUNC-26 人工探活)
func ClcProbeRun(c *gin.Context) {
	row, err := service.RunProbe(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "探活已登记")
}

// ClcAggregateHealth GET /api/clc/health/aggregate (FUNC-27)
func ClcAggregateHealth(c *gin.Context) {
	rows, err := service.AggregateHealth(map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "status": c.Query("status"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"list": rows, "total": len(rows)}, "")
}

// ---------- 平台参数(FUNC-30) ----------

// ClcParamsGet GET /api/clc/params (FUNC-30)
func ClcParamsGet(c *gin.Context) {
	rows, err := service.ListPlatformParams()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, rows, "")
}

// ClcParamsSave PUT /api/clc/params (FUNC-30)
func ClcParamsSave(c *gin.Context) {
	body := common.BindJSON(c)
	items := map[string]string{}
	if m, ok := body["items"].(map[string]any); ok {
		for k, v := range m {
			items[k] = common.Trim(v)
		}
	}
	rows, err := service.SavePlatformParams(items, common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, rows, "运行参数已保存")
}
