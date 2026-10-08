// clc_wizard.go —— 交付向导步进 / 跳步 / 生命周期台动作派发控制器
// （FUNC-44 / FUNC-45 / FUNC-46，AGG-CLC-06；m5 权限码 PERM-FUNC-44 / PERM-FUNC-45 / PERM-FUNC-46）。
//
// 与其余业务接口同一信封（OKJSON/FailJSON）；不变量中文提示全部在服务层给出，
// 本层只做入参搬运与权限挂载。派发是 COMMAND（改任务/步骤聚合内状态），
// 领域终态由交付内核异步回写，接口只回执「已受理」与当前台账视图。
package business

import (
	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
)

// ClcWizardStepExecute POST /api/clc/task/:no/step-execute (FUNC-44「执行本步」)。
// body.step_index 可选:界面期望派发的步骤序号;缺省或与队列首个 PENDING 不符时由服务层按 INV-50 处置。
func ClcWizardStepExecute(c *gin.Context) {
	body := common.BindJSON(c)
	row, err := service.WizardStepAdvance(c.Param("no"), int(service.Int(body["step_index"])), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "本步已受理:交付内核单步下发后即停在人工确认点(REF-17),未跑完的步骤不会被自动推进")
}

// ClcWizardStepRetry POST /api/clc/cluster/:id/wizard-retry (FUNC-44「重试本步」)。
// 新建 RETRY_INSTALL 续跑任务(stepMode=STEP,已成功序号 R-17 置 SKIPPED);原任务行 INV-31 不动。
func ClcWizardStepRetry(c *gin.Context) {
	row, err := service.WizardStepRetry(common.PathID(c, "id"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "单步重试已受理:已新建续跑任务(INV-31 原任务不改写),已成功序号置 SKIPPED 不重跑(R-17)")
}

// ClcWizardStepSkip POST /api/clc/task/:no/step-skip (FUNC-45「跳过此步」)。
// body:step_index(必填,自 1 起)+ reason(必填,事由累加写任务头 operationReason,R-24)。
func ClcWizardStepSkip(c *gin.Context) {
	body := common.BindJSON(c)
	row, err := service.WizardStepSkip(
		c.Param("no"),
		int(service.Int(body["step_index"])),
		common.Trim(body["reason"]),
		common.UserOf(c),
	)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "已跳步:该步置 SKIPPED 并登记跳过原因/结束时间/判定依据(终态不回退 INV-41);集群健康不因跳步预置(R-08)")
}

// ClcLifecycleAction POST /api/clc/cluster/:id/lifecycle-action (FUNC-46 生命周期台动作派发)。
// body:task_kind(START/STOP/UPGRADE/BACKUP/RESTORE/CERT_RENEW)+ operation_reason + confirmed(高危必填)。
// 销毁(DESTROY)不在此直派 —— 唯一经审批流 PROC-APP-01(FUNC-05)。
//
// DEF-06 两道门:路由级 perm("PERM-FUNC-46")(routes.go:116)只到「生命周期台派发」这一粗粒度档;
// 本 handler 在进入服务层任何副作用之前按 task_kind 复查该动作自己的 FUNC 档(kind→码由本体
// 注册表两级派生,见 service.clc_task.go lookupTaskKindPerm,Go 无码表副本;入口 03 与行为档 09~12
// 叠加是刻意分档,不是重复校验)。字典外/派发集外的取值
// 仍由服务层既有分支(INV-28 / INV-33 / ErrParam)承担,报错口径逐字不变。
func ClcLifecycleAction(c *gin.Context) {
	body := common.BindJSON(c)
	if kind := common.Trim(body["task_kind"]); kind != "" {
		if _, ok := service.TaskKindPermCode(kind); ok {
			if err := service.CheckTaskKindPermission(common.UserOf(c), kind); err != nil {
				common.FailJSON(c, err)
				return
			}
		}
	}
	row, err := service.DispatchLifecycleAction(common.PathID(c, "id"), body, common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, row, "生命周期动作已受理:任务排队 QUEUED,单步动作展开为一行由交付内核执行(R-17/INV-45)")
}
