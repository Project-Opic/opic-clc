// clc_wizard_pg_test.go —— v1.2 向导步进 / 跳步 / 生命周期台派发（FUNC-44 / FUNC-45 / FUNC-46）
// 服务入口语义用例（依赖临时 PG 装置）。断言只匹配错误文案关键片段（编号 / 规则号是硬事实）。
//
// 覆盖不变量：INV-50（STEP 仅首个 PENDING 可派发、终态不可再派 / 不可跳）、REF-17（步进缺省按入口、
// AUTO 不接管步进）、R-23（置灰不谎报排队）、INV-48（同集群在途互斥）、REF-14/R-18（占满排队）、
// INV-31（重试新建任务、原任务行不动）、R-17（已成功序号 SKIPPED）、INV-38/INV-41（跳步终态判定依据、
// 不回退）、R-24（跳步事由必填 + 失败归因体现跳过）、R-08（跳步不预置健康）、
// INV-02/INV-28（生命周期合法性、动作集只取既有枚举）、INV-33（销毁唯一经审批流）、不进流程引擎。
package service

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------- FUNC-44 步进派发门控（INV-50 / REF-17 / R-23） ----------

// TestWizardStepAdvanceGate 断言「只派发队列首个 PENDING、其余一律拒绝并点名当前可执行步」，
// 以及终态步不可再派（INV-50）、AUTO 任务不存在步进派发（REF-17）。本用例全程走拒绝路径，不触发内核。
func TestWizardStepAdvanceGate(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, ClusterStatusDraft)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	mustSQL(t, `UPDATE clc_task SET step_mode=? WHERE task_no=?`, StepModeStep, taskNo)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	// 队列首个 PENDING 是第 01 步：请求第 03 步即拒，并回指「当前可执行的是第 01 步」（INV-50/INV-39）。
	_, err := WizardStepAdvance(taskNo, 3, testUser)
	tplWantErr(t, err, "INV-50", "第 01 步")

	// 01 已成功、02 为续跑跳过（R-17 固定文本）⇒ 首个 PENDING 变为第 03 步。
	if e := setStep(t, taskNo, 1, StepStatusSucceeded, "退出码 0", "", "", nil); e != nil {
		t.Fatalf("置 01 成功失败: %v", e)
	}
	if e := setStep(t, taskNo, 2, StepStatusSkipped, "", "", skipReasonResume, nil); e != nil {
		t.Fatalf("置 02 跳过失败: %v", e)
	}
	// 已 SUCCEEDED 的 01 不可再派（INV-50 终态不回退 / 不再派发）。
	_, err = WizardStepAdvance(taskNo, 1, testUser)
	tplWantErr(t, err, "INV-50", "已处于终态", "SUCCEEDED")
	// 已 SKIPPED 的 02 不可再派。
	_, err = WizardStepAdvance(taskNo, 2, testUser)
	tplWantErr(t, err, "INV-50", "已处于终态", "SKIPPED")
	// 首个 PENDING 现在是 03；请求 04（仍 PENDING、但非首个）⇒ 拒绝并点名第 03 步。
	_, err = WizardStepAdvance(taskNo, 4, testUser)
	tplWantErr(t, err, "INV-50", "第 03 步")

	// AUTO 任务：连跑模式不存在「执行本步」人工派发（REF-17）。
	autoNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	_, err = WizardStepAdvance(autoNo, 0, testUser)
	tplWantErr(t, err, "连跑模式", "REF-17")
}

