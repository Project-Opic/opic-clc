// clc_delivery.go —— FLOW-PROC-06 交付执行内核胶水(B-AUTO-09~13 的落地)。
//
// 分层可插拔适配器(裁定一):LOCAL = 后端直调交付工具进程(delivery.Local),
// REMOTE_EXECUTOR = 经点火中心提交 + 回执对账(delivery.Ignition);形态登记后运行期不互转(REF-16)。
// 步骤权威在后端(裁定二):任务创建事务内展开步骤行(INV-45 / R-17),单步推进取最小非终态序号(INV-39);
// 日志通道(裁定三):逐行落库前掩码(INV-42)+ 游标只增(INV-43)+ SSE 回放 + FUNC-35 REST 兜底;
// 执行机即后端容器(裁定四):失联只由进程不可存活判定(B-AUTO-11),执行位置登记值只读(INV-32)。
package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// CLC_STEP_STATUS 步骤状态(m1 子实体 ClusterTaskStep;INV-41 终局保护)。
const (
	StepStatusPending   = "PENDING"
	StepStatusRunning   = "RUNNING"
	StepStatusSucceeded = "SUCCEEDED"
	StepStatusFailed    = "FAILED"
	StepStatusCancelled = "CANCELLED"
	StepStatusSkipped   = "SKIPPED"
)

const (
	// skipReasonResume 续跑跳过原因(R-17 / m1 skipReason 固定文本,界面不内置步骤清单)。
	skipReasonResume = "上次任务该步已成功（INV-03 不重跑）"
	// maxLogRunes 日志单行长度上限(超出截断并标注,m1 TaskLogLine.content 描述)。
	maxLogRunes = 4000
)

// stepTerminalStatuses INV-41:终局不可回退。
var stepTerminalStatuses = map[string]bool{
	StepStatusSucceeded: true, StepStatusFailed: true,
	StepStatusCancelled: true, StepStatusSkipped: true,
}

// DeliveryArgs 交付任务运行期参数(命令面之外的一次性入参;凭据零持久化,
// 凭据只以存放标识传递,进程环境注入由配置解析)。重启后缺失项按登记状态派生。
type DeliveryArgs struct {
	NodeIPs         []string // ADD_NODE / DEL_NODE 目标节点地址
	MasterEndpoints string   // 安装类成功落点覆盖值(缺省按集群 DNS 派生)
	KubeconfigRef   string   // 访问凭据存放标识覆盖值(缺省 vault 路径;INV-08 只登记标识)
	VersionRef      string   // UPGRADE 目标版本引用
	NodeName        string   // ADD_NODE 成功落点的 K8s 节点名覆盖值(缺省按集群号 + 地址派生,INV-14)
	FailStep        int      // 故障注入步骤序号(仅验证用:该步不落交付工具,直接走失败回写链 R-09/INV-30)
}

// stepPlanItem 展开计划的一行(纯数据,便于脱离 DB 单测)。
type stepPlanItem struct {
	Index      int
	Code       string
	Name       string
	Status     string
	SkipReason string
}

// buildStepPlan 步骤展开计划(INV-45 先展开后执行 / R-17 续跑展开,纯函数):
// 安装类按 CLC_INSTALL_STEP 字典展开 01..07;续跑取失败步骤索引,resumeFrom 之前置 SKIPPED;
// 非安装类展开单步(编码 = 任务类别同名,m1 stepCode 描述)。
func buildStepPlan(kind string, installItems []map[string]string, kindName string, resumeFrom int) []stepPlanItem {
	if kind != TaskKindInstall && kind != TaskKindRetryInstall {
		return []stepPlanItem{{Index: 1, Code: kind, Name: kindName, Status: StepStatusPending}}
	}
	var plan []stepPlanItem
	for i, it := range installItems {
		idx := i + 1
		item := stepPlanItem{Index: idx, Code: it["code"], Name: it["label"], Status: StepStatusPending}
		if kind == TaskKindRetryInstall && resumeFrom > 0 && idx < resumeFrom {
			item.Status = StepStatusSkipped
			item.SkipReason = skipReasonResume
		}
		plan = append(plan, item)
	}
	return plan
}

// installStepItems 安装步骤字典项(权威展示名,界面不内置清单;本体取不到时回退编码口径)。
func installStepItems() []map[string]string {
	items := ontologyDictItems("DICT-CLC_INSTALL_STEP", "CLC_INSTALL_STEP")
	if len(items) == 0 {
		fallback := []string{"环境准备", "部署控制面存储", "部署容器运行时", "部署控制面", "加入工作节点", "部署网络插件", "部署扩展组件"}
		for i, name := range fallback {
			items = append(items, map[string]string{"code": fmt.Sprintf("%02d", i+1), "label": name})
		}
	}
	return items
}

// taskKindName 任务类别展示名(单步任务的步骤名称来源)。
func taskKindName(kind string) string {
	for _, it := range ontologyDictItems("DICT-CLC_TASK_KIND", "CLC_TASK_KIND") {
		if it["code"] == kind {
			return it["label"]
		}
	}
	return kind
}

// ---------- 运行期单例(槽位/互斥/适配器/游标/SSE 总线) ----------

type taskRunner struct {
	cancel context.CancelFunc
	reason string // 取消原因(FUNC-33 必填,落取消链摘要)
}

type taskLogger struct {
	mu   sync.Mutex
	next int // 0 = 未初始化(自 DB MAX(line_no)+1 装载);INV-43 游标只增
}

type logEvent struct {
	TaskNo string
	Row    map[string]any
}

type logHub struct {
	mu   sync.Mutex
	subs map[int]chan logEvent
	seq  int
}

func (h *logHub) subscribe(taskNo string) (int, chan logEvent) {
	_ = taskNo // 单域任务量级小:总线按广播 + 订阅端过滤(保持 hub 简单)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ch := make(chan logEvent, 256)
	h.subs[h.seq] = ch
	return h.seq, ch
}

func (h *logHub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
}

func (h *logHub) publish(taskNo string, row map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- logEvent{TaskNo: taskNo, Row: row}:
		default: // 慢消费者丢弃(游标兜底:回放端以 DB 续查补差,INV-43 不丢数)
		}
	}
}

type deliveryRuntime struct {
	mu      sync.Mutex
	slots   *delivery.Slots
	locks   *delivery.ClusterLock
	local   *delivery.Local
	remote  *delivery.Ignition
	runners map[string]*taskRunner
	loggers map[string]*taskLogger
	hub     *logHub
	args    map[string]DeliveryArgs // taskNo → 运行期参数(入队时登记)
	stop    chan struct{}
}

var (
	dl     *deliveryRuntime
	dlOnce sync.Once
)

