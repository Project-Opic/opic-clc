// Package business —— 集群域(clc)控制器。
//
// 接口一律 OPIC 信封(common.OKJSON/FailJSON);
// 写接口经 middleware.RequirePermission("PERM-FUNC-XX") 挂载 m5 行为级权限;
// 审计统一走 service.WriteAudit(无返回值,直接调用,勿用 _ =)。
package business

import (
	"encoding/json"
	"strconv"
	"time"

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
	// ⚠ 交付任务台账走 clc_task(ListClusterTasks)；service.ListTasks 是 flow_task 审批任务
	p, err := service.ListClusterTasks(page, size, map[string]any{
		"cluster_ref": c.Query("cluster_ref"), "task_kind": c.Query("task_kind"),
		"task_status": c.Query("task_status"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// clcTaskActionKinds 交付动出入口受理的 task_kind 集:与下方 ClcTaskAction 的 switch 分支一一对应
// (DEF-06 行为级分档门的作用域;新增受理分支必须同步补这一份,否则新类别会绕过第二道门)。
var clcTaskActionKinds = map[string]bool{
	service.TaskKindInstall:      true,
	service.TaskKindRetryInstall: true,
	service.TaskKindUpgrade:      true,
	service.TaskKindBackup:       true,
	service.TaskKindRestore:      true,
	service.TaskKindStart:        true,
	service.TaskKindStop:         true,
}

// ClcTaskAction POST /api/clc/cluster/:id/task (FUNC-03/04/09/10/11/12 交付动作)。
//
// DEF-06 两道门:路由级 perm("PERM-FUNC-03")(routes.go:84)是「一道门管一族动作」的粗粒度门(入口档,
// 发起交付动作的总门),只保证调用者能进这个口;但本 handler 按 task_kind 分派七个不同行为
// (FUNC-03/04/09/10/11/12),m2/m5 对每一档行为各自发牌 —— 一条码挂七个行为 = 权限分档丢失,
// 持 03 无 09~12 的账号本不该能发起升级/备份/恢复/启停。所以两道门刻意叠加:入口门在中间件,
// 行为档在本 handler 调用任何服务层入口(即任何副作用:任务行/审计/事件/交付门面)之前复查。
// kind→权限码在服务层从运行时本体注册表两级派生(m2 postconditions 显式着陆 × requiredPermissions
// 反查并对 m5 校验,见 service/clc_task.go lookupTaskKindPerm;解析不出一律拒绝,Go 里无码表副本)。
// 受理集之外的取值仍先走下方 default 分支的「不支持」参数报错,既有口径逐字不变。
func ClcTaskAction(c *gin.Context) {
	body := common.BindJSON(c)
	clusterID := common.PathID(c, "id")
	kind := common.Trim(body["task_kind"])
	var (
		row map[string]any
		err error
	)
	if clcTaskActionKinds[kind] {
		if err := service.CheckTaskKindPermission(common.UserOf(c), kind); err != nil {
			common.FailJSON(c, err)
			return
		}
	}
	switch kind {
	case service.TaskKindInstall:
		row, err = service.StartInstall(clusterID, body, common.UserOf(c))
	case service.TaskKindRetryInstall:
		row, err = service.RetryInstall(clusterID, body, common.UserOf(c))
	case service.TaskKindUpgrade:
		row, err = service.StartUpgrade(clusterID, body, common.UserOf(c))
	case service.TaskKindBackup:
		row, err = service.StartBackup(clusterID, body, common.UserOf(c))
	case service.TaskKindRestore:
		row, err = service.StartRestore(clusterID, body, common.UserOf(c))
	case service.TaskKindStart, service.TaskKindStop:
		row, err = service.StartStop(clusterID, body, common.UserOf(c))
	default:
		common.FailJSON(c, service.Fail(errcode.ErrParam, "任务类别「%s」不支持(可选 INSTALL/RETRY_INSTALL/UPGRADE/BACKUP/RESTORE/START/STOP)", kind))
		return
	}
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "交付动作已受理,任务排队执行")
}

// ---------- 交付执行内核(FUNC-33/34/35,v1.1) ----------

// ClcTaskCancel POST /api/clc/task/:no/cancel (FUNC-33;取消 ≠ 回滚,R-15)
func ClcTaskCancel(c *gin.Context) {
	reason := common.Trim(common.BindJSON(c)["reason"])
	if reason == "" {
		common.FailJSON(c, service.Fail(errcode.ErrParam, "取消必须显式填写原因(审计留痕)"))
		return
	}
	row, err := service.CancelClusterTask(c.Param("no"), reason, common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "任务已取消(取消不是回滚,已执行步骤的效果由交付工具幂等性承担)")
}

// ClcTaskSteps GET /api/clc/task/:no/steps (FUNC-34 → REP-12 c1~c9;无整表原始日志正文,INV-40)
func ClcTaskSteps(c *gin.Context) {
	rows, err := service.ListTaskSteps(c.Param("no"), map[string]any{
		"step_status": c.Query("step_status"), "verdict_source": c.Query("verdict_source"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	common.OKJSON(c, gin.H{"list": rows, "total": len(rows)}, "")
}

// ClcTaskLogs GET /api/clc/task/:no/logs (FUNC-35 → REP-13;游标增量,禁整表拉取,INV-43)
func ClcTaskLogs(c *gin.Context) {
	taskNo := c.Param("no")
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	rows, err := service.ReadTaskLogs(taskNo, offset, limit, map[string]any{
		"log_level": c.Query("log_level"), "step_index": c.Query("step_index"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	for _, r := range rows {
		if ln := service.Int(r["line_no"]); ln > offset {
			offset = ln
		}
	}
	status, _, err := service.TaskLogSnapshot(taskNo)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{
		"list": rows, "total": len(rows), "offset": offset, "task_status": status,
	}, "")
}

// sseReplayBatch SSE 历史补差单批行数(与 ReadTaskLogs 上限口径一致)。
const sseReplayBatch = 2000

// ClcTaskLogsStream GET /api/clc/task/:no/logs/stream?offset=N (FUNC-35 SSE;
// 先补历史再追增量,终态任务一次性回放至末尾后停止 —— REP-13 交付形态)。
func ClcTaskLogsStream(c *gin.Context) {
	taskNo := c.Param("no")
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	params := map[string]any{"log_level": c.Query("log_level"), "step_index": c.Query("step_index")}
	status, _, err := service.TaskLogSnapshot(taskNo)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	if status == "" {
		common.FailJSON(c, service.NotFound("任务"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Connection", "keep-alive")

	// 订阅先行:补历史期间的实时行经游标去重,不漏不重(INV-43)
	unsub, ch := service.SubscribeTaskLogs(taskNo)
	defer unsub()

	sendEvent := func(event string, data any) bool {
		raw, jerr := json.Marshal(data)
		if jerr != nil {
			return false
		}
		if _, werr := c.Writer.WriteString("event: " + event + "\ndata: " + string(raw) + "\n\n"); werr != nil {
			return false
		}
		c.Writer.Flush()
		return c.Request.Context().Err() == nil
	}
	sendBatch := func(rows []map[string]any) (alive bool, advanced bool) {
		for _, r := range rows {
			ln := service.Int(r["line_no"])
			if ln <= offset {
				continue
			}
			offset = ln
			advanced = true
			if !sendEvent("log", r) {
				return false, advanced
			}
		}
		return true, advanced
	}
	// 1) 先补历史(offset = 已读最大日志序号)
	for {
		rows, rerr := service.ReadTaskLogs(taskNo, offset, sseReplayBatch, params)
		if rerr != nil {
			sendEvent("error", gin.H{"message": rerr.Error()})
			return
		}
		if len(rows) == 0 {
			break
		}
		alive, _ := sendBatch(rows)
		if !alive {
			return
		}
		if int64(len(rows)) < sseReplayBatch {
			break
		}
	}
	reloaded, _, rerr := service.TaskLogSnapshot(taskNo)
	if rerr == nil {
		status = reloaded
	}
	if clcTaskTerminal(status) {
		sendEvent("done", gin.H{"task_status": status, "cursor": offset})
		return
	}

	// 2) 再追增量:hub 广播 + 周期补差(慢消费者丢行由游标续查兜底)+ 终局检测
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return // 断开即释放 hub 订阅(defer unsub,转发协程随 close 退出,无泄漏)
		case row, open := <-ch:
			if !open {
				return
			}
			if alive, _ := sendBatch([]map[string]any{row}); !alive {
				return
			}
		case <-ticker.C:
			rows, rerr2 := service.ReadTaskLogs(taskNo, offset, sseReplayBatch, params)
			if rerr2 == nil {
				alive, _ := sendBatch(rows)
				if !alive {
					return
				}
			}
			st, _, err2 := service.TaskLogSnapshot(taskNo)
			if err2 == nil {
				status = st
			}
			if clcTaskTerminal(status) {
				sendEvent("done", gin.H{"task_status": status, "cursor": offset})
				return
			}
			if _, werr := c.Writer.WriteString(": ping\n\n"); werr == nil {
				c.Writer.Flush()
			}
		}
	}
}

// clcTaskTerminal 非 QUEUED / RUNNING 即终局(REP-13:终态任务一次性回放至末尾后停止)。
func clcTaskTerminal(status string) bool {
	return status != service.TaskStatusQueued && status != service.TaskStatusRunning
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