// TestWizardStepAdvanceRunsFirstPendingAndParks 断言成功派发只跑「首个 PENDING」一步后即停在
// 人工确认点（B-AUTO-10 单步 + m6 A13）：任务回到 QUEUED、下一步仍 PENDING、一次性授权已失效。
func TestWizardStepAdvanceRunsFirstPendingAndParks(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusDraft)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	mustSQL(t, `UPDATE clc_task SET step_mode=? WHERE task_no=?`, StepModeStep, taskNo)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	if _, err := WizardStepAdvance(taskNo, 0, testUser); err != nil {
		t.Fatalf("执行本步失败: %v", err)
	}
	// 首个 PENDING（第 01 步）被推成 SUCCEEDED。
	waitStepStatus(t, taskNo, 1, StepStatusSucceeded)
	// 任务停靠回排队态等下一次派发（parkStepTask），不是连跑到底。
	if row := waitTaskStatuses(t, taskNo, TaskStatusQueued); Str(row["task_status"]) != TaskStatusQueued {
		t.Fatalf("STEP 派发后应停回 QUEUED 等人工(REF-17/A13): %+v", row)
	}
	// 第 02 步未被自动推进（停下等人工，绝不连跑）。
	if s := Str(mustStepRow(t, taskNo, 2)["step_status"]); s != StepStatusPending {
		t.Fatalf("未派发前第 02 步须仍 PENDING(不连跑),实为 %s", s)
	}
	// 一次性授权在结论落定时失效（clearStepIntent），跨步骤不留存。
	if rt.hasStepIntent(taskNo) {
		t.Fatal("单步结论落定后步进授权必须失效(clearStepIntent),不得跨步骤留存")
	}
	// 全部步骤未跑完，任务不该 SUCCEEDED。
	waitAllSlotsFree(t, rt)
	if got := len(stepsByStatus(t, taskNo, StepStatusSucceeded)); got != 1 {
		t.Fatalf("一次派发只推进一步(B-AUTO-10),得到 %d", got)
	}
}

// TestWizardStepRetryNewTaskAndOriginalUnchanged 断言单步重试 = 新建 RETRY_INSTALL 任务
// （INV-31 原任务行逐字不动）、已成功序号创建即展开为 SKIPPED（R-17）、向导入口缺省 STEP /
// 编排屏续跑缺省 AUTO（REF-17）。
func TestWizardStepRetryNewTaskAndOriginalUnchanged(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusInstallFailed)
	id := Int(mustClusterRow(t, clusterNo)["id"])
	mustSQL(t, `UPDATE clc_cluster SET install_step_index=3 WHERE cluster_no=? AND flag=1`, clusterNo)

	// 原任务（INSTALL, FAILED, STEP）——快照整行，重试后须逐字不变（INV-31）。
	origNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusFailed, ExecPathLocal)
	mustSQL(t, `UPDATE clc_task SET step_mode=?, fail_step_index=3, result_summary='第 3 步失败' WHERE task_no=?`, StepModeStep, origNo)
	before := mustOne(t, `SELECT * FROM clc_task WHERE task_no=?`, origNo)

	if _, err := WizardStepRetry(id, map[string]any{}, testUser); err != nil {
		t.Fatalf("向导单步重试失败: %v", err)
	}
	nos := taskNosOf(t, clusterNo)
	if len(nos) != 2 {
		t.Fatalf("重试应新增 1 个续跑任务(原任务行保留),得到 %d 个", len(nos))
	}
	retryNo := nos[1]
	retry := mustOne(t, `SELECT * FROM clc_task WHERE task_no=?`, retryNo)
	if Str(retry["task_kind"]) != TaskKindRetryInstall {
		t.Fatalf("重试须新建 RETRY_INSTALL 续跑任务(INV-31),得到 %s", Str(retry["task_kind"]))
	}
	if Str(retry["step_mode"]) != StepModeStep {
		t.Fatalf("向导「重试本步」入口 stepMode 缺省须为 STEP(REF-17),得到 %s", Str(retry["step_mode"]))
	}
	// STEP 无授权 ⇒ 停在排队态等「执行本步」，绝不自动连跑。
	if s := Str(retry["task_status"]); s != TaskStatusQueued {
		t.Fatalf("向导重试任务应排队等人工派发(STEP 无授权不开跑),得到 %s", s)
	}
	// R-17：创建事务内即展开步骤行，已成功的前 2 序号 SKIPPED（固定文本），3..7 PENDING。
	if got := len(stepsByStatus(t, retryNo, StepStatusSkipped)); got != 2 {
		t.Fatalf("R-17:已成功的前 2 步应展开为 SKIPPED,得到 %d", got)
	}
	for _, idx := range []int{1, 2} {
		row := mustStepRow(t, retryNo, idx)
		if Str(row["step_status"]) != StepStatusSkipped || Str(row["skip_reason"]) != skipReasonResume {
			t.Fatalf("R-17:第 %d 步跳过原因须为续跑固定文本,得到 %+v", idx, row)
		}
	}
	if got := len(stepsByStatus(t, retryNo, StepStatusPending)); got != 5 {
		t.Fatalf("第 3~7 步应待执行 PENDING,得到 %d", got)
	}
	// 原任务行逐字不变（INV-31 任务只增不改）。
	after := mustOne(t, `SELECT * FROM clc_task WHERE task_no=?`, origNo)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("INV-31:原任务行不得被重试改写\n前=%v\n后=%v", before, after)
	}

	// 编排屏续跑入口（RetryInstall 不带 step_mode）缺省 AUTO（REF-17；v1.1 连跑口径逐字不变）。
	clusterNo2 := seedCluster(t, ClusterStatusInstallFailed)
	id2 := Int(mustClusterRow(t, clusterNo2)["id"])
	mustSQL(t, `UPDATE clc_cluster SET install_step_index=3 WHERE cluster_no=? AND flag=1`, clusterNo2)
	if _, err := RetryInstall(id2, map[string]any{"fail_step_index": 3}, testUser); err != nil {
		t.Fatalf("编排屏续跑失败: %v", err)
	}
	orchRetry := taskNosOf(t, clusterNo2)
	row2 := mustOne(t, `SELECT * FROM clc_task WHERE task_no=?`, orchRetry[len(orchRetry)-1])
	if Str(row2["step_mode"]) != StepModeAuto {
		t.Fatalf("编排屏续跑缺省 stepMode 须为 AUTO(REF-17),得到 %s", Str(row2["step_mode"]))
	}
	waitAllSlotsFree(t, rt)
}