// deliveryRT 交付内核运行期单例(惰性:槽位/超时取平台参数,形态取部署配置)。
func deliveryRT() *deliveryRuntime {
	dlOnce.Do(func() {
		slotLimit := int(paramInt(platformParam("clc.exec_slot_limit", "2")))
		timeoutMin := int(paramInt(platformParam("clc.step_timeout_minutes", "45")))
		if timeoutMin <= 0 {
			timeoutMin = 45 // REF-15 缺省 45 分钟
		}
		cfg := config.Config.Delivery
		poll := time.Duration(cfg.IgnitionPollSeconds) * time.Second
		dl = &deliveryRuntime{
			slots: delivery.NewSlots(slotLimit),
			locks: delivery.NewClusterLock(),
			local: delivery.NewLocal(delivery.Config{ToolBin: cfg.ToolBin, ToolDir: cfg.ToolDir,
				KubeconfigEnv: cfg.KubeconfigEnv, KubeconfigPathPattern: cfg.KubeconfigPathPattern,
				ExtraEnv: cfg.ExtraEnv, StepTimeout: time.Duration(timeoutMin) * time.Minute}),
			remote: delivery.NewIgnition(delivery.IgnitionConfig{
				BaseURL: cfg.IgnitionBaseURL, Token: cfg.IgnitionToken,
				SubmitPath: cfg.IgnitionSubmitPath, ReceiptPath: cfg.IgnitionReceiptPath,
				CancelPath: cfg.IgnitionCancelPath, PollInterval: poll,
			}),
			runners: map[string]*taskRunner{},
			loggers: map[string]*taskLogger{},
			hub:     &logHub{subs: map[int]chan logEvent{}},
			args:    map[string]DeliveryArgs{},
			stop:    make(chan struct{}),
		}
	})
	return dl
}

// adapterFor 按任务登记形态取适配器(REF-16:运行期不互转,登记表决定;INV-32 执行位置只读)。
func (rt *deliveryRuntime) adapterFor(execPath string) delivery.Adapter {
	if execPath == ExecPathRemoteExecutor {
		return rt.remote
	}
	return rt.local
}

// checkExecPathRegistered REF-16 的配置侧同源:登记执行位置前先确认该链路在本部署确实可用 ——
// 远程要求点火中心地址已配置、本地要求交付工具路径已配置,否则提交即刻拒绝。
// 放行的话任务会先排队、执行时才发现链路不存在(失联 + 集群降级),而登记值必须是可达的真实位置(INV-32/INV-46)。
func checkExecPathRegistered(execPath string) error {
	cfg := config.Config.Delivery
	switch execPath {
	case ExecPathLocal:
		if strings.TrimSpace(cfg.ToolBin) == "" {
			return bizRuleFail("REF-16", "本地链路未配置交付工具路径(部署配置 delivery.tool_bin),不可登记执行位置 LOCAL")
		}
		return nil
	case ExecPathRemoteExecutor:
		if strings.TrimSpace(cfg.IgnitionBaseURL) == "" {
			return bizRuleFail("INV-46", "远程链路未配置点火中心地址(部署配置 delivery.ignition_base_url),不可登记执行位置 REMOTE_EXECUTOR(REF-16)")
		}
		return nil
	default:
		return bizRuleFail("INV-32", "执行位置「%s」不在 CLC_EXEC_PATH 字典取值内(LOCAL / REMOTE_EXECUTOR)", execPath)
	}
}

// ---------- 步骤展开与单步推进 ----------

