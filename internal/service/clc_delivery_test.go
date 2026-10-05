// clc_delivery_test.go —— 交付执行内核服务层语义用例(v1.1 TDD 债清偿)。
//
// 覆盖口径(模型为唯一语义来源):INV-37/38/39/40/41/42/43/45/46/48、R-14/R-15/R-17/R-18、
// REF-14/15/16、R-07/INV-29/INV-31(FUNC-33)、B-AUTO-09~13、REP-12/13 回放口径、G3/G4 入库口径。
package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
)

// freshRT 重置交付运行期单例(槽位 REF-14=2,本地超时/远程轮询由用例控制)。
func freshRT(t *testing.T, stepTimeout time.Duration) *deliveryRuntime {
	t.Helper()
	rt := &deliveryRuntime{
		slots: delivery.NewSlots(2),
		locks: delivery.NewClusterLock(),
		local: delivery.NewLocal(delivery.Config{ToolBin: config.Config.Delivery.ToolBin,
			ToolDir: config.Config.Delivery.ToolDir, StepTimeout: stepTimeout}),
		remote:  delivery.NewIgnition(delivery.IgnitionConfig{PollInterval: 10 * time.Millisecond}),
		runners: map[string]*taskRunner{},
		loggers: map[string]*taskLogger{},
		hub:     &logHub{subs: map[int]chan logEvent{}},
		args:    map[string]DeliveryArgs{},
		stop:    make(chan struct{}),
	}
	dlOnce.Do(func() {})
	dl = rt
	return rt
}

// ---------- 点火中心替身 ----------

type fakeRemote struct {
	mu       sync.Mutex
	submits  int
	cancels  int
	receipts []string
	next     int
	ref      string
	srv      *httptest.Server
	lastSubm map[string]any
}

func newFakeRemote(receipts []string, ref string) *fakeRemote {
	f := &fakeRemote{receipts: receipts, ref: ref}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/delivery/submit", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.submits++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastSubm = body
		_ = json.NewEncoder(w).Encode(map[string]string{"remote_task_ref": f.ref, "status": "ACCEPTED"})
	})
	mux.HandleFunc("/api/delivery/receipt/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		st := "RUNNING"
		if f.next < len(f.receipts) {
			st = f.receipts[f.next]
			f.next++
		}
		ec := 0
		_ = json.NewEncoder(w).Encode(map[string]any{"status": st, "summary": "远端结论", "exit_code": ec})
	})
	mux.HandleFunc("/api/delivery/cancel/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cancels++
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "CANCELLED"})
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeRemote) attach(t *testing.T, rt *deliveryRuntime) {
	t.Helper()
	rt.remote = delivery.NewIgnition(delivery.IgnitionConfig{BaseURL: f.srv.URL, PollInterval: 10 * time.Millisecond})
	t.Cleanup(f.srv.Close)
}

func (f *fakeRemote) counts() (submits, cancels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submits, f.cancels
}

func (f *fakeRemote) submitted() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSubm
}

var testUser = UserInfo{ID: 1, Username: "tester"}

// ---------- 用例助手 ----------

// useTool 指定交付工具夹具路径并重置运行期单例(命令面在展开与执行两处均按此配置直调)。
func useTool(t *testing.T, body string, stepTimeout time.Duration) *deliveryRuntime {
	t.Helper()
	config.Config.Delivery.ToolBin = writeDeliveryTool(t, body)
	return freshRT(t, stepTimeout)
}

// expandSteps 任务创建事务内的步骤展开(INV-45 先展开后执行)。
func expandSteps(t *testing.T, taskNo, kind, clusterNo string, resumeFrom int, nodeIPs []string) {
	t.Helper()
	if err := tx(func(db *gorm.DB) error {
		return expandStepsTx(db, taskNo, kind, clusterNo, resumeFrom, nodeIPs, testUser)
	}); err != nil {
		t.Fatalf("步骤展开失败: %v", err)
	}
}

// setStep 步骤状态回写(INV-38 终态口径 / INV-41 终局守卫)。
func setStep(t *testing.T, taskNo string, idx int, status, verdict, failDetail, skipReason string, exitCode *int) error {
	t.Helper()
	return tx(func(db *gorm.DB) error {
		return markStepTx(db, taskNo, idx, status, verdict, exitCode, failDetail, skipReason, nil)
	})
}

func mustStepRow(t *testing.T, taskNo string, idx int) map[string]any {
	t.Helper()
	return mustOne(t, `SELECT * FROM clc_task_step WHERE task_ref=? AND step_index=? AND flag=1`, taskNo, idx)
}