// ---------- FUNC-45 向导跳步（R-24 / INV-37/38/41） ----------

// TestWizardSkipStep 跳步事由必填、被跳步落 SKIPPED 并写跳过原因 / 结束时间 / 判定依据、
// 终态不可再跳（INV-41）、不存在的步 NotFound、集群健康与状态绝不因跳步改动（R-08）。
func TestWizardSkipStep(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, ClusterStatusDraft)
	// 跳步不该把健康预置成好看的值：先显式改成 UNKNOWN 再验其纹丝不动。
	mustSQL(t, `UPDATE clc_cluster SET health=? WHERE cluster_no=? AND flag=1`, HealthUnknown, clusterNo)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	mustSQL(t, `UPDATE clc_task SET step_mode=? WHERE task_no=?`, StepModeStep, taskNo)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	reason := "网络插件这步暂缓，缺件由后续补齐（R-24 验收）"

	// 事由必填（R-24）。
	_, err := WizardStepSkip(taskNo, 1, "   ", testUser)
	tplWantErr(t, err, "R-24", "事由")

	// 正常跳步。
	row, err := WizardStepSkip(taskNo, 1, reason, testUser)
	if err != nil {
		t.Fatalf("跳步失败: %v", err)
	}
	if Str(row["step_status"]) != StepStatusSkipped {
		t.Fatalf("被跳步须置 SKIPPED,得到 %s", Str(row["step_status"]))
	}
	if Str(row["skip_reason"]) != reason {
		t.Fatalf("跳过原因须落人工事由,得到 %q", Str(row["skip_reason"]))
	}
	if trimEmpty(Str(row["ended_at"])) == "" {
		t.Fatal("INV-38:进入终态须写结束时间")
	}
	if trimEmpty(Str(row["started_at"])) != "" {
		t.Fatal("被跳步从未执行,started_at 须为空")
	}
	if Str(row["verdict_source"]) != wizardStepVerdictBasis {
		t.Fatalf("INV-38:跳步判定依据须登记,得到 %q", Str(row["verdict_source"]))
	}
	// 事由累加写任务头 operationReason（R-24）。
	task := mustOne(t, `SELECT operation_reason FROM clc_task WHERE task_no=?`, taskNo)
	if !strings.Contains(Str(task["operation_reason"]), reason) {
		t.Fatalf("R-24:跳步事由须写任务头 operationReason,得到 %q", Str(task["operation_reason"]))
	}

	// 终态不可回退再跳（INV-41）。
	_, err = WizardStepSkip(taskNo, 1, reason, testUser)
	tplWantErr(t, err, "INV-41", "终态")
	// 不存在的步骤 NotFound。
	_, err = WizardStepSkip(taskNo, 99, reason, testUser)
	tplWantErr(t, err, "不存在")
	// 已 SUCCEEDED 的步骤同样不可跳。
	if e := setStep(t, taskNo, 2, StepStatusSucceeded, "退出码 0", "", "", nil); e != nil {
		t.Fatalf("置 02 成功失败: %v", e)
	}
	_, err = WizardStepSkip(taskNo, 2, reason, testUser)
	tplWantErr(t, err, "INV-41")

	// R-08：跳步绝不预置集群健康 / 状态。
	c := mustClusterRow(t, clusterNo)
	if Str(c["health"]) != HealthUnknown {
		t.Fatalf("R-08:跳步不得为界面好看预先置健康(应仍 UNKNOWN),得到 %s", Str(c["health"]))
	}
	if Str(c["status"]) != ClusterStatusDraft {
		t.Fatalf("跳步不应改动集群生命周期状态,得到 %s", Str(c["status"]))
	}
}