// expandStepsTx 任务创建事务内展开步骤行(INV-45 先展开后执行;R-17 续跑 SKIPPED;
// INV-37 序号唯一自 1 连续由唯一索引兜底;幂等:已有步骤行直接返回)。
func expandStepsTx(db *gorm.DB, taskNo, kind, clusterNo string, resumeFrom int, nodeIPs []string, user UserInfo) error {
	cnt, err := store.Count(db, `SELECT COUNT(*) FROM clc_task_step WHERE task_ref=? AND flag=1`, taskNo)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return nil // 幂等(重复展开不得产生第二行集,INV-37)
	}
	plan := buildStepPlan(kind, installStepItems(), taskKindName(kind), resumeFrom)
	toolBin := config.Config.Delivery.ToolBin
	for _, it := range plan {
		summary := ""
		if kind != "" && toolBin != "" {
			args, err := delivery.BuildStepArgs(toolBin, kind, clusterNo, it.Code, nodeIPs)
			if err != nil {
				return err // 提交时强制:三类入参命中白名单(INV-47),不合法即拒绝建任务
			}
			summary = delivery.MaskSecret(delivery.RenderCommandSummary(args))
		}
		if _, err := store.Exec(db, `
			INSERT INTO clc_task_step (task_ref, step_index, step_code, step_name, step_status,
				command_summary, skip_reason, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			taskNo, it.Index, it.Code, it.Name, it.Status, summary, nilIfEmpty(it.SkipReason), user.ID, user.ID); err != nil {
			return err
		}
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nextRunnableStep INV-39 单步推进:取任务内最小非终态序号步骤行(无则已完成)。
func nextRunnableStep(taskNo string) (map[string]any, error) {
	return qOne(`
		SELECT * FROM clc_task_step
		WHERE task_ref=? AND flag=1 AND step_status NOT IN ('SUCCEEDED','FAILED','CANCELLED','SKIPPED')
		ORDER BY step_index ASC LIMIT 1`, taskNo)
}

// stepStatusTx 读步骤当前状态(终局保护判定用)。
func stepStatusTx(db *gorm.DB, taskNo string, idx int) (string, error) {
	row, err := store.One(db, `SELECT step_status FROM clc_task_step WHERE task_ref=? AND step_index=? AND flag=1`, taskNo, idx)
	if err != nil || row == nil {
		return "", err
	}
	return Str(row["step_status"]), nil
}

// markStepTx 步骤状态回写(INV-38 终态必填判定依据与结束时间;INV-41 终局保护:
// 已终态行不再被改写 —— UPDATE 带非终态条件,不命中即静默跳过)。
func markStepTx(db *gorm.DB, taskNo string, idx int, status, verdict string, exitCode *int,
	failDetail, skipReason string, logEnd *int) error {
	if stepTerminalStatuses[status] && status != StepStatusCancelled && status != StepStatusSkipped {
		if verdict == "" {
			return bizRuleFail("INV-38", "步骤进入终态(%s)必须登记判定依据", status)
		}
	}
	if status == StepStatusFailed && failDetail == "" {
		return bizRuleFail("INV-38", "步骤失败(FAILED)必须登记失败摘要")
	}
	if status == StepStatusSkipped && skipReason == "" {
		return bizRuleFail("INV-38", "步骤跳过(SKIPPED)必须登记跳过原因")
	}
	detail := failDetail
	if detail != "" {
		detail = delivery.MaskSecret(detail) // INV-40:失败摘要落库前掩码
	}
	var set []string
	var args []any
	set = append(set, "step_status=?", "updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')")
	args = append(args, status)
	switch status {
	case StepStatusRunning:
		set = append(set, "started_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')")
	case StepStatusSucceeded, StepStatusFailed, StepStatusCancelled, StepStatusSkipped:
		set = append(set, "ended_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')")
		if verdict != "" {
			set = append(set, "verdict_source=?")
			args = append(args, verdict)
		}
		if exitCode != nil {
			set = append(set, "exit_code=?")
			args = append(args, *exitCode)
		}
		if logEnd != nil {
			set = append(set, "log_end_line=?")
			args = append(args, *logEnd)
		}
	}
	if detail != "" {
		set = append(set, "fail_detail=?")
		args = append(args, detail)
	}
	if skipReason != "" {
		set = append(set, "skip_reason=?")
		args = append(args, skipReason)
	}
	args = append(args, taskNo, idx)
	// INV-41:WHERE 携带非终态守卫,终局行不回退
	_, err := store.Exec(db, fmt.Sprintf(`UPDATE clc_task_step SET %s
		WHERE task_ref=? AND step_index=? AND flag=1
		AND step_status NOT IN ('SUCCEEDED','FAILED','CANCELLED','SKIPPED')`, joinSet(set)), args...)
	return err
}

func joinSet(set []string) string {
	out := ""
	for i, s := range set {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// cancelPendingStepsTx R-14/R-15:后续未执行(PENDING)步骤置未执行即取消。
func cancelPendingStepsTx(db *gorm.DB, taskNo string, fromIdx int) error {
	_, err := store.Exec(db, `
		UPDATE clc_task_step SET step_status=?, ended_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'),
			updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE task_ref=? AND flag=1 AND step_status=? AND step_index >= ?`,
		StepStatusCancelled, taskNo, StepStatusPending, fromIdx)
	return err
}

// ---------- 日志落库(掩码 + 游标 + SSE 总线) ----------

// nextLineNo 任务内日志游标取号(只增,INV-43;插入失败也不回退,重放端以 DB 续查补差)。
func (rt *deliveryRuntime) nextLineNo(taskNo string) int {
	lg := rt.loggerFor(taskNo)
	lg.mu.Lock()
	defer lg.mu.Unlock()
	n := rt.cursor(lg, taskNo)
	lg.next++
	return n
}

// peekLineNo 预读下一个游标号而不占用:日志区间左界定位用(A08),
// 否则区间会漏掉本步开始行并留下无日志行的空洞号。
func (rt *deliveryRuntime) peekLineNo(taskNo string) int {
	lg := rt.loggerFor(taskNo)
	lg.mu.Lock()
	defer lg.mu.Unlock()
	return rt.cursor(lg, taskNo)
}

// cursor 当前待分配游标(须持 lg.mu;首用自 DB MAX+1 装载,断线重放与重启续写安全)。
func (rt *deliveryRuntime) cursor(lg *taskLogger, taskNo string) int {
	if lg.next == 0 {
		if row, err := qOne(`SELECT COALESCE(MAX(line_no),0)+1 AS nx FROM clc_task_log WHERE task_ref=?`, taskNo); err == nil {
			lg.next = int(Int(row["nx"]))
		} else {
			lg.next = 1
		}
	}
	return lg.next
}

// loggerFor 任务日志游标(懒建,同一任务只对应一个游标实例)。
func (rt *deliveryRuntime) loggerFor(taskNo string) *taskLogger {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	lg, ok := rt.loggers[taskNo]
	if !ok {
		lg = &taskLogger{}
		rt.loggers[taskNo] = lg
	}
	return lg
}

// writeTaskLog 单行日志落库:掩码在写之前(INV-42,原文不留存),超长截断标注;
// 成功落库后经 SSE 总线广播。stepIdx 为空 = 任务级输出。
func writeTaskLog(taskNo string, stepIdx *int, level, text string) {
	masked := delivery.MaskSecret(text)
	if r := []rune(masked); len(r) > maxLogRunes {
		masked = string(r[:maxLogRunes]) + "…(已截断)"
	}
	rt := deliveryRT()
	line := rt.nextLineNo(taskNo)
	if _, err := qExec(`
		INSERT INTO clc_task_log (task_ref, line_no, step_index, logged_at, log_level, content)
		VALUES (?, ?, ?, ?, ?, ?)`, taskNo, line, stepIdx, nowText(), level, masked); err != nil {
		// 日志行高频旁路:落库失败不阻塞执行链(游标已前进,重放以 DB 为准)
		return
	}
	rt.hub.publish(taskNo, map[string]any{
		"line_no": line, "step_index": stepIdx, "logged_at": nowText(), "log_level": level, "content": masked,
	})
}

// SubscribeTaskLogs SSE 订阅(回放端先 DB 续查、后经此通道追增量)。
func SubscribeTaskLogs(taskNo string) (func(), <-chan map[string]any) {
	rt := deliveryRT()
	id, ch := rt.hub.subscribe(taskNo)
	out := make(chan map[string]any, 256)
	go func() {
		defer close(out)
		for ev := range ch {
			if ev.TaskNo == taskNo {
				out <- ev.Row
			}
		}
	}()
	return func() { rt.hub.unsubscribe(id) }, out
}

// LogHubSubscribers 当前日志总线订阅数(装配自检口径:客户端断开后必须归零,无协程泄漏)。
func LogHubSubscribers() int {
	rt := deliveryRT()
	rt.hub.mu.Lock()
	defer rt.hub.mu.Unlock()
	return len(rt.hub.subs)
}

// ---------- 任务入队 / 槽位 / 互斥 / 执行 ----------

// enqueueTask 登记运行期参数并尝试立即启动(槽位占满 ⇒ 保持 QUEUED 由 janitor 按序推进,R-18)。
func enqueueTask(taskNo string, args DeliveryArgs) {
	rt := deliveryRT()
	rt.mu.Lock()
	rt.args[taskNo] = args
	rt.mu.Unlock()
	rt.tryStart(taskNo)
}

// resolveArgs 取运行期参数;重启后缺失时按登记状态派生(节点增删的目标即当前非终态节点)。
func (rt *deliveryRuntime) resolveArgs(task map[string]any) DeliveryArgs {
	rt.mu.Lock()
	a, ok := rt.args[Str(task["task_no"])]
	rt.mu.Unlock()
	if ok {
		return a
	}
	return DeliveryArgs{NodeIPs: deriveNodeIPs(Str(task["task_kind"]), Str(task["cluster_ref"]))}
}

// deriveNodeIPs 从节点台账派生增删目标 IP(ADD_NODE ⇒ PENDING;DEL_NODE ⇒ REMOVING)。
func deriveNodeIPs(kind, clusterNo string) []string {
	status := ""
	switch kind {
	case TaskKindAddNode:
		status = NodeStatusPending
	case TaskKindDelNode:
		status = NodeStatusRemoving
	default:
		return nil
	}
	rows, err := qList(`SELECT ip_address FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status=? ORDER BY id`,
		clusterNo, status)
	if err != nil {
		return nil
	}
	var ips []string
	for _, r := range rows {
		ips = append(ips, Str(r["ip_address"]))
	}
	return ips
}

// isInstallFamily 安装族任务类别(展开 01~07 步骤、由被调用方按步骤编码落安装动作)。
func isInstallFamily(kind string) bool {
	return kind == TaskKindInstall || kind == TaskKindRetryInstall
}

// stepNodeSpecs 交付门面请求体的节点清单(R-19 三档取用 / O-IGN 侧裁定 Q-72:清单进请求体,两侧各校验一次)。
//
// 安装族 ⇒ 台账在册节点全量(地址 + 角色 + K8s 节点名):被调用方据此派生集群交付配置,
// 拓扑权威始终在本域台账(每步重取,事实变了就跟着改);
// 节点增删 ⇒ 只带本次目标地址(角色留空,由命令面按"加一个工作节点/摘一个在册节点"处理);
// 其余族 ⇒ 不带清单(对端口径:非节点增删类动作携带清单即请求形态拒绝)。
func stepNodeSpecs(kind, clusterNo string, args DeliveryArgs) []delivery.NodeSpec {
	switch kind {
	case TaskKindInstall, TaskKindRetryInstall:
		return deriveInstallNodes(clusterNo)
	case TaskKindAddNode, TaskKindDelNode:
		var out []delivery.NodeSpec
		for _, ip := range args.NodeIPs {
			out = append(out, delivery.NodeSpec{Addr: ip})
		}
		return out
	}
	return nil
}

// deriveInstallNodes 安装族的在册节点拓扑:PENDING(向导已登记、安装未落点)+ ADDED(续跑时已在册)。
// REMOVING/REMOVED 不参与新装 —— 那是摘除链的中间/终局态。
//
// 节点名未回填时按 INV-14 口径派生下发:被调用方把该名字写进交付配置,真实集群里的节点名
// 才与成功落点回填的台账值一致(否则界面名单与 kubectl 看到的两样)。
func deriveInstallNodes(clusterNo string) []delivery.NodeSpec {
	rows, err := qList(`SELECT ip_address, node_role, k8s_nodename FROM clc_cluster_node
		WHERE cluster_ref=? AND flag=1 AND node_status IN (?, ?) ORDER BY id`,
		clusterNo, NodeStatusPending, NodeStatusAdded)
	if err != nil {
		return nil
	}
	var out []delivery.NodeSpec
	for _, r := range rows {
		ip := strings.TrimSpace(Str(r["ip_address"]))
		if ip == "" {
			continue
		}
		name := trimEmpty(Str(r["k8s_nodename"]))
		if name == "" {
			name = deriveK8sNodeName(clusterNo, ip)
		}
		out = append(out, delivery.NodeSpec{Addr: ip, Role: Str(r["node_role"]), Nodename: name})
	}
	return out
}

// tryStart 取得全局槽位(REF-14)+ 同集群互斥(INV-48)后启动 QUEUED 任务。
func (rt *deliveryRuntime) tryStart(taskNo string) bool {
	task, err := qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil || task == nil || Str(task["task_status"]) != TaskStatusQueued {
		return false
	}
	return rt.acquireAndRun(task)
}

// acquireAndRun 槽位 + 集群互斥(进程内快道 + DB 强校验兜底)满足即启动执行协程;
// 同时用于重启接管 RUNNING 远程任务(B-AUTO-12 对账接管路径)。
func (rt *deliveryRuntime) acquireAndRun(task map[string]any) bool {
	taskNo := Str(task["task_no"])
	clusterNo := Str(task["cluster_ref"])
	if !rt.slots.TryAcquire() {
		return false // 槽位占满 ⇒ 排队 QUEUED 而不是失败(R-18)
	}
	if ok, _ := rt.locks.TryLock(clusterNo, taskNo); !ok {
		rt.slots.Release()
		return false
	}
	// INV-48 DB 强校验兜底:同集群已有其他 RUNNING 任务即让位
	if n, err := qCount(`SELECT COUNT(*) FROM clc_task WHERE cluster_ref=? AND flag=1 AND task_status=? AND task_no <> ?`,
		clusterNo, TaskStatusRunning, taskNo); err == nil && n > 0 {
		rt.locks.Unlock(clusterNo, taskNo)
		rt.slots.Release()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.mu.Lock()
	rt.runners[taskNo] = &taskRunner{cancel: cancel}
	rt.mu.Unlock()
	go rt.execute(ctx, task)
	return true
}

// execute 单步推进主循环(INV-39):取最小非终态步骤 → 直调/提交 → 结论分发落行,直至终局。
func (rt *deliveryRuntime) execute(ctx context.Context, task map[string]any) {
	taskNo := Str(task["task_no"])
	clusterNo := Str(task["cluster_ref"])
	kind := Str(task["task_kind"])
	execPath := Str(task["execution_path"])
	defer func() {
		rt.mu.Lock()
		delete(rt.runners, taskNo)
		delete(rt.args, taskNo)
		delete(rt.loggers, taskNo)
		rt.mu.Unlock()
		rt.locks.Unlock(clusterNo, taskNo)
		rt.slots.Release()
	}()
	user := UserInfo{ID: Int(task["created_by"]), Username: Str(task["submitted_by"])}
	args := rt.resolveArgs(task)
	// 交付版本引用按 R-19 仅安装族下发:取集群登记的版本引用(FUNC-01 四步向导;可空 ⇒ 对端沿用发行包自身版本)。
	// UPGRADE 的目标版本不经该列下发:改写集群交付配置里的版本面归点火中心的交付配置对齐职责,本域不代答。
	clusterVersion := ""
	if isInstallFamily(kind) {
		if row, err := qOne(`SELECT version_ref FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo); err == nil && row != nil {
			clusterVersion = trimEmpty(Str(row["version_ref"]))
		}
	}

	// QUEUED → RUNNING(带守卫:已被取消/终态即中止,INV-29 终局不被覆盖)
	var running map[string]any
	if err := tx(func(db *gorm.DB) error {
		if _, err := store.Exec(db, `
			UPDATE clc_task SET task_status=?, started_at=COALESCE(started_at, to_char(now(),'YYYY-MM-DD HH24:MI:SS')),
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE task_no=? AND flag=1 AND task_status=?`,
			TaskStatusRunning, taskNo, TaskStatusQueued); err != nil {
			return err
		}
		// INV-45 兜底:进入执行前步骤行必须齐备(创建事务已展开;缺行 = 平台数据异常,补展开)
		cluster, err := store.One(db, `SELECT install_step_index FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
		if err != nil {
			return err
		}
		resumeFrom := 0
		if kind == TaskKindRetryInstall && cluster != nil {
			resumeFrom = int(paramInt(cluster["install_step_index"]))
		}
		if err := expandStepsTx(db, taskNo, kind, clusterNo, resumeFrom, args.NodeIPs, user); err != nil {
			return err
		}
		row, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=?`, taskNo)
		running = row
		return err
	}); err != nil || running == nil || Str(running["task_status"]) != TaskStatusRunning {
		return // 未获执行权(已被取消或他副本持有)
	}
	writeTaskLog(taskNo, nil, delivery.LevelInfo, fmt.Sprintf("任务开始执行:类别 %s,执行位置 %s(INV-32 登记值只读)", kind, execPath))

	adapter := rt.adapterFor(execPath)
	for {
		if ctx.Err() != nil {
			rt.applyCancelChain(taskNo, kind, args, user, "人工取消")
			return
		}
		step, err := nextRunnableStep(taskNo)
		if err != nil || step == nil {
			break
		}
		idx := int(paramInt(step["step_index"]))
		if Str(step["step_status"]) == StepStatusRunning && execPath == ExecPathRemoteExecutor && Str(task["remote_task_ref"]) != "" {
			// 重启恢复:远程链路以既有远端引用对账接管(B-AUTO-12),不重复提交
			out := rt.reconcileStep(ctx, taskNo, idx, Str(task["remote_task_ref"]))
			switch {
			case out.Success:
				ec := out.ExitCode
				endLine := rt.currentCursor(taskNo)
				_ = tx(func(db *gorm.DB) error {
					return markStepTx(db, taskNo, idx, StepStatusSucceeded, out.Verdict, &ec, "", "", &endLine)
				})
				continue
			case out.Lost:
				rt.applyStalledChain(taskNo, out, user)
			case out.Cancelled:
				rt.applyCancelChain(taskNo, kind, args, user, out.Detail)
			default:
				rt.applyFailChain(taskNo, kind, idx, user, out, args)
			}
			return
		}
		stepCode := Str(step["step_code"])
		stepIdx := idx
		sink := func(l delivery.Line) { writeTaskLog(taskNo, &stepIdx, l.Level, l.Text) }
		if err := tx(func(db *gorm.DB) error {
			return markStepTx(db, taskNo, idx, StepStatusRunning, "", nil, "", "", nil)
		}); err != nil {
			writeTaskLog(taskNo, &stepIdx, delivery.LevelError, "步骤状态回写失败: "+err.Error())
			continue
		}
		// 游标区间起点(A08):开始行自身即本步日志首行,预读取号不占号
		startLine := rt.peekLineNo(taskNo)
		writeTaskLog(taskNo, &stepIdx, delivery.LevelInfo,
			fmt.Sprintf("步骤 %02d %s 开始(命令面: %s)", idx, stepCode, Str(step["command_summary"])))
		rt.markLogStart(taskNo, idx, startLine)

		if args.FailStep == idx {
			// 故障注入(仅验证用):该步不落交付工具,直接按失败结论走回写链(R-09/INV-30/R-14)
			writeTaskLog(taskNo, &stepIdx, delivery.LevelError,
				fmt.Sprintf("故障注入:第 %d 步判定失败(验证条件,不经交付工具执行)", idx))
			rt.applyFailChain(taskNo, kind, idx, user,
				delivery.Outcome{Verdict: delivery.VerdictExitCode, Detail: "故障注入(验证):该步按注入条件判定失败", ExitCode: 1}, args)
			return
		}

		// 远程链路只登记标识(R-19:来源任务号/步骤序号/集群标识/步骤编码/节点清单/版本引用),
		// 命令面归点火中心私有;本地链路才在本域渲染 argv(INV-47)。
		ref := delivery.StepRef{TaskNo: taskNo, ClusterID: clusterNo, StepIndex: idx,
			StepCode: stepCode, Nodes: stepNodeSpecs(kind, clusterNo, args), VersionRef: clusterVersion}
		if adapter.Form() == delivery.FormLocal {
			built, err := delivery.BuildStepArgs(config.Config.Delivery.ToolBin, kind, clusterNo, stepCode, args.NodeIPs)
			if err != nil {
				rt.applyFailChain(taskNo, kind, idx, user,
					delivery.Outcome{Verdict: delivery.VerdictExitCode, Detail: "命令契约拒绝: " + err.Error() + "(INV-47)"}, args)
				return
			}
			ref.Args = built
		}
		out := adapter.Execute(ctx, ref, sink)
		if out.RemoteRef != "" && Str(task["remote_task_ref"]) != out.RemoteRef {
			// INV-46:远程链路远端任务引用必写(不写 = 登记值与真实位置不符)
			if _, err := qExec(`UPDATE clc_task SET remote_task_ref=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE task_no=? AND flag=1`,
				out.RemoteRef, taskNo); err == nil {
				task["remote_task_ref"] = out.RemoteRef
			}
		}
		endLine := rt.currentCursor(taskNo)
		switch {
		case out.Success:
			ec := out.ExitCode
			if err := tx(func(db *gorm.DB) error {
				return markStepTx(db, taskNo, idx, StepStatusSucceeded, out.Verdict, &ec, "", "", &endLine)
			}); err != nil {
				writeTaskLog(taskNo, &stepIdx, delivery.LevelError, "步骤终态落行失败: "+err.Error())
			}
			continue
		case out.Lost:
			rt.applyStalledChain(taskNo, out, user)
			return
		case out.Cancelled:
			rt.applyCancelChain(taskNo, kind, args, user, out.Detail)
			return
		default:
			rt.applyFailChain(taskNo, kind, idx, user, out, args)
			return
		}
	}
	// 全部步骤终局 ⇒ B-AUTO-09 成功落点
	rt.applySuccessChain(taskNo, clusterNo, kind, args, user)
}