// waitStepStatus 期限内轮询步骤状态(超时/取消类用例不用硬 sleep)。
func waitStepStatus(t *testing.T, taskNo string, idx int, want ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		row := mustStepRow(t, taskNo, idx)
		if row != nil {
			for _, w := range want {
				if Str(row["step_status"]) == w {
					return row
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	row := mustStepRow(t, taskNo, idx)
	t.Fatalf("任务 %s 第 %d 步未在期限内到达 %v(当前 %s)", taskNo, idx, want, Str(row["step_status"]))
	return nil
}

// waitTryStart 槽位/互斥满足后按序推进(R-18):重试直到该任务取得执行权。
func waitTryStart(t *testing.T, rt *deliveryRuntime, taskNo string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rt.tryStart(taskNo) {
			return
		}
		if taskStatus(t, taskNo) != TaskStatusQueued {
			t.Fatalf("任务 %s 已不在排队态(%s),无法按序推进", taskNo, taskStatus(t, taskNo))
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("任务 %s 未在期限内取得执行权(槽位未释放?)", taskNo)
}

func mustClusterRow(t *testing.T, clusterNo string) map[string]any {
	t.Helper()
	return mustOne(t, `SELECT * FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
}

// waitAllSlotsFree 槽位释放发生在执行协程 defer 中(与任务终态写库并非同一事务),
// 故终局断言前按序轮询取还槽位(REF-14;轮询代替硬 sleep,无固定等待)。
func waitAllSlotsFree(t *testing.T, rt *deliveryRuntime) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := 0
		for rt.slots.TryAcquire() {
			got++
		}
		for i := 0; i < got; i++ {
			rt.slots.Release()
		}
		if got >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("REF-14:任务终局后全局槽位应全部释放(容量 2)")
}

func logDump(t *testing.T, taskNo string) string {
	t.Helper()
	rows, err := ReadTaskLogs(taskNo, 0, 2000, nil)
	if err != nil {
		t.Fatalf("日志回放失败: %v", err)
	}
	return MarshalJSONVal(rows)
}

// waitLogContains 等待某行输出落库(采集协程先于执行结论收束;轮询代替硬 sleep)。
func waitLogContains(t *testing.T, taskNo, needle string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logDump(t, taskNo), needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("日志未在期限内出现 %q: %s", needle, logDump(t, taskNo))
}

func intPtr(n int) *int { return &n }

// ---------- 步骤展开与单步推进(INV-45/37/39/17/38/41) ----------

func TestBuildStepPlanPure(t *testing.T) {
	// 非安装类:单步,编码 = 任务类别同名(m1 stepCode 描述)
	plan := buildStepPlan("UPGRADE", nil, "升级", 0)
	if len(plan) != 1 || plan[0].Index != 1 || plan[0].Code != "UPGRADE" || plan[0].Name != "升级" ||
		plan[0].Status != StepStatusPending {
		t.Fatalf("非安装类应展开同名单步: %+v", plan)
	}
	items := []map[string]string{{"code": "01", "label": "环境准备"}, {"code": "02", "label": "部署控制面存储"},
		{"code": "03", "label": "部署容器运行时"}, {"code": "04", "label": "部署控制面"},
		{"code": "05", "label": "加入工作节点"}, {"code": "06", "label": "部署网络插件"},
		{"code": "07", "label": "部署扩展组件"}}
	plan = buildStepPlan(TaskKindInstall, items, "安装", 0)
	if len(plan) != 7 {
		t.Fatalf("安装类应按字典展开 01..07: %+v", plan)
	}
	for i, it := range plan {
		if it.Index != i+1 || it.Code != items[i]["code"] || it.Status != StepStatusPending || it.SkipReason != "" {
			t.Fatalf("步骤 %d 展开不符: %+v", i+1, it)
		}
	}
	// R-17 续跑:resumeFrom 之前置 SKIPPED(上次已成功),与 CANCELLED(本次未执行)分列
	plan = buildStepPlan(TaskKindRetryInstall, items, "安装续跑", 3)
	if plan[0].Status != StepStatusSkipped || plan[0].SkipReason != skipReasonResume ||
		plan[1].Status != StepStatusSkipped || plan[1].SkipReason != skipReasonResume {
		t.Fatalf("续跑 1..2 应落 SKIPPED 且填跳过原因: %+v", plan[:2])
	}
	for _, it := range plan[2:] {
		if it.Status != StepStatusPending {
			t.Fatalf("第 %d 步起应待执行: %+v", it.Index, it)
		}
	}
}

func TestExpandStepsIdempotentAndAdvance(t *testing.T) {
	requireDB(t)
	useTool(t, `echo "PLAY RECAP ****"`+"\n"+`echo "n1 : ok=1 changed=0 unreachable=0 failed=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)

	// INV-45 幂等展开:重复展开不得产生第二行集(INV-37 唯一索引兜底)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	rows := stepRows(t, taskNo)
	if len(rows) != 7 {
		t.Fatalf("安装类应展开 01..07 共 7 行(INV-45),得到 %d 行", len(rows))
	}
	if n := countRows(t, `SELECT COUNT(DISTINCT step_index) FROM clc_task_step WHERE task_ref=? AND flag=1`, taskNo); n != 7 {
		t.Fatalf("INV-37:步骤序号任务内唯一,得到 %d 个不同序号", n)
	}
	for i, r := range rows {
		wantCode := fmt.Sprintf("%02d", i+1)
		if int(paramInt(r["step_index"])) != i+1 || Str(r["step_code"]) != wantCode {
			t.Fatalf("第 %d 步序号/编码不符(INV-37 自 1 连续): %+v", i+1, r)
		}
		if Str(r["step_status"]) != StepStatusPending {
			t.Fatalf("展开后应全 PENDING(INV-45): %+v", r)
		}
		if Str(r["started_at"]) != "" || Str(r["ended_at"]) != "" {
			t.Fatalf("未执行的步骤不得有起止时间: %+v", r)
		}
		// REP-12 c5:命令摘要由已校验参数数组渲染的动作+参数骨架,不含 argv[0] 路径(INV-40/47)
		if want := "setup " + clusterNo + " " + wantCode; Str(r["command_summary"]) != want {
			t.Fatalf("命令摘要应为 %q,得到 %q", want, Str(r["command_summary"]))
		}
		if strings.Contains(Str(r["command_summary"]), config.Config.Delivery.ToolBin) {
			t.Fatal("INV-40:命令摘要不得含可执行文件路径与凭据")
		}
	}

	// INV-39 单步推进:取当前最小非终态序号;已成功/已跳过的序号不再被取出
	step, err := nextRunnableStep(taskNo)
	if err != nil || step == nil || int(paramInt(step["step_index"])) != 1 {
		t.Fatalf("首个可推进步骤应为序号 1: %+v err=%v", step, err)
	}
	if err := setStep(t, taskNo, 1, StepStatusRunning, "", "", "", nil); err != nil {
		t.Fatalf("置 RUNNING 失败: %v", err)
	}
	if err := setStep(t, taskNo, 1, StepStatusSucceeded, delivery.VerdictPlayRecap, "", "", intPtr(0)); err != nil {
		t.Fatalf("置 SUCCEEDED 失败: %v", err)
	}
	if err := setStep(t, taskNo, 2, StepStatusSkipped, "", "", skipReasonResume, nil); err != nil {
		t.Fatalf("置 SKIPPED 失败: %v", err)
	}
	step, err = nextRunnableStep(taskNo)
	if err != nil || step == nil || int(paramInt(step["step_index"])) != 3 {
		t.Fatalf("INV-37/39:已成功与已跳过步骤不得再次取出,期望序号 3 得到 %+v err=%v", step, err)
	}

	// INV-41 终局保护:已成功行不被后续回写覆盖
	if err := setStep(t, taskNo, 1, StepStatusRunning, "", "", "", nil); err != nil {
		t.Fatalf("终局守卫不应报错: %v", err)
	}
	if got := Str(mustStepRow(t, taskNo, 1)["step_status"]); got != StepStatusSucceeded {
		t.Fatalf("INV-41:终态不可回退,当前 %s", got)
	}
	// INV-38:终态缺判定依据/缺失败摘要/缺跳过原因一律拒绝
	if err := setStep(t, taskNo, 3, StepStatusSucceeded, "", "", "", nil); err == nil ||
		!strings.Contains(err.Error(), "INV-38") {
		t.Fatalf("INV-38:进入终态必须登记判定依据,得到 %v", err)
	}
	if err := setStep(t, taskNo, 3, StepStatusFailed, delivery.VerdictExitCode, "", "", intPtr(1)); err == nil ||
		!strings.Contains(err.Error(), "INV-38") {
		t.Fatalf("INV-38:FAILED 必须登记失败摘要,得到 %v", err)
	}
	if err := setStep(t, taskNo, 3, StepStatusSkipped, "", "", "", nil); err == nil ||
		!strings.Contains(err.Error(), "INV-38") {
		t.Fatalf("INV-38:SKIPPED 必须登记跳过原因,得到 %v", err)
	}
	// 全部步骤进入终态 ⇒ 无待推进步骤(INV-45 展开齐备是执行前置,反之即完成)
	for i := 3; i <= 7; i++ {
		if err := setStep(t, taskNo, i, StepStatusSucceeded, delivery.VerdictPlayRecap, "", "", intPtr(0)); err != nil {
			t.Fatalf("第 %d 步终态落行失败: %v", i, err)
		}
	}
	if step, err := nextRunnableStep(taskNo); err != nil || step != nil {
		t.Fatalf("全部步骤终局后不得再取出待推进步骤: %+v err=%v", step, err)
	}
}

func TestResumeExpansionSeparatesSkipAndCancel(t *testing.T) {
	requireDB(t)
	useTool(t, `echo "PLAY RECAP ****"`+"\n"+`echo "n1 : ok=1 changed=0 unreachable=0 failed=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusInstallFailed)
	mustSQL(t, `UPDATE clc_cluster SET install_step_index=3 WHERE cluster_no=?`, clusterNo)
	taskNo := seedTask(t, clusterNo, TaskKindRetryInstall, TaskStatusQueued, ExecPathLocal)

	// R-17/INV-37:失败索引之前落 SKIPPED 并写跳过原因、不执行
	expandSteps(t, taskNo, TaskKindRetryInstall, clusterNo, 3, nil)
	skipped := stepsByStatus(t, taskNo, StepStatusSkipped)
	if len(skipped) != 2 || int(paramInt(skipped[0]["step_index"])) != 1 || int(paramInt(skipped[1]["step_index"])) != 2 {
		t.Fatalf("R-17:续跑应在索引 3 之前落 2 行 SKIPPED: %+v", skipped)
	}
	for _, r := range skipped {
		if Str(r["skip_reason"]) != skipReasonResume {
			t.Fatalf("INV-38:SKIPPED 必填跳过原因(R-17 固定文本),得到 %q", Str(r["skip_reason"]))
		}
		if Str(r["started_at"]) != "" {
			t.Fatal("跳过步骤不得有起始时间(不执行)")
		}
	}
	if len(stepsByStatus(t, taskNo, StepStatusCancelled)) != 0 {
		t.Fatal("展开阶段不得产生 CANCELLED 行(REP-12:上次已成功 vs 本次未执行)")
	}

	// R-14/R-15:未执行步骤取消 —— 与 SKIPPED 分列(REP-12 口径)
	if err := tx(func(db *gorm.DB) error { return cancelPendingStepsTx(db, taskNo, 4) }); err != nil {
		t.Fatalf("未执行步骤置取消失败: %v", err)
	}
	cancelled := stepsByStatus(t, taskNo, StepStatusCancelled)
	if len(cancelled) != 4 {
		t.Fatalf("序号 4..7 应置 CANCELLED,得到 %d 行", len(cancelled))
	}
	for _, r := range cancelled {
		if Str(r["skip_reason"]) != "" || Str(r["fail_detail"]) != "" {
			t.Fatalf("CANCELLED 行不得复用跳过/失败口径(REP-12 分列): %+v", r)
		}
	}
	if Str(mustStepRow(t, taskNo, 1)["step_status"]) != StepStatusSkipped {
		t.Fatal("INV-41:已落 SKIPPED 的行不得被取消链改写")
	}
}

// ---------- 成功链 / 判定依据 / 超时失败链(B-AUTO-09/10、R-14、REF-15/16) ----------

func TestInstallSuccessChainLocal(t *testing.T) {
	requireDB(t)
	fr := newFakeRemote([]string{"SUCCEEDED"}, "IGN-UNUSED")
	rt := useTool(t, `echo "step $3 running"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=4 changed=2 unreachable=0 failed=0 skipped=0"`, time.Minute)
	fr.attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusSucceeded)
	if Str(row["execution_path"]) != ExecPathLocal || Str(row["ended_at"]) == "" ||
		trimEmpty(Str(row["result_summary"])) == "" {
		t.Fatalf("INV-29/32:终态须有结束时间与结果摘要,且执行位置登记值不改写: %+v", row)
	}
	if row["fail_step_index"] != nil || Str(row["fail_reason"]) != "" {
		t.Fatalf("成功任务不得携带失败索引与失败原因: %+v", row)
	}
	succeeded := stepsByStatus(t, taskNo, StepStatusSucceeded)
	if len(succeeded) != 7 {
		t.Fatalf("B-AUTO-10:单步推进应把 7 步全部推至成功,得到 %d 行", len(succeeded))
	}
	for _, r := range succeeded {
		// INV-38:终态必填判定依据与结束时间;B-AUTO-10 判定依据取剧本汇总
		if Str(r["verdict_source"]) != delivery.VerdictPlayRecap || Str(r["ended_at"]) == "" {
			t.Fatalf("INV-38/B-AUTO-10:终态缺判定依据或结束时间: %+v", r)
		}
		if Int(r["exit_code"]) != 0 {
			t.Fatalf("退出码应记录为 0: %+v", r)
		}
		if Str(r["started_at"]) == "" || paramInt(r["log_start_line"]) <= 0 || paramInt(r["log_end_line"]) <= 0 {
			t.Fatalf("A08/REP-12 c7:应登记起止时间与日志游标区间: %+v", r)
		}
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status IN (?, ?)`,
		taskNo, StepStatusRunning, StepStatusPending); n != 0 {
		t.Fatalf("任务终局后不得残留 RUNNING/PENDING 步骤(INV-39),得到 %d", n)
	}

	// B-AUTO-09 / R-13 安装成功落点:状态 → RUNNING + 主控入口 + 凭据存放标识登记(内容不入库 INV-08)
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusRunning {
		t.Fatalf("B-AUTO-09:末步成功后集群应置 RUNNING,当前 %s", Str(c["status"]))
	}
	if trimEmpty(Str(c["master_endpoints"])) == "" || trimEmpty(Str(c["kubeconfig_ref"])) == "" {
		t.Fatalf("R-13/INV-07:主控入口与凭据存放标识必须登记: %+v", c)
	}
	if !strings.HasPrefix(Str(c["kubeconfig_ref"]), "vault://") || strings.Contains(Str(c["kubeconfig_ref"]), "-----BEGIN") {
		t.Fatalf("INV-08:凭据只登记存放标识,得到 %q", Str(c["kubeconfig_ref"]))
	}
	if c["install_step_index"] != nil {
		t.Fatalf("安装成功后失败步骤索引应清空(INV-03 反向口径): %+v", c["install_step_index"])
	}
	if n := countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE action=?`, "B-AUTO-09"); n == 0 {
		t.Fatal("B-AUTO-09 成功落点须留审计痕迹")
	}

	// REF-16:形态登记后运行期不互转 —— 本地任务绝不被远程适配器接管
	if submits, cancels := fr.counts(); submits != 0 || cancels != 0 {
		t.Fatalf("REF-16:LOCAL 任务不得调用点火中心(submits=%d cancels=%d)", submits, cancels)
	}
	if rt.adapterFor(ExecPathLocal).Form() != delivery.FormLocal ||
		rt.adapterFor(ExecPathRemoteExecutor).Form() != delivery.FormRemote {
		t.Fatal("REF-16:适配器形态按任务登记值选取,不互转")
	}
	waitAllSlotsFree(t, rt)
}

func TestRecapVerdictOverridesExitCodeZero(t *testing.T) {
	requireDB(t)
	// 退出码 0 但剧本汇总有失败 ⇒ 判失败(B-AUTO-10 / A13 判定网关)
	rt := useTool(t, `echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=5 changed=1 unreachable=0 failed=1 skipped=0"`+"\n"+
		`echo "node-2 : ok=3 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	if rt == nil {
		t.Fatal("运行期未就绪")
	}

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusFailed)
	if row["fail_step_index"] == nil || int(paramInt(row["fail_step_index"])) != 1 {
		t.Fatalf("INV-30:失败步骤索引应登记为 1: %+v", row)
	}
	step := mustStepRow(t, taskNo, 1)
	if Str(step["step_status"]) != StepStatusFailed {
		t.Fatalf("剧本汇总失败必须落 FAILED,当前 %s", Str(step["step_status"]))
	}
	if Str(step["verdict_source"]) != delivery.VerdictPlayRecap {
		t.Fatalf("B-AUTO-10:判定依据应为 PLAY_RECAP 而非仅退出码,得到 %s", Str(step["verdict_source"]))
	}
	if Int(step["exit_code"]) != 0 {
		t.Fatalf("退出码应如实记录 0(判定依据来自汇总行): %+v", step["exit_code"])
	}
	if !containsAll(Str(step["fail_detail"]), "剧本汇总", "1 个失败") {
		t.Fatalf("失败摘要应带汇总依据(REP-12 c8): %q", Str(step["fail_detail"]))
	}
	if !strings.Contains(Str(row["result_summary"]), "第 1 步失败") {
		t.Fatalf("R-14:任务摘要应回写失败步骤: %q", Str(row["result_summary"]))
	}
	// R-14:非安装类失败 ⇒ 集群状态不动(只有安装类回写 INSTALL_FAILED)
	if Str(mustClusterRow(t, clusterNo)["status"]) != ClusterStatusRunning {
		t.Fatal("R-14:UPGRADE 失败不得改写集群生命周期状态")
	}
}

func TestStepTimeoutFailChain(t *testing.T) {
	requireDB(t)
	// REF-15:单步超时 ⇒ 进程组终止 ⇒ 按失败结论落行 ⇒ R-14 回写链
	rt := useTool(t, `echo "TIMEOUT-STEP-BEGIN"`+"\n"+`sleep 30`+"\n"+`echo "TIMEOUT-STEP-DONE"`, 1500*time.Millisecond)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)
	startedAt := time.Now()
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusFailed)
	step := waitStepStatus(t, taskNo, 1, StepStatusFailed)
	// 进程组被终止而非等自然结束:夹具自身要跑 30s,超时阈值 400ms ⇒ 终局必须远早于 30s
	if spent := time.Since(startedAt); spent > 15*time.Second {
		t.Fatalf("REF-15:超时应即刻终止进程组并按失败落行,实际耗时 %v", spent)
	}
	if Str(step["verdict_source"]) != delivery.VerdictExitCode {
		t.Fatalf("REF-15:超时按失败结论落行,判定依据回落退出码口径,得到 %s", Str(step["verdict_source"]))
	}
	if !strings.Contains(Str(step["fail_detail"]), "单步超时(REF-15)") {
		t.Fatalf("失败摘要须标注超时依据: %q", Str(step["fail_detail"]))
	}
	if Int(step["exit_code"]) == 0 {
		t.Fatalf("超时步骤不得记录成功退出码: %+v", step)
	}
	if Str(step["ended_at"]) == "" {
		t.Fatal("INV-38:终态必填结束时间")
	}
	if n := len(stepsByStatus(t, taskNo, StepStatusCancelled)); n != 6 {
		t.Fatalf("R-14:后续未执行步骤应全部置 CANCELLED,得到 %d 行", n)
	}
	// INV-42/REP-13 不漏行:超时前已产生的输出必须完整入库(退出结论不得先于末行采集)
	if dump := logDump(t, taskNo); !strings.Contains(dump, "TIMEOUT-STEP-BEGIN") {
		t.Fatalf("REF-15/INV-42:超时步骤的已产生输出不得丢失: %s", dump)
	}
	// INV-30/R-09:失败步骤索引任务与集群双写 ⇒ 可续跑
	if row["fail_step_index"] == nil || int(paramInt(row["fail_step_index"])) != 1 {
		t.Fatalf("INV-30:任务侧失败步骤索引应为 1: %+v", row)
	}
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusInstallFailed || paramInt(c["install_step_index"]) != 1 {
		t.Fatalf("R-14:安装类失败 ⇒ 集群置 INSTALL_FAILED 并保留续跑索引: %+v", c)
	}
	waitAllSlotsFree(t, rt)
}

// ---------- 互斥与槽位(INV-48 / R-18 / REF-14) ----------

func TestClusterMutexAndGlobalSlots(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `sleep 1`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)

	newUpgradeTask := func() string {
		clusterNo := seedCluster(t, ClusterStatusRunning)
		taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
		expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
		return taskNo
	}

	clusterA := seedCluster(t, ClusterStatusRunning)
	taskA1 := seedTask(t, clusterA, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskA1, TaskKindUpgrade, clusterA, 0, nil)
	taskA2 := seedTask(t, clusterA, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskA2, TaskKindUpgrade, clusterA, 0, nil)
	taskB := newUpgradeTask()
	taskC := newUpgradeTask()

	enqueueTask(taskA1, DeliveryArgs{})
	// INV-48:同一集群至多一个运行中交付任务 —— 同集群第二个不得起
	if rt.tryStart(taskA2) {
		t.Fatal("INV-48:同集群第二个任务不得取得执行权")
	}
	if got := taskStatus(t, taskA2); got != TaskStatusQueued {
		t.Fatalf("INV-48:被互斥拦下的任务应保持 QUEUED,当前 %s", got)
	}

	enqueueTask(taskB, DeliveryArgs{})
	// REF-14=2:两槽位占满 ⇒ 第三个任务排队而不是失败(R-18)
	if rt.tryStart(taskC) {
		t.Fatal("REF-14:全局槽位占满时不得再起新任务")
	}
	if got := taskStatus(t, taskC); got != TaskStatusQueued {
		t.Fatalf("R-18:槽位占满应排队 QUEUED,当前 %s", got)
	}
	if rt.slots.TryAcquire() {
		rt.slots.Release()
		t.Fatal("REF-14:两个在跑任务应占满 2 个全局槽位")
	}

	// INV-48 DB 强校验兜底:同集群已有 RUNNING 行(无本副本协程)⇒ 不得双跑
	clusterD := seedCluster(t, ClusterStatusRunning)
	taskD1 := seedTask(t, clusterD, TaskKindUpgrade, TaskStatusRunning, ExecPathLocal)
	taskD2 := seedTask(t, clusterD, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskD2, TaskKindUpgrade, clusterD, 0, nil)
	if rt.tryStart(taskD2) {
		t.Fatal("INV-48:集群已有 RUNNING 任务时,DB 兜底必须拦下第二个任务")
	}

	waitTaskStatuses(t, taskA1, TaskStatusSucceeded)
	waitTaskStatuses(t, taskB, TaskStatusSucceeded)
	// 该 RUNNING 行只是兜底道具(无协程持有),显式取消以免污染后续清扫
	mustSQL(t, `UPDATE clc_task SET task_status=?, ended_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE task_no=?`,
		TaskStatusCancelled, taskD1)

	// R-18:槽位释放后按排队次序推进
	waitTryStart(t, rt, taskC)
	waitTaskStatuses(t, taskC, TaskStatusSucceeded)
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE flag=1 AND task_status=?`, TaskStatusRunning); n != 0 {
		t.Fatalf("用例收尾不得残留 RUNNING 任务,得到 %d", n)
	}
}

// ---------- FUNC-33 取消(R-15 / INV-28/29/31/41 / R-07) ----------

func TestCancelRunningTaskChain(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `if [ "$3" = "01" ]; then`+"\n"+
		`  echo "PLAY RECAP ****"`+"\n"+
		`  echo "node-1 : ok=2 changed=1 unreachable=0 failed=0 skipped=0"`+"\n"+
		`  exit 0`+"\n"+
		`fi`+"\n"+
		`echo "STEP-$3-BEGIN"`+"\n"+
		`sleep 30`+"\n"+
		`echo "STEP-$3-DONE"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	waitStepStatus(t, taskNo, 2, StepStatusRunning)
	// 采集先于结论:确认在跑步骤的输出已落库后再下达取消(INV-42/REP-13)
	waitLogContains(t, taskNo, "STEP-02-BEGIN")
	cancelledAt := time.Now()
	if _, err := CancelClusterTask(taskNo, "演练取消(R-15)", testUser); err != nil {
		t.Fatalf("RUNNING 取消应被受理: %v", err)
	}
	row := waitTaskStatuses(t, taskNo, TaskStatusCancelled)
	// R-15「即刻终止」:夹具在跑步骤自身要睡 30s ⇒ 取消终局必须远早于自然结束
	if spent := time.Since(cancelledAt); spent > 15*time.Second {
		t.Fatalf("R-15:取消应即刻终止进程组并落取消链,实际耗时 %v", spent)
	}

	// 已终态步骤不回改(INV-41):第 1 步仍为成功结论
	first := mustStepRow(t, taskNo, 1)
	if Str(first["step_status"]) != StepStatusSucceeded || Str(first["verdict_source"]) != delivery.VerdictPlayRecap {
		t.Fatalf("INV-41:已成功步骤不得被取消链回改: %+v", first)
	}
	// R-15/m2 FUNC-33 后置:运行中步骤置 FAILED 且摘要记「人工取消」
	second := mustStepRow(t, taskNo, 2)
	if Str(second["step_status"]) != StepStatusFailed || !strings.Contains(Str(second["fail_detail"]), "人工取消") {
		t.Fatalf("R-15:运行中步骤应置 FAILED 并记人工取消: %+v", second)
	}
	if n := len(stepsByStatus(t, taskNo, StepStatusCancelled)); n != 5 {
		t.Fatalf("R-15:未执行步骤(3..7)应置 CANCELLED,得到 %d 行", n)
	}
	for _, r := range stepsByStatus(t, taskNo, StepStatusCancelled) {
		if Str(r["started_at"]) != "" {
			t.Fatalf("取消不是回滚:未执行步骤不得被启动过(REP-12 c6): %+v", r)
		}
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status IN (?, ?)`,
		taskNo, StepStatusRunning, StepStatusPending); n != 0 {
		t.Fatalf("取消后不得残留运行/待执行步骤,得到 %d", n)
	}
	dump := logDump(t, taskNo)
	if !strings.Contains(dump, "STEP-02-BEGIN") {
		t.Fatalf("R-15/INV-42:被取消步骤的已产生输出不得丢失(采集须先于结论收束): %s", dump)
	}
	if !strings.Contains(Str(row["result_summary"]), "取消完成") || Str(row["ended_at"]) == "" {
		t.Fatalf("INV-29:取消终态须有结果摘要与结束时间: %+v", row)
	}
	// 集群回退可续跑 + 执行位置登记值不变(INV-31/32)
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusInstallFailed || paramInt(c["install_step_index"]) != 2 {
		t.Fatalf("m2 FUNC-33 后置:取消安装类应保留续跑索引(首个未执行/在跑序号): %+v", c)
	}
	if Str(row["execution_path"]) != ExecPathLocal {
		t.Fatalf("INV-32:取消不得改写执行位置: %s", Str(row["execution_path"]))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE action=? AND detail LIKE ?`,
		"FUNC-33", "%"+taskNo+"%"); n == 0 {
		t.Fatal("m2 FUNC-33 前置:取消原因须入审计留痕")
	}
	waitAllSlotsFree(t, rt)
}

func TestCancelQueuedTaskBranchAndGuards(t *testing.T) {
	requireDB(t)
	useTool(t, `echo "PLAY RECAP ****"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)

	// 取消必须显式填原因(m2 FUNC-33 前置)
	if _, err := CancelClusterTask(taskNo, "   ", testUser); err == nil ||
		!strings.Contains(err.Error(), "原因") {
		t.Fatalf("取消缺原因应拒绝,得到 %v", err)
	}
	if got := taskStatus(t, taskNo); got != TaskStatusQueued {
		t.Fatalf("被拒绝的取消不得改动任务状态: %s", got)
	}

	// QUEUED 未开跑即取消(R-15 的另一分支)
	if _, err := CancelClusterTask(taskNo, "排队中取消", testUser); err != nil {
		t.Fatalf("QUEUED 取消应被受理: %v", err)
	}
	row := waitTaskStatuses(t, taskNo, TaskStatusCancelled)
	if !strings.Contains(Str(row["result_summary"]), "未开跑") {
		t.Fatalf("未开跑分支应留独立摘要: %q", Str(row["result_summary"]))
	}
	if n := len(stepsByStatus(t, taskNo, StepStatusCancelled)); n != 7 {
		t.Fatalf("未开跑取消应把 7 步全部置 CANCELLED,得到 %d 行", n)
	}
	if n := len(stepsByStatus(t, taskNo, StepStatusFailed)); n != 0 {
		t.Fatal("未开跑取消没有运行中步骤,不得落 FAILED(与 RUNNING 取消分支分列)")
	}
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusInstallFailed || paramInt(c["install_step_index"]) != 1 {
		t.Fatalf("m2 FUNC-33 后置:续跑索引应取首个未执行步骤序号: %+v", c)
	}

	// 终态任务不渲染取消入口(INV-29)且取消不是改写(INV-31)
	if _, err := CancelClusterTask(taskNo, "再取消一次", testUser); err == nil ||
		!strings.Contains(err.Error(), "INV-29") {
		t.Fatalf("终态任务取消应被拒绝(INV-29),得到 %v", err)
	}

	// DESTROY 类不可取消(R-07:审批通过后即进入不可逆编排)
	dc := seedCluster(t, ClusterStatusDestroying)
	destroyTask := seedTask(t, dc, TaskKindDestroy, TaskStatusQueued, ExecPathLocal)
	if _, err := CancelClusterTask(destroyTask, "想撤回销毁", testUser); err == nil ||
		!strings.Contains(err.Error(), "R-07") {
		t.Fatalf("R-07:DESTROY 类任务应拒绝取消,得到 %v", err)
	}
	if got := taskStatus(t, destroyTask); got != TaskStatusQueued {
		t.Fatalf("被拒绝的销毁取消不得改动任务: %s", got)
	}
	if _, err := CancelClusterTask("TSK-NOT-EXIST", "无此任务", testUser); err == nil {
		t.Fatal("不存在的任务应报未找到")
	}
}

// ---------- 远程链路(INV-46 / B-AUTO-12 / R-15 / REF-16) ----------

func TestRemoteTaskWritesRefAndReceipt(t *testing.T) {
	requireDB(t)
	fr := newFakeRemote([]string{"RUNNING", "SUCCEEDED"}, "IGN-RT-77")
	rt := useTool(t, `echo "本地链路不得执行"`, time.Minute)
	fr.attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathRemoteExecutor)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusSucceeded)
	if Str(row["remote_task_ref"]) != "IGN-RT-77" {
		t.Fatalf("INV-46:远程链路必须回写远端任务引用,得到 %q", Str(row["remote_task_ref"]))
	}
	if Str(row["execution_path"]) != ExecPathRemoteExecutor {
		t.Fatalf("REF-16/INV-32:远程任务运行期不得互转: %s", Str(row["execution_path"]))
	}
	step := mustStepRow(t, taskNo, 1)
	if Str(step["step_status"]) != StepStatusSucceeded || Str(step["verdict_source"]) != delivery.VerdictRemoteReceipt {
		t.Fatalf("B-AUTO-12:远程结论判定依据应为远端回执: %+v", step)
	}
	submits, cancels := fr.counts()
	if submits != 1 || cancels != 0 {
		t.Fatalf("远程链路应恰好提交一次且无撤销: submits=%d cancels=%d", submits, cancels)
	}
	// REF-12:提交只带登记标识(集群/步骤/任务),命令面与凭据不出域
	body := fr.submitted()
	if Str(body["task_no"]) != taskNo || Str(body["cluster_no"]) != clusterNo || Str(body["step_code"]) != TaskKindUpgrade {
		t.Fatalf("提交意图应携带登记标识: %+v", body)
	}
	if _, ok := body["args"]; ok {
		t.Fatal("REF-12/INV-40:提交不得携带命令面参数")
	}
}

func TestRemoteCancelCallsIgnition(t *testing.T) {
	requireDB(t)
	fr := newFakeRemote([]string{"RUNNING"}, "IGN-RT-88")
	rt := useTool(t, `echo "本地链路不得执行"`, time.Minute)
	fr.attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathRemoteExecutor)
	expandSteps(t, taskNo, TaskKindInstall, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	waitStepStatus(t, taskNo, 1, StepStatusRunning)
	if _, err := CancelClusterTask(taskNo, "远程任务取消", testUser); err != nil {
		t.Fatalf("远程 RUNNING 取消应被受理: %v", err)
	}
	row := waitTaskStatuses(t, taskNo, TaskStatusCancelled)

	submits, cancels := fr.counts()
	if submits != 1 || cancels != 1 {
		t.Fatalf("R-15:远程链路取消须经点火中心撤销远端任务: submits=%d cancels=%d", submits, cancels)
	}
	if Str(row["remote_task_ref"]) != "IGN-RT-88" {
		t.Fatalf("INV-46:取消后远端引用仍须留痕(残留可见): %q", Str(row["remote_task_ref"]))
	}
	step := mustStepRow(t, taskNo, 1)
	if Str(step["step_status"]) != StepStatusFailed || !strings.Contains(Str(step["fail_detail"]), "人工取消") {
		t.Fatalf("R-15:在跑步骤置 FAILED 且摘要记人工取消: %+v", step)
	}
	if n := len(stepsByStatus(t, taskNo, StepStatusCancelled)); n != 6 {
		t.Fatalf("R-15:其余未执行步骤应置 CANCELLED,得到 %d 行", n)
	}
	if dump := logDump(t, taskNo); !strings.Contains(dump, "远端撤销") {
		t.Fatal("FUNC-33:远程取消须在日志留撤销痕迹(INV-31 留痕)")
	}
}

func TestRemoteMissingRefIsFailure(t *testing.T) {
	requireDB(t)
	fr := newFakeRemote([]string{"SUCCEEDED"}, "") // 远端受理但未回引用
	rt := useTool(t, `echo "本地链路不得执行"`, time.Minute)
	fr.attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathRemoteExecutor)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusFailed)
	if Str(row["remote_task_ref"]) != "" {
		t.Fatalf("无远端引用时不得伪造登记值: %q", Str(row["remote_task_ref"]))
	}
	step := mustStepRow(t, taskNo, 1)
	if Str(step["step_status"]) != StepStatusFailed {
		t.Fatalf("INV-46:登记为远程却无远端引用即判失败,当前 %s", Str(step["step_status"]))
	}
	if !strings.Contains(Str(step["fail_detail"]), "INV-46") {
		t.Fatalf("失败摘要须引用 INV-46 依据: %q", Str(step["fail_detail"]))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE task_no=? AND task_status=?`,
		taskNo, TaskStatusSucceeded); n != 0 {
		t.Fatal("INV-46:缺远端引用绝不可当作成功")
	}
	if submits, _ := fr.counts(); submits != 1 {
		t.Fatalf("应恰好提交一次: %d", submits)
	}
}

// ---------- 失联终局(B-AUTO-11 / INV-28/32 / R-09) ----------

func TestStalledWhenExecutorProcessNotSurvivable(t *testing.T) {
	requireDB(t)
	// 交付进程不可存活 = 失联唯一判据(裁定四),不是失败也不误判成功
	missing := filepath.Join(t.TempDir(), "no-such-dir", "delivery-tool")
	config.Config.Delivery.ToolBin = missing
	rt := freshRT(t, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusInstalling)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})

	row := waitTaskStatuses(t, taskNo, TaskStatusStalled)
	if Str(row["task_status"]) == TaskStatusFailed || Str(row["task_status"]) == TaskStatusSucceeded {
		t.Fatalf("INV-28:失联须与失败/成功分列: %s", Str(row["task_status"]))
	}
	if !strings.Contains(Str(row["result_summary"]), "失联") {
		t.Fatalf("B-AUTO-11:结果摘要须标注失联挂起: %q", Str(row["result_summary"]))
	}
	if Str(mustStepRow(t, taskNo, 1)["step_status"]) == StepStatusSucceeded {
		t.Fatal("B-AUTO-11:无结论的执行不得误判成功")
	}
	// R-09:STALLED ⇒ 集群降级归因 EXECUTOR_LOST;执行位置登记值不改写(INV-32/REF-16)
	c := mustClusterRow(t, clusterNo)
	if Str(c["health"]) != HealthDegraded || Str(c["degraded_reason"]) != DegradedExecutorLost {
		t.Fatalf("R-09:失联应降级归因执行机失联: %+v", c)
	}
	if Str(row["execution_path"]) != ExecPathLocal {
		t.Fatalf("INV-32:失联不得静默改写执行位置: %s", Str(row["execution_path"]))
	}

	// 启动/看护清扫:无协程持有且进程不可存活的 RUNNING 行同样判失联(B-AUTO-11)
	orphanCluster := seedCluster(t, ClusterStatusRunning)
	orphan := seedTask(t, orphanCluster, TaskKindUpgrade, TaskStatusRunning, ExecPathLocal)
	rt.sweepOrphans()
	if got := taskStatus(t, orphan); got != TaskStatusStalled {
		t.Fatalf("B-AUTO-11:遗留 RUNNING 任务应被清扫为 STALLED,当前 %s", got)
	}
}

// ---------- 日志游标与掩码(INV-42/43、REP-12/13、FUNC-35) ----------

func TestLogCursorIncrementalReplay(t *testing.T) {
	requireDB(t)
	useTool(t, `echo "LINE-A"`+"\n"+`echo "LINE-B"`+"\n"+`echo "LINE-C"`+"\n"+`echo "LINE-D"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+`echo "n1 : ok=1 changed=0 unreachable=0 failed=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})
	waitTaskStatuses(t, taskNo, TaskStatusSucceeded)

	all := mustList(t, `SELECT line_no FROM clc_task_log WHERE task_ref=? AND flag=1 ORDER BY line_no ASC`, taskNo)
	if len(all) < 8 {
		t.Fatalf("夹具 6 行 + 任务级行应齐备,得到 %d 行", len(all))
	}
	if n := countRows(t, `SELECT COUNT(DISTINCT line_no) FROM clc_task_log WHERE task_ref=? AND flag=1`, taskNo); n != int64(len(all)) {
		t.Fatalf("INV-43:日志序号任务内唯一(只增不改),得到 %d 个不同游标 / %d 行", n, len(all))
	}

	// REP-13:按游标增量读取 —— 不漏不重、游标只增
	var seenSeq []int64
	seen := map[int64]bool{}
	var offset int64
	for rounds := 0; rounds < 50; rounds++ {
		batch, err := ReadTaskLogs(taskNo, offset, 3, nil)
		if err != nil {
			t.Fatalf("增量读取失败: %v", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			ln := Int(r["line_no"])
			if ln <= offset {
				t.Fatalf("INV-43/REP-13:游标只增,读到 %d <= offset %d", ln, offset)
			}
			if seen[ln] {
				t.Fatalf("REP-13:增量回放不得重复行号 %d", ln)
			}
			seen[ln] = true
			seenSeq = append(seenSeq, ln)
		}
		offset = Int(batch[len(batch)-1]["line_no"])
	}
	if len(seenSeq) != len(all) {
		t.Fatalf("REP-13:增量回放不得漏行,读到 %d 行 / 实际 %d 行", len(seenSeq), len(all))
	}
	if seenSeq[0] != 1 {
		t.Fatalf("INV-43:游标应自 1 起,首行 %d", seenSeq[0])
	}
	for i := 1; i < len(seenSeq); i++ {
		// INV-43 口径为「单调递增、只增不改」(A08 游标区间起点会预占号位,空洞不影响不漏不重)
		if seenSeq[i] <= seenSeq[i-1] {
			t.Fatalf("INV-43:游标只增不回退、不重复:%d 之后出现 %d", seenSeq[i-1], seenSeq[i])
		}
	}

	// 断线重放:先取快照(状态 + 最大游标),再从该游标续读 ⇒ 无重复行
	status, maxLine, err := TaskLogSnapshot(taskNo)
	if err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	if status != TaskStatusSucceeded || maxLine != Int(all[len(all)-1]["line_no"]) {
		t.Fatalf("REP-13:快照应返回终态与游标水位,得到 %s/%d", status, maxLine)
	}
	tail, err := ReadTaskLogs(taskNo, maxLine, 500, nil)
	if err != nil {
		t.Fatalf("尾部续读失败: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("REP-13:水位之后不得有增量,得到 %d 行", len(tail))
	}
	// 过滤口径(REP-13 p2/p3)
	stepOnly, err := ReadTaskLogs(taskNo, 0, 500, map[string]any{"step_index": 1})
	if err != nil || len(stepOnly) == 0 {
		t.Fatalf("按步骤序号过滤应有行: rows=%d err=%v", len(stepOnly), err)
	}
	for _, r := range stepOnly {
		if int(paramInt(r["step_index"])) != 1 {
			t.Fatalf("REP-13 p2:步骤过滤越界: %+v", r)
		}
	}
	warnOnly, err := ReadTaskLogs(taskNo, 0, 500, map[string]any{"log_level": delivery.LevelInfo})
	if err != nil {
		t.Fatalf("按级别过滤失败: %v", err)
	}
	for _, r := range warnOnly {
		if Str(r["log_level"]) != delivery.LevelInfo {
			t.Fatalf("REP-13 p3:级别过滤越界: %+v", r)
		}
	}
	if st, mx, err := TaskLogSnapshot("TSK-NOT-EXIST"); err != nil || st != "" || mx != 0 {
		t.Fatalf("未知任务快照应返回空而不报错: status=%q max=%d err=%v", st, mx, err)
	}
}

func TestSecretMaskingIsIrreversible(t *testing.T) {
	requireDB(t)
	const plainPwd = "PlainPayload-Pwd9527"
	const plainToken = "PlainPayload-Token666"
	useTool(t, `echo "connect node-1 password=`+plainPwd+`"`+"\n"+
		`echo "auth token: `+plainToken+`"`+"\n"+
		`echo "-----BEGIN OPENSSH PRIVATE KEY-----"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "n1 : ok=1 changed=0 unreachable=0 failed=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})
	waitTaskStatuses(t, taskNo, TaskStatusSucceeded)

	// INV-42:落库行已完成掩码、原文不留存
	for _, plain := range []string{plainPwd, plainToken, "BEGIN OPENSSH PRIVATE KEY"} {
		if n := countRows(t, `SELECT COUNT(*) FROM clc_task_log WHERE task_ref=? AND content LIKE ?`,
			taskNo, "%"+plain+"%"); n != 0 {
			t.Fatalf("INV-42:落库日志仍含明文 %q(%d 行),掩码不可逆未落实", plain, n)
		}
	}
	dump := logDump(t, taskNo)
	if !strings.Contains(dump, "[已掩码]") {
		t.Fatalf("INV-42:日志应带掩码标记: %s", dump)
	}
	// FUNC-35 响应面同样不得出现明文
	rows, err := ReadTaskLogs(taskNo, 0, 500, nil)
	if err != nil {
		t.Fatalf("回放失败: %v", err)
	}
	if raw := MarshalJSONVal(rows); strings.Contains(raw, plainPwd) || strings.Contains(raw, plainToken) {
		t.Fatal("INV-42:日志回放响应不得携带凭据明文")
	}
	// INV-40:失败摘要落库前掩码(REP-12 c8)。用未开跑的第二个任务 —— 首步仍 PENDING,
	// 回写才生效(已终局任务的首步受 INV-41 守卫,静默跳过)。
	bc := seedCluster(t, ClusterStatusRunning)
	failedTask := seedTask(t, bc, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, failedTask, TaskKindUpgrade, bc, 0, nil)
	if err := setStep(t, failedTask, 1, StepStatusRunning, "", "", "", nil); err != nil {
		t.Fatalf("置 RUNNING 失败: %v", err)
	}
	if err := setStep(t, failedTask, 1, StepStatusFailed, delivery.VerdictExitCode,
		"连接失败 password="+plainPwd, "", intPtr(4)); err != nil {
		t.Fatalf("失败摘要落库异常: %v", err)
	}
	failRow := mustStepRow(t, failedTask, 1)
	if strings.Contains(Str(failRow["fail_detail"]), plainPwd) {
		t.Fatalf("INV-40:失败摘要不得含凭据明文: %q", Str(failRow["fail_detail"]))
	}
	if !strings.Contains(Str(failRow["fail_detail"]), "[已掩码]") {
		t.Fatalf("INV-40:失败摘要应已掩码: %q", Str(failRow["fail_detail"]))
	}
	// REP-12 c5:命令摘要面只有动作 + 参数骨架(不含可执行文件路径、不含凭据)
	stepAll := mustList(t, `SELECT * FROM clc_task_step WHERE task_ref IN (?, ?) AND flag=1`, taskNo, failedTask)
	stepRaw := MarshalJSONVal(stepAll)
	if strings.Contains(stepRaw, plainPwd) || strings.Contains(stepRaw, plainToken) {
		t.Fatal("INV-40:步骤行(含失败摘要)不得含凭据明文")
	}
	if bin := config.Config.Delivery.ToolBin; bin != "" && strings.Contains(stepRaw, bin) {
		t.Fatal("REP-12 c5/INV-40:命令摘要不得暴露可执行文件路径")
	}
}

func TestTaskStepsQueryCaliber(t *testing.T) {
	requireDB(t)
	const bodyMarker = "LOG-BODY-ONLY-MARKER-77"
	useTool(t, `echo "`+bodyMarker+`"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "n1 : ok=1 changed=0 unreachable=0 failed=0"`, time.Minute)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	taskNo := seedTask(t, clusterNo, TaskKindUpgrade, TaskStatusQueued, ExecPathLocal)
	expandSteps(t, taskNo, TaskKindUpgrade, clusterNo, 0, nil)
	enqueueTask(taskNo, DeliveryArgs{})
	waitTaskStatuses(t, taskNo, TaskStatusSucceeded)

	rows, err := ListTaskSteps(taskNo, map[string]any{})
	if err != nil {
		t.Fatalf("FUNC-34 查询失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("REP-12:行集合完全来自步骤数据行,得到 %d 行", len(rows))
	}
	// REP-12 c1~c9 列名齐(c1 序号 / c2 编码与名称 / c3 状态 / c4 判定依据 / c5 命令摘要 /
	// c6 起止时间 / c7 日志区间 / c8 失败摘要 / c9 跳过原因)
	wantCols := []string{"step_index", "step_code", "step_name", "step_status", "command_summary",
		"verdict_source", "exit_code", "started_at", "ended_at",
		"log_start_line", "log_end_line", "fail_detail", "skip_reason"}
	for _, c := range wantCols {
		if _, ok := rows[0][c]; !ok {
			t.Fatalf("REP-12 缺列 %s(c1~c9 口径)", c)
		}
	}
	if _, ok := rows[0]["content"]; ok {
		t.Fatal("REP-12 口径 8:交付步骤查询不得携带日志正文列")
	}
	raw := MarshalJSONVal(rows)
	if strings.Contains(raw, bodyMarker) {
		t.Fatal("REP-12 口径 8:不得回 clc_task_log 全量正文")
	}
	if logCount := countRows(t, `SELECT COUNT(*) FROM clc_task_log WHERE task_ref=? AND flag=1`, taskNo); logCount <= int64(len(rows)) {
		t.Fatalf("自检:日志行数应多于步骤行数(否则无法证伪整表日志出口),log=%d step=%d", logCount, len(rows))
	}
	// REP-12 p1:任务 / 步骤状态 / 判定依据过滤
	if ok, err := ListTaskSteps(taskNo, map[string]any{"step_status": StepStatusSucceeded}); err != nil || len(ok) != 1 {
		t.Fatalf("按状态过滤应命中 1 行: rows=%d err=%v", len(ok), err)
	}
	if ok, err := ListTaskSteps(taskNo, map[string]any{"step_status": StepStatusFailed}); err != nil || len(ok) != 0 {
		t.Fatalf("按失败状态过滤应为空: rows=%d err=%v", len(ok), err)
	}
	if ok, err := ListTaskSteps(taskNo, map[string]any{"verdict_source": delivery.VerdictPlayRecap}); err != nil || len(ok) != 1 {
		t.Fatalf("按判定依据过滤应命中 1 行: rows=%d err=%v", len(ok), err)
	}
	// c7:日志区间可点击进入回放(REP-13)
	if paramInt(rows[0]["log_start_line"]) <= 0 || paramInt(rows[0]["log_end_line"]) < paramInt(rows[0]["log_start_line"]) {
		t.Fatalf("REP-12 c7:日志游标区间应可定位: %+v", rows[0])
	}
	if _, err := ListTaskSteps("TSK-NOT-EXIST", map[string]any{}); err != nil {
		t.Fatalf("空任务查询应返回空集不报错: %v", err)
	}
}

// ---------- G3 权限种子 / G4 流程入库(m5 / m6) ----------

func TestPermissionSeedForDeliveryBehaviors(t *testing.T) {
	requireDB(t)
	if err := EnsureSeed(); err != nil {
		t.Fatalf("种子入库失败: %v", err)
	}
	// 权限行:PERM-FUNC-33/34/35 已入库且指向 m2 行为(m5 permissions)
	for i, code := range []string{"PERM-FUNC-33", "PERM-FUNC-34", "PERM-FUNC-35"} {
		row := mustOne(t, `SELECT code, name, target_type, target_ref, data_scope FROM sys_permission WHERE code=?`, code)
		if row == nil {
			t.Fatalf("G3:%s 未入库(m5 permissions)", code)
		}
		if Str(row["target_type"]) != "BEHAVIOR" || Str(row["target_ref"]) != fmt.Sprintf("FUNC-%02d", 33+i) ||
			Str(row["data_scope"]) != "ALL" {
			t.Fatalf("m5 权限行口径不符: %+v", row)
		}
	}
	// 角色绑定矩阵照抄 m5 roles[].permissions
	want := map[string]map[string]bool{
		"CLC-ROLE-01": {"PERM-FUNC-33": true, "PERM-FUNC-34": true, "PERM-FUNC-35": true},
		"CLC-ROLE-02": {"PERM-FUNC-33": false, "PERM-FUNC-34": true, "PERM-FUNC-35": true},
		"CLC-ROLE-03": {"PERM-FUNC-33": false, "PERM-FUNC-34": true, "PERM-FUNC-35": true},
		"CLC-ROLE-04": {"PERM-FUNC-33": false, "PERM-FUNC-34": false, "PERM-FUNC-35": false},
		"CLC-ROLE-05": {"PERM-FUNC-33": false, "PERM-FUNC-34": false, "PERM-FUNC-35": false},
	}
	for role, perms := range want {
		if r := mustOne(t, `SELECT id FROM sys_role WHERE code=?`, role); r == nil {
			t.Fatalf("m5 角色 %s 未入库", role)
		}
		for pc, expect := range perms {
			n := countRows(t, `
				SELECT COUNT(*) FROM sys_role_permission rp
				JOIN sys_role r ON r.id=rp.role_id
				JOIN sys_permission p ON p.id=rp.permission_id
				WHERE r.code=? AND p.code=?`, role, pc)
			if (n > 0) != expect {
				t.Fatalf("m5 绑定不符:%s %s 期望绑定=%v,实际 %d 行", role, pc, expect, n)
			}
		}
	}
}

func TestFlowProc06GraphPersisted(t *testing.T) {
	requireDB(t)
	if err := EnsureSeed(); err != nil {
		t.Fatalf("种子入库失败: %v", err)
	}
	row := mustOne(t, `SELECT node_graph FROM flow_definition WHERE code=?`, "FLOW-PROC-06")
	if row == nil {
		t.Fatal("G4:FLOW-PROC-06 未入 flow_definition(m6 交付执行与续跑)")
	}
	var g struct {
		Nodes []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			Branches []struct {
				Target    string `json:"target"`
				Condition string `json:"condition"`
			} `json:"branches"`
		} `json:"nodes"`
		Edges []struct {
			Source    string `json:"source"`
			Target    string `json:"target"`
			Condition string `json:"condition"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(Str(row["node_graph"])), &g); err != nil {
		t.Fatalf("node_graph JSON 解析失败: %v", err)
	}
	typeOf := map[string]string{}
	ends := map[string]bool{}
	gateways := 0
	for _, n := range g.Nodes {
		typeOf[n.ID] = n.Type
		switch n.Type {
		case "gateway":
			gateways++
		case "end":
			ends[n.ID] = true
		}
	}
	// m6 A02/A05/A13/A15/A19 五处 gateway:槽位判定、形态判定、单步结论、是否续步、人工出口
	if gateways == 0 {
		t.Fatalf("G4:buildNodeGraph 应产出 gateway 节点(m6 GATEWAY),nodes=%s", Str(row["node_graph"]))
	}
	for _, id := range []string{"A02", "A05", "A13", "A15", "A19"} {
		if typeOf[id] != "gateway" {
			t.Fatalf("m6 FLOW-PROC-06 节点 %s 应为 gateway,得到 %q", id, typeOf[id])
		}
	}
	// 分支边必须真实存在(设计器读 edges,引擎读 branches —— 二者同源)
	condEdges := 0
	out := map[string]map[string]bool{}
	for _, e := range g.Edges {
		if strings.TrimSpace(e.Condition) != "" {
			condEdges++
		}
		if out[e.Source] == nil {
			out[e.Source] = map[string]bool{}
		}
		out[e.Source][e.Target] = true
	}
	if condEdges == 0 {
		t.Fatal("G4:gateway 分支必须生成带条件的边(REF-16/R-18/REF-15 落点)")
	}
	for _, gw := range []string{"A02", "A05", "A13", "A15", "A19"} {
		if len(out[gw]) < 2 {
			t.Fatalf("m6 gateway %s 应有分支边,得到 %v", gw, out[gw])
		}
	}
	// 4 个 END(A16/A21/A23/A25)可达:成功 / 失败 / 失联 / 取消终局
	if len(ends) != 4 {
		t.Fatalf("m6 endActivities 应为 4 个终局(A16/A21/A23/A25),得到 %v", ends)
	}
	reachable := map[string]bool{"A01": true}
	queue := []string{"A01"}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range out[cur] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	for end := range ends {
		if !reachable[end] {
			t.Fatalf("G4:终局节点 %s 从 A01 不可达(分支丢失)", end)
		}
	}

	// m6 SUB_FLOW_CALL 节点保留 sub_flow_ref(PROC-01/02 复用 PROC-06)
	sub := mustOne(t, `SELECT node_graph FROM flow_definition WHERE code=?`, "FLOW-PROC-02")
	if sub == nil {
		t.Fatal("G4:FLOW-PROC-02 未入库")
	}
	var sg struct {
		Nodes []struct {
			Type       string `json:"type"`
			SubFlowRef string `json:"sub_flow_ref"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(Str(sub["node_graph"])), &sg); err != nil {
		t.Fatalf("FLOW-PROC-02 node_graph 解析失败: %v", err)
	}
	subFlowCalls := 0
	for _, n := range sg.Nodes {
		if n.Type == "sub_flow_call" {
			subFlowCalls++
			if n.SubFlowRef != "FLOW-PROC-06" {
				t.Fatalf("m6 SUB_FLOW_CALL 节点须保留 sub_flow_ref(FLOW-PROC-06),得到 %q", n.SubFlowRef)
			}
		}
	}
	if subFlowCalls != 2 {
		t.Fatalf("m6 FLOW-PROC-02 A08/A09 两个子流程调用节点应入库,得到 %d", subFlowCalls)
	}
}

// ---------- 业务入口 → 交付内核贯通(FLOW-PROC-06 A02) ----------
//
// 本组用例锁死"台账模拟"回潮:交付动作入口只落 QUEUED 与前置台账,
// 步骤展开、单步推进、终态与领域落点一律由交付内核触达(clc_task.go / clc_node.go)。

func taskNosOf(t *testing.T, clusterNo string) []string {
	t.Helper()
	rows := mustList(t, `SELECT task_no FROM clc_task WHERE cluster_ref=? AND flag=1 ORDER BY id`, clusterNo)
	var nos []string
	for _, r := range rows {
		nos = append(nos, Str(r["task_no"]))
	}
	return nos
}

func TestStartInstallRunsInDeliveryKernel(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusDraft)
	if _, err := StartInstall(Int(mustClusterRow(t, clusterNo)["id"]), map[string]any{}, testUser); err != nil {
		t.Fatalf("StartInstall 失败: %v", err)
	}
	nos := taskNosOf(t, clusterNo)
	if len(nos) != 1 {
		t.Fatalf("FUNC-03 应创建 1 个交付任务,得到 %d", len(nos))
	}
	taskNo := nos[0]
	row := waitTaskStatuses(t, taskNo, TaskStatusSucceeded)
	if Str(row["task_kind"]) != TaskKindInstall || trimEmpty(Str(row["started_at"])) == "" {
		t.Fatalf("内核须接管并写 started_at(INV-29): %+v", row)
	}
	if got := len(stepsByStatus(t, taskNo, StepStatusSucceeded)); got != 7 {
		t.Fatalf("INV-45/B-AUTO-10:入口贯通应展开并推成 7 步,得到 %d", got)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task_log WHERE task_ref=? AND flag=1`, taskNo); n == 0 {
		t.Fatalf("交付日志须由内核落库游标(INV-43),得到 %d 行", n)
	}
	if !strings.Contains(Str(row["result_summary"]), "安装成功") {
		t.Fatalf("R-13 成功落点摘要缺失: %q", Str(row["result_summary"]))
	}
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusRunning || trimEmpty(Str(c["master_endpoints"])) == "" ||
		trimEmpty(Str(c["kubeconfig_ref"])) == "" {
		t.Fatalf("B-AUTO-09/R-13:成功落点应置 RUNNING 并登记主控入口与凭据标识: %+v", c)
	}
	waitAllSlotsFree(t, rt)
}

func TestStartInstallFailStepInjectionLandsInKernel(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusDraft)
	id := Int(mustClusterRow(t, clusterNo)["id"])
	if _, err := StartInstall(id, map[string]any{"fail_step_index": 3}, testUser); err != nil {
		t.Fatalf("StartInstall(注入第 3 步失败)失败: %v", err)
	}
	taskNo := taskNosOf(t, clusterNo)[0]
	row := waitTaskStatuses(t, taskNo, TaskStatusFailed)
	if paramInt(row["fail_step_index"]) != 3 || !strings.Contains(Str(row["result_summary"]), "第 3 步失败") {
		t.Fatalf("INV-30:失败步骤索引与摘要须由内核回写: %+v", row)
	}
	if got := len(stepsByStatus(t, taskNo, StepStatusSucceeded)); got != 2 {
		t.Fatalf("注入前 2 步应有结论,得到 %d", got)
	}
	failed := stepsByStatus(t, taskNo, StepStatusFailed)
	if len(failed) != 1 || paramInt(failed[0]["step_index"]) != 3 {
		t.Fatalf("R-14:仅第 3 步置 FAILED,得到 %+v", failed)
	}
	if got := len(stepsByStatus(t, taskNo, StepStatusCancelled)); got != 4 {
		t.Fatalf("INV-41/R-14:后续 4 步应置 CANCELLED,得到 %d", got)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task_log WHERE task_ref=? AND flag=1`, taskNo); n == 0 {
		t.Fatalf("失败链同样须落交付日志(游标只增),得到 %d 行", n)
	}
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusInstallFailed || paramInt(c["install_step_index"]) != 3 {
		t.Fatalf("R-09:失败写回集群状态与续跑索引: %+v", c)
	}

	// FUNC-04 续跑:必须建 RETRY_INSTALL 任务(R-17 展开 SKIPPED),而非再建 INSTALL
	if _, err := RetryInstall(id, map[string]any{}, testUser); err != nil {
		t.Fatalf("RetryInstall 失败: %v", err)
	}
	nos := taskNosOf(t, clusterNo)
	if len(nos) != 2 {
		t.Fatalf("续跑应新增 1 个任务,得到 %d 个", len(nos))
	}
	retryNo := nos[1]
	retry := waitTaskStatuses(t, retryNo, TaskStatusSucceeded)
	if Str(retry["task_kind"]) != TaskKindRetryInstall {
		t.Fatalf("m2 FUNC-04:续跑任务类别须为 RETRY_INSTALL,得到 %s", Str(retry["task_kind"]))
	}
	if got := len(stepsByStatus(t, retryNo, StepStatusSkipped)); got != 2 {
		t.Fatalf("R-17/INV-37:已成功的前 2 步应置 SKIPPED,得到 %d", got)
	}
	if got := len(stepsByStatus(t, retryNo, StepStatusSucceeded)); got != 5 {
		t.Fatalf("R-17:第 3~7 步应推成 5 步成功,得到 %d", got)
	}
	c = mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusRunning || c["install_step_index"] != nil {
		t.Fatalf("B-AUTO-09:续跑成功落点应置 RUNNING 并清空续跑索引: %+v", c)
	}
	waitAllSlotsFree(t, rt)
}

func TestDestroyApprovalQueuesAndKernelLandsDestroyed(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	mustSQL(t, `
		INSERT INTO clc_binding (binding_no, cluster_ref, target_capability, target_kind, target_ref, purpose)
		VALUES (?, ?, 'CERT', 'IDP', 'CERT-DESTROY-1', 'CLUSTER_CERT')`,
		fmt.Sprintf("BND-T%05d", nextSeq()), clusterNo)

	// 审批通过回调在事务内:只建 QUEUED 任务 + 集群 DESTROYING(INV-33 结论为前置,R-07 不在此伪造终态)
	if err := tx(func(db *gorm.DB) error { return SyncClusterDestroyFromFlow(db, clusterNo, "APPROVED") }); err != nil {
		t.Fatalf("销毁审批同步失败: %v", err)
	}
	taskNo := taskNosOf(t, clusterNo)[0]
	if s := taskStatus(t, taskNo); s != TaskStatusQueued {
		t.Fatalf("FLOW-PROC-05:审批通过应落 QUEUED,当前 %s", s)
	}
	if s := Str(mustClusterRow(t, clusterNo)["status"]); s != ClusterStatusDestroying {
		t.Fatalf("m2 FUNC-31 后置:审批通过应置 DESTROYING,当前 %s", s)
	}
	// 排队接管:看护取执行权的等价触发(R-18,测试内不依赖 ticker)
	waitTryStart(t, rt, taskNo)
	waitTaskStatuses(t, taskNo, TaskStatusSucceeded)
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusDestroyed || trimEmpty(Str(c["destroyed_at"])) == "" {
		t.Fatalf("R-07 落点:卸载成功后应置 DESTROYED 并登记销毁时间: %+v", c)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_binding WHERE cluster_ref=? AND flag=1`, clusterNo); n != 0 {
		t.Fatalf("R-07:级联清理引用台账未完成,残留 %d 项", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE cluster_no=?`, clusterNo); n != 1 {
		t.Fatalf("INV-02:DESTROYED 为终态保留行(逻辑删除由他域承担),得到 %d 行", n)
	}
	waitAllSlotsFree(t, rt)
}

func TestNodeDeliveryActionsRunInKernel(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	id := Int(mustClusterRow(t, clusterNo)["id"])
	if _, err := AddNode(id, map[string]any{"ip_address": "10.2.3.4", "node_role": "WORKER"}, testUser); err != nil {
		t.Fatalf("AddNode 失败: %v", err)
	}
	addTask := taskNosOf(t, clusterNo)[0]
	waitTaskStatuses(t, addTask, TaskStatusSucceeded)
	n := mustOne(t, `SELECT * FROM clc_cluster_node WHERE cluster_ref=? AND flag=1`, clusterNo)
	if Str(n["node_status"]) != NodeStatusAdded || !strings.Contains(Str(n["k8s_nodename"]), "10-2-3-4") {
		t.Fatalf("INV-14:加入成功后回填状态与 K8s 节点名: %+v", n)
	}

	// 移除:入口只置 REMOVING,REMOVED 终局行由内核成功落点写入(INV-13)
	if _, err := RemoveNode(Int(n["id"]), map[string]any{}, testUser); err != nil {
		t.Fatalf("RemoveNode 失败: %v", err)
	}
	delTask := taskNosOf(t, clusterNo)[1]
	waitTaskStatuses(t, delTask, TaskStatusSucceeded)
	after := mustOne(t, `SELECT * FROM clc_cluster_node WHERE id=?`, Int(n["id"]))
	if Str(after["node_status"]) != NodeStatusRemoved || paramInt(after["flag"]) != 1 {
		t.Fatalf("INV-13:REMOVED 终局保留行(flag=1),得到 %+v", after)
	}
	waitAllSlotsFree(t, rt)
}

// TestInstallSuccessLandsRegisteredNodes 四步向导登记的 PENDING 节点在集群安装成功后加入
// (INV-14:与 ADD_NODE 成功落点同一份语义;安装链路不再把登记节点留在 PENDING)。
func TestInstallSuccessLandsRegisteredNodes(t *testing.T) {
	requireDB(t)
	rt := useTool(t, `echo "exec $3"`+"\n"+
		`echo "PLAY RECAP ****"`+"\n"+
		`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	created, err := CreateCluster(map[string]any{
		"name":               fmt.Sprintf("向导安装节点验收-%d", nextSeq()),
		"cluster_dns_domain": "wizard.test",
		"version_ref":        "v1.30.4",
		"nodes": []any{
			map[string]any{"ip_address": "10.9.8.7", "node_role": "MASTER"},
			map[string]any{"ip_address": "10.9.8.8", "node_role": "WORKER"},
		},
	}, testUser)
	if err != nil {
		t.Fatalf("CreateCluster(四步向导)失败: %v", err)
	}
	clusterNo, id := Str(created["cluster_no"]), Int(created["id"])
	if n := countRows(t, `SELECT COUNT(*) FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status=?`,
		clusterNo, NodeStatusPending); n != 2 {
		t.Fatalf("向导第二步应登记 2 个 PENDING 节点,得到 %d", n)
	}

	if _, err := StartInstall(id, map[string]any{}, testUser); err != nil {
		t.Fatalf("StartInstall 失败: %v", err)
	}
	taskNo := taskNosOf(t, clusterNo)[0]
	row := waitTaskStatuses(t, taskNo, TaskStatusSucceeded)
	if !strings.Contains(Str(row["result_summary"]), "登记节点 2 个已加入") {
		t.Fatalf("INV-14:安装成功摘要须含登记节点加入结论: %q", Str(row["result_summary"]))
	}
	for _, r := range mustList(t, `SELECT * FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 ORDER BY id`, clusterNo) {
		want := strings.ReplaceAll(Str(r["ip_address"]), ".", "-")
		if Str(r["node_status"]) != NodeStatusAdded || !strings.Contains(Str(r["k8s_nodename"]), want) {
			t.Fatalf("INV-14:安装成功后登记节点应置 ADDED 并回填 K8s 节点名: %+v", r)
		}
	}
	waitAllSlotsFree(t, rt)
}

// TestDestroyFailureCanReapplyAndReland DESTROY 失败不复位销毁链:集群停在 DESTROYING,
// 允许再次排队执行(仍需审批结论为前置,INV-33/R-10);存在 QUEUED/RUNNING DESTROY 任务时判为在途不可重复创建。
func TestDestroyFailureCanReapplyAndReland(t *testing.T) {
	requireDB(t)
	mark := filepath.Join(t.TempDir(), "calls")
	rt := useTool(t, fmt.Sprintf(
		`n=$(cat '%s' 2>/dev/null || echo 0); echo $((n+1)) > '%s'; `+
			`if [ "$n" -lt 1 ]; then echo "销毁执行模拟失败(验证条件)"; exit 1; fi; `+
			`echo "exec $3"; echo "PLAY RECAP ****"; `+
			`echo "node-1 : ok=1 changed=0 unreachable=0 failed=0 skipped=0"`, mark, mark), time.Minute)
	newFakeRemote([]string{"UNUSED"}, "IGN-NA").attach(t, rt)

	clusterNo := seedCluster(t, ClusterStatusRunning)
	if err := tx(func(db *gorm.DB) error { return SyncClusterDestroyFromFlow(db, clusterNo, "APPROVED") }); err != nil {
		t.Fatalf("首次销毁审批同步失败: %v", err)
	}
	first := taskNosOf(t, clusterNo)[0]
	waitTryStart(t, rt, first)
	waitTaskStatuses(t, first, TaskStatusFailed)
	if s := Str(mustClusterRow(t, clusterNo)["status"]); s != ClusterStatusDestroying {
		t.Fatalf("INV-28:销毁确定失败后集群停在 DESTROYING 等重试,得到 %s", s)
	}
	if no, ok := destroyInFlight(clusterNo); ok {
		t.Fatalf("任务 %s 已 FAILED,不应仍判为在途", no)
	}
	// FUNC-05 侧同一依据:DESTROYING + 在途 ⇒ 拦截;DESTROYING + 已出失败结论 ⇒ 放行到再次审批
	if err := EnsureSeed(); err != nil {
		t.Fatalf("种子入库失败: %v", err)
	}
	blockedNo := seedCluster(t, ClusterStatusDestroying)
	seedTask(t, blockedNo, TaskKindDestroy, TaskStatusRunning, ExecPathLocal)
	if _, err := SubmitDestroy(Int(mustClusterRow(t, blockedNo)["id"]), map[string]any{"reason": "重复提交保护验收"}, testUser); err == nil ||
		!strings.Contains(err.Error(), "排队/执行中") {
		t.Fatalf("R-10:DESTROYING 且有在途 DESTROY 任务时必须拦截,得到 %v", err)
	}
	retryableNo := seedCluster(t, ClusterStatusDestroying)
	seedTask(t, retryableNo, TaskKindDestroy, TaskStatusFailed, ExecPathLocal)
	if _, err := SubmitDestroy(Int(mustClusterRow(t, retryableNo)["id"]), map[string]any{"reason": "销毁失败后重新申请"}, testUser); err != nil {
		t.Fatalf("上次销毁已出失败结论时应允许重新提交销毁申请(R-10 仍需审批),得到 %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM flow_instance i JOIN flow_definition d ON d.id=i.def_id
		WHERE d.code=? AND i.business_key=? AND i.status='RUNNING'`, flowDestroyApp, retryableNo); n != 1 {
		t.Fatalf("重新提交应生成 1 个在途销毁审批实例(R-10 强制不免审批),得到 %d", n)
	}

	// 失败后重新排队:同一份审批通过结论再次生效(不引入新行为/新权限)
	if err := tx(func(db *gorm.DB) error { return SyncClusterDestroyFromFlow(db, clusterNo, "APPROVED") }); err != nil {
		t.Fatalf("DESTROY 失败后应可重新排队销毁: %v", err)
	}
	nos := taskNosOf(t, clusterNo)
	if len(nos) != 2 {
		t.Fatalf("重新排队应新建第 2 个 DESTROY 任务,得到 %v", nos)
	}
	if no, ok := destroyInFlight(clusterNo); !ok {
		t.Fatalf("R-10/INV-48:存在排队中的 DESTROY 任务时应判为在途(当前 %v)", no)
	}
	if err := tx(func(db *gorm.DB) error { return SyncClusterDestroyFromFlow(db, clusterNo, "APPROVED") }); err == nil {
		t.Fatal("在途 DESTROY 任务未出结论前重复排队必须被拦截(R-10)")
	}

	waitTryStart(t, rt, nos[1])
	waitTaskStatuses(t, nos[1], TaskStatusSucceeded)
	c := mustClusterRow(t, clusterNo)
	if Str(c["status"]) != ClusterStatusDestroyed || trimEmpty(Str(c["destroyed_at"])) == "" {
		t.Fatalf("重试销毁应到达 DESTROYED 终态并登记销毁时间: %+v", c)
	}
	if _, ok := destroyInFlight(clusterNo); ok {
		t.Fatal("DESTROYED 后不应再有在途 DESTROY 任务")
	}
	waitAllSlotsFree(t, rt)
}