// TestWizardSkipFailAttribution 断言跳步后的失败归因如实体现「人工跳过」而非「执行失败于该步」
// （R-24），失败步骤索引按内核回写（R-09），集群健康仍未被预置（R-08）。
func TestWizardSkipFailAttribution(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusDraft)
	mustSQL(t, `UPDATE clc_cluster SET health=? WHERE cluster_no=? AND flag=1`, HealthUnknown, clusterNo)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	if _, err := WizardStepSkip(taskNo, 1, "第 01 步人工跳过（归因验收）", testUser); err != nil {
		t.Fatalf("跳步失败: %v", err)
	}
	// 注入第 3 步失败（连跑 AUTO 任务，经内核失败回写链）。
	enqueueTask(taskNo, DeliveryArgs{FailStep: 3})
	row := waitTaskStatuses(t, taskNo, TaskStatusFailed)
	summary := Str(row["result_summary"])
	// 归因：如实标注「人工跳过」，且失败点在第 3 步而非第 1 步。
	if !strings.Contains(summary, "人工跳过") || !strings.Contains(summary, "01") {
		t.Fatalf("R-24:失败摘要须体现人工跳过事实(含步序 01),得到 %q", summary)
	}
	if strings.Contains(summary, "第 1 步失败") {
		t.Fatalf("R-24:归因不得写成「执行失败于被跳过的那一步」,得到 %q", summary)
	}
	if !strings.Contains(summary, "第 3 步失败") {
		t.Fatalf("失败点应为注入的第 3 步,得到 %q", summary)
	}

	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusInstallFailed || paramInt(c["install_step_index"]) != 3 {
		t.Fatalf("R-09:失败须写回集群状态与续跑索引 3,得到 %+v", c)
	}
	if Str(c["health"]) != HealthUnknown {
		t.Fatalf("R-08:集群健康仍由双源写入,失败链不得预置健康,得到 %s", Str(c["health"]))
	}
	waitAllSlotsFree(t, rt)
}

// ---------- FUNC-46 生命周期台动作派发（INV-28/02/33 + R-24 + 不进流程引擎） ----------