// markLogStart / currentCursor 日志游标区间(A08:logStartLine/logEndLine)。
func (rt *deliveryRuntime) markLogStart(taskNo string, idx, line int) {
	_, _ = qExec(`UPDATE clc_task_step SET log_start_line=? WHERE task_ref=? AND step_index=? AND flag=1`, line, taskNo, idx)
}

// closeStepWindow 失败/取消链收口日志区间右界(A08):已按结论行落库后取当前游标,
// 已闭合的区间(成功链)不回改。
func (rt *deliveryRuntime) closeStepWindow(taskNo string, idx int) {
	_, _ = qExec(`UPDATE clc_task_step SET log_end_line=?
		WHERE task_ref=? AND step_index=? AND flag=1 AND log_end_line IS NULL`,
		rt.currentCursor(taskNo), taskNo, idx)
}

func (rt *deliveryRuntime) currentCursor(taskNo string) int {
	rt.mu.Lock()
	lg, ok := rt.loggers[taskNo]
	rt.mu.Unlock()
	if ok {
		lg.mu.Lock()
		defer lg.mu.Unlock()
		if lg.next > 0 {
			return lg.next - 1
		}
	}
	if row, err := qOne(`SELECT COALESCE(MAX(line_no),0) AS mx FROM clc_task_log WHERE task_ref=?`, taskNo); err == nil {
		return int(Int(row["mx"]))
	}
	return 0
}

