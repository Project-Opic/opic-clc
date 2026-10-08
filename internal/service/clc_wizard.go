// clc_wizard.go —— 交付向导步进 / 跳步 / 生命周期台动作派发的服务入口
// (m2 FUNC-44 / FUNC-45 / FUNC-46,AGG-CLC-06;权限 PERM-FUNC-44/45/46)。
//
// 本文件只做「入口」:前置校验 → 规则校验 → 状态变更 → 联动派发,领域终态一律由交付内核
// (clc_delivery.go FLOW-PROC-06)触达,不在本层伪造。三个行为的口径(m2 为准):
//
//	FUNC-44 向导步进执行与单步重试 —— STEP 任务只有队列中首个 PENDING 步可派发(INV-50);
//	        同集群有步在跑 ⇒ 置灰不谎报排队(R-23);槽位占满仍 QUEUED(REF-14);派发经
//	        B-AUTO-10 单步循环下发后停下等人工(不连跑);重试 = 新建 RETRY_INSTALL 任务
//	        (INV-31 原任务行不动),已成功序号按 R-17 置 SKIPPED;续跑 stepMode 按入口取
//	        (向导「重试本步」⇒ STEP,编排屏续跑仍 AUTO,REF-17)。R-19 请求体口径不改。
//	FUNC-45 向导跳步 —— 只作用 PENDING 步;事由必填并累加写任务头 operationReason(R-24);
//	        被跳步置 SKIPPED 且填跳过原因 / 结束时间 / 判定依据(INV-38,终态不回退 INV-41);
//	        后续失败归因须体现跳过事实(由内核 skipAttributionTx 承载,本层不重写);
//	        集群健康不因跳步预置(R-08 双源,本层绝不碰 clc_cluster)。
//	FUNC-46 生命周期台动作派发 —— 动作集只取 CLC_TASK_KIND 既有着陆
//	        (START/STOP/UPGRADE/BACKUP/RESTORE/CERT_RENEW),不新增枚举;INV-02 生命周期合法性、
//	        INV-48/R-18 同集群在途互斥(排队不失败)、REF-16 链路可用性由内核校验;
//	        高危(停止/恢复)二次确认 + 事由必填(R-24),销毁仍只走 PROC-APP-01(FUNC-05),
//	        其余高危不进流程引擎(「确认 + 事由 + 留痕」)。
package service