// TestLifecycleActionDispatch 动作集只取既有枚举（非法 / 非本口动作 / 销毁一律拒），
// 生命周期前置合法（DRAFT 不可续期），CERT_RENEW 单步派发成功，高危 STOP 需二次确认 + 事由，
// 派发不进流程引擎，同集群在途互斥排队不失败（INV-48/R-18）。
func TestLifecycleActionDispatch(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	// 先跑无副作用的拒绝路径（不建任务、不占槽）。
	runningNo := seedCluster(t, ClusterStatusRunning)
	runningID := Int(mustClusterRow(t, runningNo)["id"])

	_, err := DispatchLifecycleAction(runningID, map[string]any{"task_kind": "FOO_BAR"}, testUser)
	tplWantErr(t, err, "INV-28", "不在 CLC_TASK_KIND 字典") // 非枚举（INV-28）
	_, err = DispatchLifecycleAction(runningID, map[string]any{"task_kind": "CERT_UPLOAD"}, testUser)
	tplWantErr(t, err, "INV-28") // 不在字典的相邻动作名同样拒
	_, err = DispatchLifecycleAction(runningID, map[string]any{"task_kind": TaskKindInstall}, testUser)
	tplWantErr(t, err, "生命周期台派发集") // 合法枚举但非本口动作
	_, err = DispatchLifecycleAction(runningID, map[string]any{"task_kind": TaskKindDestroy}, testUser)
	tplWantErr(t, err, "INV-33", "PROC-APP-01", "FUNC-05") // 销毁唯一经审批流

	// 高危 STOP：缺事由 / 缺二次确认（R-24）。
	_, err = DispatchLifecycleAction(runningID, map[string]any{"task_kind": TaskKindStop}, testUser)
	tplWantErr(t, err, "R-24", "事由")
	_, err = DispatchLifecycleAction(runningID, map[string]any{"task_kind": TaskKindStop, "operation_reason": "停机维护"}, testUser)
	tplWantErr(t, err, "R-24", "二次确认")

	// DRAFT 集群做 CERT_RENEW：生命周期前置不满足（INV-02）。
	draftNo := seedCluster(t, ClusterStatusDraft)
	_, err = DispatchLifecycleAction(Int(mustClusterRow(t, draftNo)["id"]), map[string]any{"task_kind": TaskKindCertRenew}, testUser)
	tplWantErr(t, err, "RUNNING", "INV-02")

	// 派发前流程实例计数（用于断言高危动作不进流程引擎）。
	flowBefore := countRows(t, `SELECT COUNT(*) FROM flow_instance`)

	// CERT_RENEW 正常派发 → 单步展开一行（R-17/INV-45）→ 内核推成 SUCCEEDED（stepMode 缺省 AUTO）。
	if _, err := DispatchLifecycleAction(runningID, map[string]any{"task_kind": TaskKindCertRenew}, testUser); err != nil {
		t.Fatalf("CERT_RENEW 派发失败: %v", err)
	}
	certTask := lastTaskNo(t, runningNo)
	cert := waitTaskStatuses(t, certTask, TaskStatusSucceeded)
	if Str(cert["task_kind"]) != TaskKindCertRenew {
		t.Fatalf("任务类别须为既有 CERT_RENEW,得到 %s", Str(cert["task_kind"]))
	}
	if Str(cert["step_mode"]) != StepModeAuto {
		t.Fatalf("生命周期台非向导入口,stepMode 缺省 AUTO(REF-17),得到 %s", Str(cert["step_mode"]))
	}
	steps := stepRows(t, certTask)
	if len(steps) != 1 || Str(steps[0]["step_code"]) != TaskKindCertRenew {
		t.Fatalf("单步动作应展开为一行且编码=类别名(R-17/INV-45),得到 %+v", steps)
	}
	waitAllSlotsFree(t, rt)

	// 高危 STOP 带事由 + 二次确认 → 受理并落 operationReason；不进流程引擎。
	reason := "停机维护窗口（高危验收）"
	if _, err := DispatchLifecycleAction(runningID, map[string]any{
		"task_kind": TaskKindStop, "operation_reason": reason, "confirmed": true,
	}, testUser); err != nil {
		t.Fatalf("高危 STOP 派发失败: %v", err)
	}
	stopTask := lastTaskNo(t, runningNo)
	stop := waitTaskStatuses(t, stopTask, TaskStatusSucceeded)
	if !strings.Contains(Str(stop["operation_reason"]), reason) {
		t.Fatalf("R-24:高危事由须留痕任务头,得到 %q", Str(stop["operation_reason"]))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM flow_instance`); n != flowBefore {
		t.Fatalf("FUNC-46:高危动作走「确认+事由+留痕」,绝不进流程引擎,flow_instance 由 %d→%d", flowBefore, n)
	}
	waitAllSlotsFree(t, rt)

	// 同集群在途互斥：先手工挂一个 RUNNING 孤儿任务（无执行协程），新派发须排队 QUEUED 而非失败（INV-48/R-18）。
	busyNo := seedCluster(t, ClusterStatusRunning)
	busyID := Int(mustClusterRow(t, busyNo)["id"])
	seedTask(t, busyNo, TaskKindUpgrade, TaskStatusRunning, ExecPathLocal)
	if _, err := DispatchLifecycleAction(busyID, map[string]any{"task_kind": TaskKindBackup}, testUser); err != nil {
		t.Fatalf("备份派发失败: %v", err)
	}
	backupTask := lastTaskNo(t, busyNo)
	if s := taskStatus(t, backupTask); s != TaskStatusQueued {
		t.Fatalf("INV-48/R-18:同集群有在途任务时新动作须排队 QUEUED 而非失败,得到 %s", s)
	}
}

// lastTaskNo 取集群最新一条任务编号（派发新建的都在末尾）。
func lastTaskNo(t *testing.T, clusterNo string) string {
	t.Helper()
	nos := taskNosOf(t, clusterNo)
	if len(nos) == 0 {
		t.Fatalf("集群 %s 无任务", clusterNo)
	}
	return nos[len(nos)-1]
}