// reconcileStep 重启恢复/对账: 以既有远端引用轮询回执(R-16 以远端为准, 不重复提交)。
func (rt *deliveryRuntime) reconcileStep(ctx context.Context, taskNo string, idx int, remoteRef string) delivery.Outcome {
	stepIdx := idx
	sink := func(l delivery.Line) { writeTaskLog(taskNo, &stepIdx, l.Level, l.Text) }
	writeTaskLog(taskNo, &stepIdx, delivery.LevelInfo, "回执对账接管:以远端引用 "+remoteRef+" 为准(B-AUTO-12,不重复提交)")
	return rt.remote.Reconcile(ctx, remoteRef, sink)
}

// taskSuccessHook 任务成功落点回调(per-kind;B-AUTO-09 与 PROC-01/02/05 各落点)。
type taskSuccessHook func(db *gorm.DB, task, cluster map[string]any, args DeliveryArgs, user UserInfo) error

var taskSuccessHooks = map[string]taskSuccessHook{}

// 安装类成功落点(B-AUTO-09 / R-13):末步有结论 ⇒ 集群 RUNNING + 主控入口与凭据存放标识登记。
// 与 FUNC-03 手工发起路径共用 installSuccessLandingTx(同一份落点语义,两处留痕口径不同)。
func init() {
	landing := func(db *gorm.DB, task, cluster map[string]any, args DeliveryArgs, user UserInfo) error {
		return installSuccessLandingTx(db, cluster, task, args.MasterEndpoints, args.KubeconfigRef, "B-AUTO-09", user)
	}
	taskSuccessHooks[TaskKindInstall] = landing
	taskSuccessHooks[TaskKindRetryInstall] = landing
}