import (
	"fmt"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// wizardStepVerdictBasis 跳步判定依据(INV-38 终态必填;R-24 归因可读的落点之一)。
// 与内核续跑展开的 R-17 固定文本 skipReasonResume 区分:那是「上次已成功不重跑」,
// 这里是「人工决定不执行」,失败归因只把后者计入「人工跳过」注记。
const wizardStepVerdictBasis = "人工跳步(FUNC-45/R-24)"

// lifecycleActionKinds FUNC-46 生命周期台动作集 = CLC_TASK_KIND 既有着陆中可直派的子集。
// 不新增枚举值(INV-28);INSTALL/RETRY_INSTALL/ADD_NODE/DEL_NODE 有各自入口,不在此列;
// DESTROY 只经审批流(FUNC-05/PROC-APP-01),永不从本口直派。
var lifecycleActionKinds = map[string]bool{
	TaskKindStart: true, TaskKindStop: true, TaskKindUpgrade: true,
	TaskKindBackup: true, TaskKindRestore: true, TaskKindCertRenew: true,
}

// highRiskActionKinds 本口直派的高危动作(m2 FUNC-46:停止 / 恢复;销毁另经审批流,不入此列)。
// 高危 ⇒ 二次确认 + 事由必填(R-24),但「不进流程引擎」—— 确认 + 事由 + 审计留痕即可。
var highRiskActionKinds = map[string]bool{
	TaskKindStop: true, TaskKindRestore: true,
}

// taskTerminalStatuses 任务终态(INV-29;终态任务向导不再接受任何派发/跳步)。
var taskTerminalStatuses = map[string]bool{
	TaskStatusSucceeded: true, TaskStatusFailed: true,
	TaskStatusCancelled: true, TaskStatusStalled: true,
}

// WizardStepAdvance FUNC-44「执行本步」:对 STEP 任务派发队列中首个 PENDING 步骤,
// 下发一步后即停下等人工(B-AUTO-10 单步循环 + m6 A13 人工确认点)。
//
// 派发动作只是「登记一次性人工授权」并把任务交给内核(引擎侧 STEP 门控 + parkStepTask
// 承载「跑一步即停」),真正的步骤状态回写/日志/成功失败落点全部由内核完成,本层不伪造。
// requestedStep 为界面期望派发的步骤序号(0 = 不指定,直接派发当前首个可执行步);
// 指定值与首个 PENDING 不一致即按 INV-50 拒绝,并把「当前可执行的是第几步」如实回给人工。
func WizardStepAdvance(taskNo string, requestedStep int, user UserInfo) (map[string]any, error) {
	task, err := qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, NotFound("任务")
	}
	// REF-17:步进派发只对 STEP 任务有意义;AUTO 任务由内核连跑,无需人工逐次授权。
	if taskStepMode(task) != StepModeStep {
		return nil, Fail(errcode.ErrState,
			"任务 %s 为连跑模式(AUTO),由交付内核一次跑完,不存在「执行本步」的人工派发(REF-17;步进派发仅适用于向导 STEP 任务)", taskNo)
	}
	kind := Str(task["task_kind"])
	clusterNo := Str(task["cluster_ref"])
	status := Str(task["task_status"])
	if taskTerminalStatuses[status] {
		return nil, bizRuleFail("INV-29", "任务已处于终态(%s),向导不再接受派发(不可逆)", status)
	}
	// R-23:本任务正有一步在跑 ⇒ 按钮置灰,而不是谎报「排队中」。
	if status == TaskStatusRunning {
		return nil, bizRuleFail("R-23", "当前步骤正在执行中,本步按钮应置灰而非谎报排队;请待该步结论落定后再接收下一次派发(INV-48 同集群单步在执行)")
	}
	// INV-48:同集群另有运行中任务 ⇒ 同样置灰(R-23 口径),不伪造排队。
	if other, err := qOne(`SELECT task_no FROM clc_task WHERE cluster_ref=? AND flag=1 AND task_status=? AND task_no<>? LIMIT 1`,
		clusterNo, TaskStatusRunning, taskNo); err != nil {
		return nil, err
	} else if other != nil {
		return nil, bizRuleFail("INV-48", "同集群 %s 已有运行中的任务 %s,单步派发须待其结束(R-23 置灰口径,不谎报排队)",
			clusterNo, Str(other["task_no"]))
	}
	// INV-50 / INV-39:只有队列中首个 PENDING(最小非终态序号)步可派发。
	first, err := nextRunnableStep(taskNo)
	if err != nil {
		return nil, err
	}
	if first == nil {
		return nil, Fail(errcode.ErrState,
			"任务 %s 已无待执行步骤(全部步骤均已有结论);若链路未竟功,请按失败步骤索引发起单步重试(INV-31 新建续跑任务,不重跑本任务)", taskNo)
	}
	fpIdx := int(paramInt(first["step_index"]))
	fpName := Str(first["step_name"])
	if requestedStep > 0 && requestedStep != fpIdx {
		row, err := qOne(`SELECT step_index, step_name, step_status FROM clc_task_step
			WHERE task_ref=? AND step_index=? AND flag=1`, taskNo, requestedStep)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, NotFound(fmt.Sprintf("步骤 %02d", requestedStep))
		}
		if stepTerminalStatuses[Str(row["step_status"])] {
			return nil, bizRuleFail("INV-50", "步骤 %02d(%s)已处于终态(%s),不得再次派发;当前可执行的是第 %02d 步(%s)(STEP 模式仅队列中首个 PENDING 可派发,INV-39)",
				requestedStep, Str(row["step_name"]), Str(row["step_status"]), fpIdx, fpName)
		}
		return nil, bizRuleFail("INV-50", "只能派发队列中首个 PENDING 步骤:当前可执行的是第 %02d 步(%s),第 %02d 步(%s)须待其之前各步结论落定后才轮到(INV-39 单步推进)",
			fpIdx, fpName, requestedStep, Str(row["step_name"]))
	}
	// 派发:登记一次性人工步进授权后交内核;授权在「本步结论落定 → parkStepTask」时失效,
	// 绝不跨步骤留存。槽位占满 / 竞态未抢到执行权时任务保持 QUEUED(REF-14/R-18 排队不失败),
	// 由 janitor 依序推进(promoteQueued 对持授权的 STEP 任务放行)。
	rt := deliveryRT()
	rt.setStepIntent(taskNo, kind, clusterNo)
	if !rt.tryStart(taskNo) {
		stepIdx := fpIdx
		writeTaskLog(taskNo, &stepIdx, delivery.LevelInfo,
			fmt.Sprintf("「执行本步」已受理(第 %02d 步 %s):执行槽位或同集群互斥暂未取得,任务保持排队 QUEUED(REF-14/R-18),授权留存待推进", fpIdx, fpName))
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-44",
		fmt.Sprintf("向导步进派发:任务 %s 第 %02d 步(%s)已登记一次性人工授权并交交付内核单步下发(B-AUTO-10,下发后即停在人工确认点)", taskNo, fpIdx, fpName))
	return qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
}

