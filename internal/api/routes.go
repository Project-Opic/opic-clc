// Package api —— Gin 路由注册(gopherforge internal/api 模式:公开组 + 登录组 + 权限中间件按路由挂载)。
//
// 路由表与重构前(hertz router/v1)逐条一致,API 契约不变;
// 集群域业务路由(/api/clc/*)写操作按 m5 行为级权限挂载。
package api

import (
	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/adminconsole"
	"gitcode.com/opic-ontology/opic-techbase/internal/api/auth"
	"gitcode.com/opic-ontology/opic-techbase/internal/api/business"
	"gitcode.com/opic-ontology/opic-techbase/internal/api/flow"
	"gitcode.com/opic-ontology/opic-techbase/internal/api/meta"
	"gitcode.com/opic-ontology/opic-techbase/internal/api/system"
	"gitcode.com/opic-ontology/opic-techbase/internal/middleware"
)

// SetupRoutes 挂载 /api 路由。
func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// ---- 公开路由(认证入口) ----
	pub := api.Group("")
	{
		pub.POST("/auth/login", auth.Login)
		pub.GET("/auth/mode", auth.AuthMode)
		pub.GET("/auth/captcha", auth.Captcha)
		pub.GET("/auth/login-url", auth.ZitadelLoginURL)
		pub.GET("/auth/callback", auth.ZitadelCallback)
		// 用户工作台 SSO（OPIC-SSO-01：Casdoor）
		pub.GET("/auth/casdoor/login-url", auth.CasdoorLoginURL)
		pub.GET("/auth/casdoor/callback", auth.CasdoorCallback)
	}

	// ---- 登录路由组 ----
	authed := api.Group("", middleware.LoginRequired())
	{
		// 认证
		authed.POST("/auth/logout", auth.Logout)
		authed.GET("/auth/info", auth.Info)

		// 元数据(登录即可)
		authed.GET("/meta/dictionaries", meta.Dictionaries)
		authed.POST("/assistant/chat", business.AssistantChat)
		authed.GET("/assistant/models", business.AssistantModels)
		authed.POST("/assistant/sql", business.AssistantSQL)
		authed.GET("/meta/rules", meta.Rules)

		// 写操作权限挂载助手(RequirePermission 包装)
		perm := func(code string, handler gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{middleware.RequirePermission(code), handler}
		}

		// ---- 集群域业务(/api/clc/*;写操作按 m5 行为级权限挂载) ----
		// 集群(AGG-CLC-01)
		authed.GET("/clc/cluster", perm("PERM-FUNC-19", business.ClcClusterList)...)
		authed.GET("/clc/cluster/:id", perm("PERM-FUNC-20", business.ClcClusterGet)...)
		authed.POST("/clc/cluster", perm("PERM-FUNC-01", business.ClcClusterCreate)...)
		authed.PUT("/clc/cluster/:id", perm("PERM-FUNC-02", business.ClcClusterUpdate)...)
		authed.GET("/clc/cluster/:id/destroy-impact", perm("PERM-FUNC-05", business.ClcDestroyImpact)...)
		authed.PUT("/clc/cluster/:id/destroy-draft", perm("PERM-FUNC-05", business.ClcDestroyDraft)...)
		authed.POST("/clc/cluster/:id/destroy-submit", perm("PERM-FUNC-05", business.ClcDestroySubmit)...)
		authed.POST("/clc/cluster/:id/recover", perm("PERM-FUNC-16", business.ClcRecover)...)
		authed.GET("/clc/statistics", perm("PERM-FUNC-29", business.ClcStatistics)...)
		authed.GET("/clc/cross/clusters", perm("PERM-FUNC-28", business.ClcCrossClusters)...)
		// 节点(AGG-CLC-02)
		authed.GET("/clc/node", perm("PERM-FUNC-21", business.ClcNodeList)...)
		authed.POST("/clc/cluster/:id/node", perm("PERM-FUNC-06", business.ClcNodeAdd)...)
		authed.POST("/clc/node/:id/remove", perm("PERM-FUNC-07", business.ClcNodeRemove)...)
		authed.POST("/clc/node/:id/rebind", perm("PERM-FUNC-08", business.ClcNodeRebind)...)
		// 引用与参数(AGG-CLC-03/04)
		authed.GET("/clc/binding", perm("PERM-FUNC-22", business.ClcBindingList)...)
		authed.POST("/clc/cluster/:id/binding", perm("PERM-FUNC-13", business.ClcBindingAdjust)...)
		authed.GET("/clc/param", perm("PERM-FUNC-23", business.ClcParamList)...)
		authed.POST("/clc/cluster/:id/param", perm("PERM-FUNC-14", business.ClcParamOverride)...)
		authed.POST("/clc/cluster/:id/param/sync", perm("PERM-FUNC-15", business.ClcParamSync)...)
		// 快照(AGG-CLC-05)
		authed.GET("/clc/snapshot", perm("PERM-FUNC-24", business.ClcSnapshotList)...)
		authed.POST("/clc/cluster/:id/snapshot", perm("PERM-FUNC-17", business.ClcSnapshotCreate)...)
		authed.POST("/clc/snapshot/:id/rollback", perm("PERM-FUNC-18", business.ClcSnapshotRollback)...)
		// 任务(AGG-CLC-06)
		authed.GET("/clc/task", perm("PERM-FUNC-25", business.ClcTaskList)...)
		// DEF-06:本行只挂 PERM-FUNC-03 是「入口档」(发起交付动作的总门),不是行为档 ——
		// ClcTaskAction 按 task_kind 分派 FUNC-03/04/09/10/11/12 七个行为,m2/m5 对每档行为各自发牌,
		// 一条码挂七个行为=分档丢失;行为档(UPGRADE 需 09、启停需 12 等)在 handler 内、任何副作用
		// 之前由第二道门按注册表派生的 kind→权限码复查(business/clc.go + service/clc_task.go),两道刻意叠加。
		authed.POST("/clc/cluster/:id/task", perm("PERM-FUNC-03", business.ClcTaskAction)...)
		// 交付执行内核(FUNC-33/34/35,v1.1;SSE 与日志回放同源权限)
		authed.POST("/clc/task/:no/cancel", perm("PERM-FUNC-33", business.ClcTaskCancel)...)
		authed.GET("/clc/task/:no/steps", perm("PERM-FUNC-34", business.ClcTaskSteps)...)
		authed.GET("/clc/task/:no/logs", perm("PERM-FUNC-35", business.ClcTaskLogs)...)
		authed.GET("/clc/task/:no/logs/stream", perm("PERM-FUNC-35", business.ClcTaskLogsStream)...)
		// 探活(AGG-CLC-07)
		authed.GET("/clc/probe", perm("PERM-FUNC-26", business.ClcProbeList)...)
		authed.POST("/clc/cluster/:id/probe", perm("PERM-FUNC-26", business.ClcProbeRun)...)
		authed.GET("/clc/health/aggregate", perm("PERM-FUNC-27", business.ClcAggregateHealth)...)
		// 平台参数(FUNC-30)
		authed.GET("/clc/params", business.ClcParamsGet)
		authed.PUT("/clc/params", perm("PERM-FUNC-30", business.ClcParamsSave)...)
		// 集群模板(AGG-CLC-08,FUNC-36~43;v1.2「集群模板台」frmClcTemplate)
		// 注:克隆挂在 /clc/template-clone 而非 /clc/template/clone —— gin 同一方法树里
		// 静态段与 :no 参数段同级会 panic,这是树形路由的硬约束,不是命名偏好。
		authed.GET("/clc/template", perm("PERM-FUNC-36", business.ClcTemplateList)...)
		authed.GET("/clc/template/:no", perm("PERM-FUNC-37", business.ClcTemplateGet)...)
		authed.POST("/clc/template", perm("PERM-FUNC-38", business.ClcTemplateCreate)...)
		authed.PUT("/clc/template/:no/groups", perm("PERM-FUNC-39", business.ClcTemplateGroupsSave)...)
		authed.POST("/clc/template/:no/enable", perm("PERM-FUNC-40", business.ClcTemplateEnable)...)
		authed.POST("/clc/template/:no/disable", perm("PERM-FUNC-40", business.ClcTemplateDisable)...)
		authed.POST("/clc/template/:no/default", perm("PERM-FUNC-41", business.ClcTemplateSetDefault)...)
		authed.DELETE("/clc/template/:no", perm("PERM-FUNC-42", business.ClcTemplateDelete)...)
		authed.POST("/clc/template-clone", perm("PERM-FUNC-43", business.ClcTemplateClone)...)

		// 向导步进 / 跳步 / 生命周期台派发(AGG-CLC-06,FUNC-44/45/46;v1.2「交付向导台 frmClcWizard / 生命周期台」)
		// 注:步进与跳步挂 /clc/task/:no/...(与 FUNC-33/34/35 同在 :no 子树,静态段末级不冲突);
		// 重试与生命周期派发挂 /clc/cluster/:id/...(与 :id 下既有静态段同级末级不同,不触发 gin 静态/参数同级 panic)。
		authed.POST("/clc/task/:no/step-execute", perm("PERM-FUNC-44", business.ClcWizardStepExecute)...)
		authed.POST("/clc/cluster/:id/wizard-retry", perm("PERM-FUNC-44", business.ClcWizardStepRetry)...)
		authed.POST("/clc/task/:no/step-skip", perm("PERM-FUNC-45", business.ClcWizardStepSkip)...)
		authed.POST("/clc/cluster/:id/lifecycle-action", perm("PERM-FUNC-46", business.ClcLifecycleAction)...)

		// 只读代理与前置预检(AGG-CLC-01/02,FUNC-47/48/49/50;v1.2「节点登录标识 / 交付配置字段级视图 / 原文只读视图 / 交付前置预检」)
		// 注:四条都是 GET,挂在既有 :id 子树末级静态段,与步进 / 生命周期台共用 :id 骨架不冲突
		// (gin 静态段与 :id 同级 panic 的坑只在同方法树混合时才触发,本批四条均为 GET,新增段名互不相同)。
		authed.GET("/clc/cluster/:id/node-login-identities", perm("PERM-FUNC-47", business.ClcNodeLoginIdentities)...)
		authed.GET("/clc/cluster/:id/ign-config", perm("PERM-FUNC-48", business.ClcIgnDeliveryConfig)...)
		authed.GET("/clc/cluster/:id/ign-config-raw", perm("PERM-FUNC-49", business.ClcIgnDeliveryConfigRaw)...)
		authed.GET("/clc/cluster/:id/delivery-readiness", perm("PERM-FUNC-50", business.ClcDeliveryReadinessCheck)...)
		// 交付执行日志代理查看(FUNC-51 / REP-19,v1.3 Q-125)—— :ref 就是任务头 remoteTaskRef,
		// 集群标签由服务层反查(留痕要"哪个集群"),故路由不按 :id 编址:按 ref 编址才是本报表的入参口径
		// (REP-19 p1 必填、无集群参数)。挂在 /clc/ 静态段下,与 :id / :no 子树不同级,不触发 gin 静态-参数同级 panic。
		authed.GET("/clc/exec-log/:ref", perm("PERM-FUNC-51", business.ClcRemoteExecLog)...)

		// 审批中心(登录即可;归属校验在服务层)
		authed.GET("/workbench/todo", business.WorkbenchTodo)
		authed.GET("/workbench/done", business.WorkbenchDone)
		authed.GET("/workbench/requested", business.WorkbenchRequested)
		authed.POST("/workbench/todo/:id/approve", business.WorkbenchApprove)
		authed.POST("/workbench/todo/:id/reject", business.WorkbenchReject)
		authed.POST("/workbench/todo/:id/return", business.WorkbenchReturn)

		// 流程查询(登录即可)
		authed.GET("/flow/definitions", flow.DefinitionList)
		authed.GET("/flow/definitions/:id", flow.DefinitionGet)
		authed.GET("/flow/definitions/:id/graph", flow.DefinitionGraph)
		authed.GET("/flow/instances", flow.InstanceList)
		authed.GET("/flow/instances/:id", flow.InstanceGet)
		authed.GET("/flow/tasks", flow.TaskList)

		// ---- 管理控制台补齐(gopherforge 基准):日志审计/在线用户/公告/错误码/系统监控 ----
		admin := api.Group("/admin", middleware.LoginRequired(), middleware.OperationAudit())
		{
			g := admin.Group("", middleware.RequirePermission("system:manage"))
			adminconsole.RegisterTemporalFlow(g)
			g.GET("/llm-config", adminconsole.LLMConfigGet)
			g.PUT("/llm-config", adminconsole.LLMConfigSave)
			// AI 模型设置:多供应商账号管理
			g.GET("/ai/catalog", adminconsole.AICatalog)
			g.GET("/ai/accounts", adminconsole.AIAccountList)
			g.POST("/ai/accounts", adminconsole.AIAccountCreate)
			g.PUT("/ai/accounts/:id", adminconsole.AIAccountUpdate)
			g.DELETE("/ai/accounts/:id", adminconsole.AIAccountDelete)
			g.POST("/ai/accounts/:id/test", adminconsole.AIAccountTest)
			g.GET("/ai/upstream-models", adminconsole.AIUpstreamModels)
			g.GET("/isolation", adminconsole.IsolationInfo)
			g.GET("/logs/operation", adminconsole.OperationLogs)
			g.GET("/logs/login", adminconsole.LoginLogs)
			g.GET("/logs/audit", adminconsole.AuditLogs)
			g.GET("/online-users", adminconsole.OnlineUsers)
			g.GET("/notice", adminconsole.NoticeList)
			g.POST("/notice", adminconsole.NoticeSave)
			g.PUT("/notice/:id", adminconsole.NoticeSave)
			g.DELETE("/notice/:id", adminconsole.NoticeDelete)
			g.GET("/errcodes", adminconsole.ErrCodes)
			g.GET("/system-stats", adminconsole.SystemStats)
		}

		// 系统查询(登录即可)
		authed.GET("/users", system.UserList)
		authed.GET("/users/options", system.UserOptions)
		authed.GET("/roles", system.RoleList)
		authed.GET("/permissions", system.PermissionList)
		authed.GET("/permissions/all", system.PermissionAll)
		authed.GET("/resources", system.ResourceList)
		authed.GET("/resources/tree", system.ResourceTree)

		// 用户管理
		authed.POST("/users", perm("system:user:add", system.UserCreate)...)
		authed.PUT("/users/:id", perm("system:user:edit", system.UserUpdate)...)
		authed.DELETE("/users/:id", perm("system:user:delete", system.UserDelete)...)
		authed.PUT("/users/:id/roles", perm("system:user:assign-role", system.UserAssignRoles)...)
		authed.PUT("/users/:id/reset-pwd", perm("system:user:reset-pwd", system.UserResetPwd)...)

		// 角色管理
		authed.POST("/roles", perm("system:role:add", system.RoleCreate)...)
		authed.PUT("/roles/:id", perm("system:role:edit", system.RoleUpdate)...)
		authed.DELETE("/roles/:id", perm("system:role:delete", system.RoleDelete)...)
		authed.PUT("/roles/:id/permissions", perm("system:role:assign", system.RoleAssignPermissions)...)
		authed.PUT("/roles/:id/resources", perm("system:role:assign", system.RoleAssignResources)...)

		// 权限管理
		authed.POST("/permissions", perm("system:permission:add", system.PermissionCreate)...)
		authed.PUT("/permissions/:id", perm("system:permission:edit", system.PermissionUpdate)...)
		authed.DELETE("/permissions/:id", perm("system:permission:delete", system.PermissionDelete)...)

		// 资源管理
		authed.POST("/resources", perm("system:resource:add", system.ResourceCreate)...)
		authed.PUT("/resources/:id", perm("system:resource:edit", system.ResourceUpdate)...)
		authed.DELETE("/resources/:id", perm("system:resource:delete", system.ResourceDelete)...)

		// 流程管理写操作
		authed.POST("/flow/definitions", perm("flow:definition:add", flow.DefinitionCreate)...)
		authed.PUT("/flow/definitions/:id", perm("flow:definition:edit", flow.DefinitionUpdate)...)
		authed.POST("/flow/definitions/:id/publish", perm("flow:definition:publish", flow.DefinitionPublish)...)
		authed.PUT("/flow/instances/:id/terminate", perm("flow:instance:terminate", flow.InstanceTerminate)...)
		authed.PUT("/flow/tasks/:id/transfer", perm("flow:task:transfer", flow.TaskTransfer)...)
		authed.PUT("/flow/tasks/:id/urge", perm("flow:task:urge", flow.TaskUrge)...)
	}
}