func (rt *deliveryRuntime) applySuccessChain(taskNo, clusterNo, kind string, args DeliveryArgs, user UserInfo) {
	_err := tx(func(db *gorm.DB) error {
		task, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
		if err != nil {
			return err
		}
		if task == nil || Str(task["task_status"]) != TaskStatusRunning {
			return nil // 终局保护:已被取消/失联处置,不再覆盖
		}
		cluster, err := store.One(db, `SELECT * FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
		if err != nil {
			return err
		}
		if hook, ok := taskSuccessHooks[kind]; ok {
			if err := hook(db, task, cluster, args, user); err != nil {
				return err
			}
		} else if err := finishTaskTx(db, task, TaskStatusSucceeded, nil, "交付动作完成(单步契约,剧本汇总判定)", user); err != nil {
			return err
		}
		return nil
	})
	if _err != nil {
		writeTaskLog(taskNo, nil, delivery.LevelError, "成功落点回写失败: "+_err.Error())
		return
	}
	writeTaskLog(taskNo, nil, delivery.LevelInfo, "任务到达成功终局(全部步骤有结论,B-AUTO-09/成功落点回写完成)")
}

// applyFailChain R-14 失败回写链:该步 FAILED + 判定依据与失败摘要 → 后续 PENDING 置 CANCELLED →
// 任务 FAILED + 失败步骤索引任务/集群双写(INV-30 同源)。
func (rt *deliveryRuntime) applyFailChain(taskNo, kind string, idx int, user UserInfo, out delivery.Outcome, args DeliveryArgs) {
	ec := out.ExitCode
	verdict := out.Verdict
	if verdict == "" {
		verdict = delivery.VerdictExitCode
	}
	if out.TimedOut {
		verdict = delivery.VerdictExitCode // 超时按失败结论落行(REF-15),判定依据回落退出码口径
	}
	detail := out.Detail
	if detail == "" {
		detail = "执行结论:失败"
	}
	if out.TimedOut {
		detail = "单步超时(REF-15):" + detail
	}
	err := tx(func(db *gorm.DB) error {
		task, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
		if err != nil {
			return err
		}
		if task == nil || Str(task["task_status"]) != TaskStatusRunning {
			return nil
		}
		if err := markStepTx(db, taskNo, idx, StepStatusFailed, verdict, &ec, detail, "", nil); err != nil {
			return err
		}
		if err := cancelPendingStepsTx(db, taskNo, idx+1); err != nil {
			return err
		}
		fi := idx
		summary := delivery.MaskSecret(fmt.Sprintf("第 %d 步失败:%s", idx, detail))
		return finishTaskTx(db, task, TaskStatusFailed, &fi, summary, user)
	})
	if err != nil {
		writeTaskLog(taskNo, nil, delivery.LevelError, "失败回写链落库异常: "+err.Error())
		return
	}
	stepIdx := idx
	writeTaskLog(taskNo, &stepIdx, delivery.LevelError, fmt.Sprintf("失败回写链完成(R-14):步骤 %02d FAILED,任务 FAILED,失败步骤索引已双写(INV-30)", idx))
	rt.closeStepWindow(taskNo, idx) // 结论行即本步末行(A08 右界)
}

// applyCancelChain R-15 取消处置:运行中步骤 FAILED(摘要「人工取消」)→ 未执行步骤 CANCELLED →
// 任务 CANCELLED;已终态步骤不回改(INV-41);取消不是回滚(FUNC-33)。
func (rt *deliveryRuntime) applyCancelChain(taskNo, kind string, args DeliveryArgs, user UserInfo, reasonDetail string) {
	runningIdx := 0 // 取消时的运行中步骤(其日志区间须在链末收口)
	err := tx(func(db *gorm.DB) error {
		task, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
		if err != nil {
			return err
		}
		if task == nil || (Str(task["task_status"]) != TaskStatusRunning && Str(task["task_status"]) != TaskStatusQueued) {
			return nil
		}
		// 运行中步骤(至多一个,INV-39)置 FAILED,摘要人工取消
		run, err := store.One(db, `SELECT step_index FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status=? ORDER BY step_index LIMIT 1`,
			taskNo, StepStatusRunning)
		if err != nil {
			return err
		}
		from := 1
		resumeIdx := 0
		if run != nil {
			resumeIdx = int(paramInt(run["step_index"]))
			runningIdx = resumeIdx
			from = resumeIdx
			if err := markStepTx(db, taskNo, resumeIdx, StepStatusFailed, delivery.VerdictExitCode, nil, "人工取消:"+reasonDetail, "", nil); err != nil {
				return err
			}
		}
		if (kind == TaskKindInstall || kind == TaskKindRetryInstall) && resumeIdx == 0 {
			// 步骤间隙取消:续跑索引 = 首个未执行步骤序号(取数须在置 CANCELLED 之前,m2 FUNC-33 后置)
			row, err := store.One(db, `SELECT MIN(step_index) AS mi FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status=?`,
				taskNo, StepStatusPending)
			if err != nil {
				return err
			}
			resumeIdx = int(paramInt(row["mi"]))
		}
		if err := cancelPendingStepsTx(db, taskNo, from); err != nil {
			return err
		}
		// 取消走终态守卫更新(不借 finishTaskTx 的失败摘要口径,原因独立留痕)
		res, err := store.Exec(db, `
			UPDATE clc_task SET task_status=?, result_summary=?, ended_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'),
				updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE task_no=? AND flag=1 AND task_status IN (?, ?)`,
			TaskStatusCancelled, delivery.MaskSecret("取消完成:"+reasonDetail), user.ID, taskNo, TaskStatusQueued, TaskStatusRunning)
		if err != nil {
			return err
		}
		if res == 0 {
			return nil
		}
		// FUNC-33 后置:安装/续跑取消 ⇒ 集群置 INSTALL_FAILED 并保留失败步骤索引(可续跑);其余类别状态不变
		if (kind == TaskKindInstall || kind == TaskKindRetryInstall) && resumeIdx > 0 {
			if _, err := store.Exec(db, `
				UPDATE clc_cluster SET status=?, install_step_index=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
				WHERE cluster_no=? AND flag=1`, ClusterStatusInstallFailed, resumeIdx, Str(task["cluster_ref"])); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeTaskLog(taskNo, nil, delivery.LevelError, "取消处置落库异常: "+err.Error())
		return
	}
	if runningIdx > 0 {
		rt.closeStepWindow(taskNo, runningIdx) // 收口先于任务级结论行(A08 右界=本步末行)
	}
	writeTaskLog(taskNo, nil, delivery.LevelInfo, "取消处置完成(R-15):取消不是回滚,已执行步骤效果由交付工具幂等性承担")
}

// applyStalledChain B-AUTO-11 失联挂起:执行痕迹不可存活(进程不可存活是唯一判据,裁定四);
// 步骤不回改(执行痕迹丢失、无结论),集群降级归因 EXECUTOR_LOST,执行位置登记值不改写(INV-32)。
func (rt *deliveryRuntime) applyStalledChain(taskNo string, out delivery.Outcome, user UserInfo) {
	err := tx(func(db *gorm.DB) error {
		task, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
		if err != nil {
			return err
		}
		if task == nil || Str(task["task_status"]) != TaskStatusRunning {
			return nil
		}
		summary := delivery.MaskSecret("执行失联挂起(B-AUTO-11):" + out.Detail)
		return finishTaskTx(db, task, TaskStatusStalled, nil, summary, user)
	})
	if err != nil {
		writeTaskLog(taskNo, nil, delivery.LevelError, "失联落库异常: "+err.Error())
		return
	}
	writeTaskLog(taskNo, nil, delivery.LevelError,
		"任务置失联挂起(STALLED):恢复路径唯一为人工重试或显式换执行机,执行位置不可改写(INV-32/REF-16)")
}

// ---------- FUNC-33 取消 / FUNC-34 步骤查询 / FUNC-35 日志回放 ----------

// CancelClusterTask FUNC-33 取消执行中任务(取消 ≠ 回滚;m2 前置:仅 QUEUED/RUNNING、DESTROY 拒绝、原因必填)。
func CancelClusterTask(taskNo, reason string, user UserInfo) (map[string]any, error) {
	task, err := qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, NotFound("任务")
	}
	if Str(task["task_kind"]) == TaskKindDestroy {
		return nil, bizRuleFail("R-07", "DESTROY 类任务不可取消:审批通过后即进入不可逆编排")
	}
	status := Str(task["task_status"])
	if status != TaskStatusQueued && status != TaskStatusRunning {
		return nil, bizRuleFail("INV-29", "仅 QUEUED / RUNNING 可取消,当前 %s(终态任务不渲染取消入口)", status)
	}
	if trimEmpty(reason) == "" {
		return nil, Fail(errcode.ErrParam, "取消必须显式填写原因(审计留痕)")
	}
	rt := deliveryRT()
	rt.mu.Lock()
	runner, live := rt.runners[taskNo]
	if live {
		runner.reason = reason
	}
	rt.mu.Unlock()
	if live {
		runner.cancel() // 执行协程经适配器终止进程组/撤销远端后落取消链(R-15)
		WriteAudit(nil, user.ID, user.Username, "FUNC-33", fmt.Sprintf("取消任务 %s(原因:%s):已通知执行协程终止", taskNo, reason))
		return qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	}
	// 无运行协程:QUEUED(或重启后的孤儿 RUNNING)直接走取消链落库
	if status == TaskStatusRunning {
		rt.local.Terminate(taskNo) // 兜底:进程组若仍在本容器内则终止
	}
	if err := tx(func(db *gorm.DB) error {
		return cancelWithoutRunnerTx(db, task, reason, user)
	}); err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-33", fmt.Sprintf("取消任务 %s(原因:%s):未开跑即取消", taskNo, reason))
	writeTaskLog(taskNo, nil, delivery.LevelInfo, "任务取消(FUNC-33,原因:"+reason+"):取消不是回滚")
	return qOne(`SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
}

func cancelWithoutRunnerTx(db *gorm.DB, task map[string]any, reason string, user UserInfo) error {
	taskNo := Str(task["task_no"])
	res, err := store.Exec(db, `
		UPDATE clc_task SET task_status=?, result_summary=?, ended_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'),
			updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE task_no=? AND flag=1 AND task_status IN (?, ?)`,
		TaskStatusCancelled, delivery.MaskSecret("取消完成(未开跑):"+reason), user.ID, taskNo, TaskStatusQueued, TaskStatusRunning)
	if err != nil || res == 0 {
		return err
	}
	kind := Str(task["task_kind"])
	// 续跑索引须在置 CANCELLED 之前取(首个未执行步骤序号,m2 FUNC-33 后置)
	mi := 0
	if kind == TaskKindInstall || kind == TaskKindRetryInstall {
		row, err := store.One(db, `SELECT MIN(step_index) AS mi FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status=?`,
			taskNo, StepStatusPending)
		if err != nil {
			return err
		}
		mi = int(paramInt(row["mi"]))
	}
	if err := cancelPendingStepsTx(db, taskNo, 1); err != nil {
		return err
	}
	if mi > 0 {
		// 取消安装类 ⇒ 集群置 INSTALL_FAILED 保留续跑索引(首个未执行步骤序号)
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET status=?, install_step_index=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_no=? AND flag=1`, ClusterStatusInstallFailed, mi, Str(task["cluster_ref"])); err != nil {
			return err
		}
	}
	return nil
}

// ListTaskSteps FUNC-34 交付步骤查询(REP-12 c1..c9;无整表原始日志,INV-40)。
func ListTaskSteps(taskNo string, p map[string]any) ([]map[string]any, error) {
	where := "WHERE task_ref=? AND flag=1"
	args := []any{taskNo}
	if v := Str(p["step_status"]); v != "" {
		where += " AND step_status = ?"
		args = append(args, v)
	}
	if v := Str(p["verdict_source"]); v != "" {
		where += " AND verdict_source = ?"
		args = append(args, v)
	}
	// REP-12 c1~c9:序号/编码与名称/状态/判定依据/命令摘要/起止时间/日志区间/失败摘要/跳过原因;
	// 不携带任何日志正文列(INV-40 无整表原始日志出口)。
	return qList(`
		SELECT step_index, step_code, step_name, step_status, command_summary,
			verdict_source, exit_code, started_at, ended_at,
			log_start_line, log_end_line, fail_detail, skip_reason
		FROM clc_task_step `+where+` ORDER BY step_index ASC`, args...)
}

// ReadTaskLogs FUNC-35 任务日志回放(REP-13;游标增量:line_no > offset,禁整表拉取,INV-43)。
func ReadTaskLogs(taskNo string, offset int64, limit int, p map[string]any) ([]map[string]any, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	where := "WHERE task_ref=? AND flag=1 AND line_no > ?"
	args := []any{taskNo, offset}
	if v := Str(p["log_level"]); v != "" {
		where += " AND log_level = ?"
		args = append(args, v)
	}
	if si := paramInt(p["step_index"]); si > 0 {
		where += " AND step_index = ?"
		args = append(args, int(si))
	}
	args = append(args, limit)
	return qList(`
		SELECT line_no, step_index, logged_at, log_level, content
		FROM clc_task_log `+where+` ORDER BY line_no ASC LIMIT ?`, args...)
}

// TaskLogSnapshot 回放端点用的任务终局与游标水位。
func TaskLogSnapshot(taskNo string) (status string, maxLine int64, err error) {
	row, err := qOne(`SELECT task_status FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	if err != nil || row == nil {
		return "", 0, err
	}
	mx, err := qCount(`SELECT COALESCE(MAX(line_no),0) FROM clc_task_log WHERE task_ref=? AND flag=1`, taskNo)
	return Str(row["task_status"]), mx, err
}

func trimEmpty(s string) string {
	return strings.TrimSpace(s)
}

// ---------- 后台看护(janitor):排队推进 / 失联清扫 / 对账接管 / 保留期清理 ----------

// StartDeliveryBackground 启动后台协程(main 组合根挂载;幂等)。
func StartDeliveryBackground() {
	rt := deliveryRT()
	rt.mu.Lock()
	already := rt.stop == nil
	rt.mu.Unlock()
	if already {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		retention := time.NewTicker(time.Hour)
		defer retention.Stop()
		// 启动清扫:上一进程遗留的 RUNNING 任务(执行痕迹不可存活 = 失联,B-AUTO-11)
		rt.sweepOrphans()
		for {
			select {
			case <-rt.stop:
				return
			case <-ticker.C:
				rt.sweepOrphans()
				rt.promoteQueued()
			case <-retention.C:
				rt.purgeRetention()
			}
		}
	}()
	// 首次推进一次排队中的任务
	go rt.promoteQueued()
}

// StopDeliveryBackground 优雅退出时停掉后台协程并终止在跑任务(容器即执行机:进程随容器退出)。
func StopDeliveryBackground() {
	rt := deliveryRT()
	rt.mu.Lock()
	if rt.stop != nil {
		close(rt.stop)
		rt.stop = nil
		runners := rt.runners
		rt.mu.Unlock()
		for _, r := range runners {
			r.cancel()
		}
		return
	}
	rt.mu.Unlock()
}

// sweepOrphans 失联判定唯一来源:本副本 RUNNING 任务无执行协程且交付进程不可存活(B-AUTO-11 / 裁定四)。
func (rt *deliveryRuntime) sweepOrphans() {
	rows, err := qList(`SELECT * FROM clc_task WHERE flag=1 AND task_status=? ORDER BY id`, TaskStatusRunning)
	if err != nil {
		return
	}
	for _, task := range rows {
		taskNo := Str(task["task_no"])
		rt.mu.Lock()
		_, live := rt.runners[taskNo]
		rt.mu.Unlock()
		if live {
			continue
		}
		user := UserInfo{ID: Int(task["created_by"]), Username: Str(task["submitted_by"])}
		if Str(task["execution_path"]) == ExecPathRemoteExecutor && Str(task["remote_task_ref"]) != "" {
			// B-AUTO-12:远程链路先以远端引用对账接管(以远端为准,不静默覆盖、不重复提交)
			if rt.acquireAndRun(task) {
				continue
			}
		}
		_ = tx(func(db *gorm.DB) error {
			t, err := store.One(db, `SELECT * FROM clc_task WHERE task_no=? AND flag=1 AND task_status=?`, taskNo, TaskStatusRunning)
			if err != nil || t == nil {
				return err
			}
			return finishTaskTx(db, t, TaskStatusStalled, nil,
				"执行失联(进程不可存活,B-AUTO-11):启动/看护清扫判定", user)
		})
		writeTaskLog(taskNo, nil, delivery.LevelError, "任务置失联挂起(STALLED):执行痕迹不可存活,恢复路径为人工重试(INV-28/B-AUTO-11)")
	}
}

// promoteQueued 槽位释放按排队次序推进(R-18:QUEUED 不是失败,是排队)。
func (rt *deliveryRuntime) promoteQueued() {
	rows, err := qList(`SELECT task_no FROM clc_task WHERE flag=1 AND task_status=? ORDER BY id`, TaskStatusQueued)
	if err != nil {
		return
	}
	for _, r := range rows {
		if !rt.tryStart(Str(r["task_no"])) {
			return // 槽位或互斥未满足:保持排队
		}
	}
}

// purgeRetention B-AUTO-13 / INV-44:日志超保留期(REF-15)逻辑删除并留痕;任务与步骤台账不动。
func (rt *deliveryRuntime) purgeRetention() {
	days := paramInt(platformParam("clc.log_retention_days", "14"))
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -int(days)).Format("2006-01-02 15:04:05")
	n, err := qExec(`UPDATE clc_task_log SET flag=0, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE flag=1 AND logged_at < ?`, cutoff)
	if err != nil {
		return
	}
	if n > 0 {
		WriteAudit(nil, 0, "system", "B-AUTO-13", fmt.Sprintf("交付日志保留期清理(REF-15/%d 天):到期逻辑删除 %d 行,留痕可见;任务与步骤台账不受影响(INV-44)", days, n))
	}
}