// WizardStepRetry FUNC-44「重试本步」:新建 RETRY_INSTALL 续跑任务(INV-31 原任务行不改写),
// 已成功序号按 R-17 在创建事务内展开为 SKIPPED;向导入口缺省 stepMode=STEP(REF-17),
// 与编排屏发起的连跑续跑(FUNC-04,缺省 AUTO)区分。校验与领域落点全部复用 RetryInstall,
// 本层只负责把「向导入口 ⇒ STEP」这一创建口径显式登记。
func WizardStepRetry(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	if d == nil {
		d = map[string]any{}
	}
	// REF-17:向导上的「重试本步」= STEP;不改原任务(INV-31),新建续跑任务在创建事务内展开步骤行。
	d["step_mode"] = StepModeStep
	out, err := RetryInstall(clusterID, d, user)
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-44",
		fmt.Sprintf("向导单步重试:集群 %s 新建 RETRY_INSTALL 续跑任务(stepMode=STEP,已成功序号 R-17 置 SKIPPED,原任务行 INV-31 不动)",
			Str(out["cluster_no"])))
	return out, nil
}

// WizardStepSkip FUNC-45「跳过此步」:只作用尚未执行(PENDING)的步骤,事由必填并累加写任务头
// operationReason(R-24);被跳步置 SKIPPED 并填跳过原因 / 结束时间 / 判定依据(INV-38,终态不回退 INV-41)。
//
// 一处都不碰 clc_cluster:集群健康仍由 R-08 双源写入,跳步可能让集群「运行但缺件」,
// 为界面好看预先置健康即违背 R-08。失败归因体现跳过事实由内核 skipAttributionTx 承载。
func WizardStepSkip(taskNo string, stepIndex int, reason string, user UserInfo) (map[string]any, error) {
	task, err := qOne(`SELECT task_no, task_status FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, NotFound("任务")
	}
	if taskTerminalStatuses[Str(task["task_status"])] {
		return nil, bizRuleFail("INV-29", "任务已处于终态(%s),不再接受跳步", Str(task["task_status"]))
	}
	// R-24:事由必填,代价留在数据上(不可省,否则跳步无从归因)。
	reason = trimEmpty(reason)
	if reason == "" {
		return nil, bizRuleFail("R-24", "跳过步骤必须填写事由(跳步意味着哪一项配置未做须留痕,随任务头 operationReason 累加)")
	}
	if stepIndex < 1 {
		return nil, Fail(errcode.ErrParam, "跳步须指定步骤序号(自 1 起),收到 %d", stepIndex)
	}
	if err := tx(func(db *gorm.DB) error {
		row, err := txOne(db, `SELECT step_index, step_status FROM clc_task_step WHERE task_ref=? AND step_index=? AND flag=1`, taskNo, stepIndex)
		if err != nil {
			return err
		}
		if row == nil {
			return NotFound(fmt.Sprintf("步骤 %02d", stepIndex))
		}
		st := Str(row["step_status"])
		// INV-41 / INV-50:终态步不回退,更不得借「跳步」再次派发。
		if stepTerminalStatuses[st] {
			return bizRuleFail("INV-41", "步骤 %02d 已处于终态(%s),不可跳步(终局不回退;终态步亦不得再次派发 INV-50)", stepIndex, st)
		}
		// INV-37:跳步只作用未执行(PENDING)步;运行中的步由内核持有,不在此处置。
		if st != StepStatusPending {
			return bizRuleFail("INV-37", "跳步只作用于未执行(PENDING)的步骤,第 %02d 步当前为 %s", stepIndex, st)
		}
		// 置 SKIPPED:跳过原因=人工事由(INV-38 SKIPPED 必填),判定依据=人工跳步口径,结束时间由 markStepTx 落。
		if err := markStepTx(db, taskNo, stepIndex, StepStatusSkipped, wizardStepVerdictBasis, nil, "", reason, nil); err != nil {
			return err
		}
		// 事由累加写任务头(R-24;后写不覆盖先写,step_mode 不参与 UPDATE,INV-50 只读)。
		return applyTaskOperationReasonTx(db, taskNo, reason, user)
	}); err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-45",
		fmt.Sprintf("向导跳步:任务 %s 第 %02d 步置 SKIPPED(事由:%s;已累加写任务头 operationReason,集群健康不因跳步预置 R-08)", taskNo, stepIndex, reason))
	return qOne(`SELECT step_index, step_code, step_name, step_status, verdict_source, skip_reason, started_at, ended_at
		FROM clc_task_step WHERE task_ref=? AND step_index=? AND flag=1`, taskNo, stepIndex)
}

// DispatchLifecycleAction FUNC-46 生命周期台动作派发:动作集全部取 CLC_TASK_KIND 既有着陆,
// 不新增类别值;销毁仍唯一经审批流 PROC-APP-01(FUNC-05),其余高危动作「确认 + 事由 + 留痕」直派、
// 不进流程引擎。派发一律委托 runClusterAction(QUEUED + 创建事务内展开单步 R-17/INV-45,
// 互斥/链路/生命周期合法性由内核与既有校验承载),本层只做动作集合法性、行为级权限分档(DEF-06)与高危前置。
func DispatchLifecycleAction(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	if d == nil {
		d = map[string]any{}
	}
	kind := trimEmpty(Str(d["task_kind"]))
	// INV-28:动作集只能是既有枚举(先过字典命中,再谈本口是否受理)。
	if err := checkTaskKind(kind); err != nil {
		return nil, err
	}
	// 销毁:唯一经审批流(INV-33/R-10),本口永不直派 DESTROY。
	if kind == TaskKindDestroy {
		return nil, bizRuleFail("INV-33", "销毁(DESTROY)须先通过唯一审批流 PROC-APP-01:请经集群销毁入口(FUNC-05)提交销毁申请,本生命周期台口不直派销毁动作")
	}
	if !lifecycleActionKinds[kind] {
		return nil, Fail(errcode.ErrParam,
			"动作「%s」不属于生命周期台派发集(既有类别中可直派:START/STOP/UPGRADE/BACKUP/RESTORE/CERT_RENEW;安装类与节点增删各有专用入口)", kind)
	}
	// DEF-06 第二道门:路由级 perm("PERM-FUNC-46")(routes.go:116)只到「生命周期台派发」这一粗粒度档,
	// 本口按 task_kind 复查该动作自己的 FUNC 档(START/STOP⇒12、UPGRADE⇒09、BACKUP⇒10、RESTORE⇒11、
	// CERT_RENEW⇒46;kind→码两级派生:m2 postconditions 显式着陆 + 注册表 requiredPermissions 反查,
	// 派生与校验都在 clc_task.go lookupTaskKindPerm,Go 里没有码表副本)。
	// 位置在字典命中/DESTROY 拒绝/派发集受理之后、任何副作用之前:无任务行、无审计、无事件、不碰交付门面。
	// 上面三道既有分支的报错口径逐字不变(表外类别根本走不到这里,故既有 INV-33/ErrParam 文案不受影响)。
	if err := CheckTaskKindPermission(user, kind); err != nil {
		return nil, err
	}
	// R-24:高危动作(停止 / 恢复)二次确认 + 事由必填;销毁已在上分支拒绝,不入此列。
	if highRiskActionKinds[kind] {
		reason := trimEmpty(Str(d["operation_reason"]))
		if reason == "" {
			return nil, bizRuleFail("R-24", "高危动作(%s)必须填写事由(代价留在数据上,随任务头 operationReason 留痕)", kind)
		}
		if !truthy(d["confirmed"]) {
			return nil, bizRuleFail("R-24", "高危动作(%s)需要二次确认(confirmed=true)后方可派发(不进流程引擎,以确认 + 事由 + 审计留痕承担)", kind)
		}
	}
	var (
		out map[string]any
		err error
	)
	switch kind {
	case TaskKindStart, TaskKindStop:
		out, err = StartStop(clusterID, d, user)
	case TaskKindUpgrade:
		out, err = StartUpgrade(clusterID, d, user)
	case TaskKindBackup:
		out, err = StartBackup(clusterID, d, user)
	case TaskKindRestore:
		out, err = StartRestore(clusterID, d, user)
	case TaskKindCertRenew:
		out, err = startCertRenew(clusterID, d, user)
	}
	if err != nil {
		return nil, err
	}
	if highRiskActionKinds[kind] {
		WriteAudit(nil, user.ID, user.Username, "FUNC-46",
			fmt.Sprintf("高危生命周期动作二次确认派发:集群 %s 动作 %s,事由:%s(不进流程引擎,以确认 + 事由 + 审计留痕承担,R-24)",
				Str(out["cluster_no"]), kind, trimEmpty(Str(d["operation_reason"]))))
	}
	return out, nil
}

// startCertRenew 控制面证书续期(m2 FUNC-46 生命周期台的显式登记项)。
// 走既有 CERT_RENEW 枚举,不新增类别;前置状态 RUNNING(INV-02 生命周期合法性);
// 成功后落点由 taskSuccessHooks[CERT_RENEW] 承担(交付工具汇总判定;运行状态由探活刷新,R-08)。
func startCertRenew(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	return runClusterAction(clusterID, TaskKindCertRenew, d, user, func(db *gorm.DB, c map[string]any, task map[string]any) error {
		if s := Str(c["status"]); s != ClusterStatusRunning {
			return Fail(errcode.ErrState, "控制面证书续期的前置状态为 RUNNING(INV-02 生命周期状态合法性),当前 %s", s)
		}
		return nil
	})
}

// init FUNC-46:CERT_RENEW 的成功落点既有枚举着陆(m1 DICT-CLC_TASK_KIND 已含证书推送项),
// 无额外领域落点 ⇒ 仅任务成功终局。
func init() {
	taskSuccessHooks[TaskKindCertRenew] = simpleLanding("控制面证书续期完成(交付工具汇总判定;运行状态由探活刷新,R-08)")
}
