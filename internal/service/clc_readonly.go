// clc_readonly.go —— v1.2 只读代理与前置预检的服务入口
// (m2 FUNC-47 / FUNC-48 / FUNC-49 / FUNC-50;权限 PERM-FUNC-47/48/49/50)。
//
// 四条口径(m2 为准,本文件严格照本宣科):
//
//	FUNC-47 节点 SSH 登录标识复制 —— 展示口径固定为「登录用户@节点地址」拼接串(REF-18);
//	        登录用户取平台配置的 SSH 全局变量口径,与 FUNC-01 第二步裸机模式同源(platformParam
//	        clc.ssh_login_user)。零凭据出口:私钥 / 口令 / 口令指纹 / sshpass 式参数一律不进
//	        接口 / 界面 / 日志 / 报表 / 导出(INV-08 输出面延伸)。复制动作 = 纯前端剪贴板写入:
//	        不产生业务写入、不产生事件、复制行为不留痕(与 FUNC-49 逐次留痕刻意分档)。
//	FUNC-48 集群交付配置代理查看(字段级视图)—— 本域后端经既有交付门面代理读 O-IGN
//	        (消费 delivery.Ignition.ReadConfig):同一令牌、不前端直连、不留副本、不落对象(R-25)。
//	        返回字段键 + 生效值 + 取值来源档 —— 展示对端结论,本域不重算、不合并四层。对端不可达
//	        ⇒ 显式呈现「对端不可达」并保留跳转入口,不缓存旧内容冒充现状(REF-12)。
//	FUNC-49 交付配置原文代理只读视图 —— 独立权限码 PERM-FUNC-49(不随 PERM-FUNC-48 附带,m5 note:
//	        CLC-ROLE-01 需另行授权)。hosts / config.yaml 原文只读、不提供编辑态;消费
//	        ReadConfigRaw,一次一面(HOSTS / CONFIG)。逐次留痕(谁、何时、哪个集群、哪一面原文),
//	        复用本仓既有审计写法。原文中的凭据类字段由对端在流出前完成掩码,本域不二次解析(INV-08;
//	        content_sha256 是掩码前原文摘要,既不落库也不重算)。对端不可达口径同 FUNC-48。
//	FUNC-50 交付前置完备性预检 —— 只读本域事实与既有降级结论,不新增跨中心同步探活
//	        (「引用是否还在」已由 FUNC-27 / B-AUTO-02 承载)。即时判定项:①节点清单非空且含
//	        MASTER(与 FUNC-40 的启用校验同一配方判据),②引用齐备或显式 legacy,③执行链路已
//	        登记且可用(REF-16),④stepMode 已定(INV-50 / REF-17),⑤无与派生冲突的覆写(R-04)。
//	        结论是即时提示、不拦截提交(拦截仍由各 INV / R 在写入瞬间执行);不写本域对象、
//	        不产生事件。queryReportRef: null —— M7 里登记的唯一显式例外,不建报表(无列表形态)。
package service

import (
	"context"
	"fmt"
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ---------- FUNC-47 节点 SSH 登录标识复制 ----------

// sshLoginUser REF-18 全局变量口径的取源:平台参数 clc.ssh_login_user。
// 与 FUNC-01 第二步裸机模式同源 —— 裸机添加节点不建 SSH 用户字段、凭据零持久化,登录用户
// 只能是平台级全局变量而非节点属性;缺省 root 是运维常识值,不代表任何凭据(INV-08)。
func sshLoginUser() string {
	return platformParam("clc.ssh_login_user", "root")
}

// ClusterNodeLoginIdentities FUNC-47(REP-16 节点登录标识)按集群列出在册非 REMOVED 节点的
// 登录标识拼接串(REF-18「登录用户@节点地址」)。
//
// 只出标识、不出任何凭据值:本函数不查任何凭据字段(本域压根不建该字段),响应体只携带
// node_no / node_role / ip_address / login_identity / takeover_note;复制动作本身 = 前端剪贴板
// 写入,后端不写业务表、不发事件、不留痕(与 FUNC-49 刻意分档:登录标识本就是运维常识,
// 逐次留痕会把审计面淹没有效信息)。
//
// R-03 注记:资产下架后地址保留供人工接管(asset_ref 置空但 ip_address 不清洗,INV-12);
// 该情形在界面上同屏显示接管注记,让运维知道这条标识背后不是资产在册的健康节点。
func ClusterNodeLoginIdentities(clusterID int64) (map[string]any, error) {
	cluster, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(cluster["cluster_no"])
	sshUser := sshLoginUser()
	rows, err := qList(`SELECT node_no, node_role, ip_address, asset_ref, k8s_nodename
		FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status<>? ORDER BY id ASC`,
		clusterNo, NodeStatusRemoved)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		ip := Str(r["ip_address"])
		// REF-18 拼接串固定口径:登录用户@节点地址(不含端口、不含凭据、不含 shell 引号转义)。
		identity := sshUser + "@" + ip
		takeover := strings.TrimSpace(Str(r["asset_ref"])) == ""
		note := ""
		if takeover {
			note = "R-03 人工接管值(资产引用已置空、地址保留,登录前请先核实机器现状)"
		}
		items = append(items, map[string]any{
			"node_no":        Str(r["node_no"]),
			"node_role":      Str(r["node_role"]),
			"ip_address":     ip,
			"login_identity": identity,
			"takeover":       takeover,
			"takeover_note":  note,
		})
	}
	return map[string]any{
		"cluster_no":      clusterNo,
		"cluster_name":    Str(cluster["name"]),
		"ssh_login_user":  sshUser,
		"items":           items,
		"identity_format": "登录用户@节点地址(REF-18)",
		"caliber_note":    "零凭据出口(INV-08 输出面延伸):接口 / 界面 / 日志 / 报表 / 导出一律不含私钥、口令、口令指纹或 sshpass 式参数;复制动作 = 前端剪贴板写入,后端不产生业务写入、不发事件、不留痕",
	}, nil
}

// ---------- FUNC-48 集群交付配置代理查看(字段级视图) ----------

// IgnDeliveryConfigView FUNC-48(REP-17;EXT-EXTERNAL / alias ign_cluster_config):
// 经既有交付门面代理读 O-IGN 字段级视图,返回体透传对端 flat JSON。
//
// R-25 只读代理的三条硬约束在实现里的落点:
//   - 同一令牌:复用 deliveryRT().remote(部署期已注入 cfg.IgnitionToken),不新建链路、不前端直连;
//   - 不留副本、不落对象:任何分支一律不 INSERT/UPDATE 本域表(即便成功);
//   - 对端不可达不缓存冒充现状:显式回 peer_unreachable=true + 保留跳转入口(jump_entry),
//     由界面据 peer_unreachable 呈现「对端不可达」并给出对端地址的跳转链接(REF-12)。
//
// 对端返回 4xx/5xx(HTTP 语义错误)与「未配置 / 传输层失败」(PeerStatus ok=false)在错误文案
// 上分档:前者是「对端已答但拒绝」,后者是「本部署没接线 / 网络层不可达」;两者都不给假数据、
// 都不回退缓存 —— 用户看到的是「现状不可查,请跳转对端」的显式结论。
func IgnDeliveryConfigView(clusterID int64, fileFace, paramTemplateRef, versionRef string) (map[string]any, error) {
	cluster, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(cluster["cluster_no"])
	q := delivery.ConfigQuery{
		FileFace:         strings.ToUpper(strings.TrimSpace(fileFace)),
		ParamTemplateRef: paramTemplateRef,
		VersionRef:       versionRef,
	}
	out, err := deliveryRT().remote.ReadConfig(context.Background(), clusterNo, q)
	if err != nil {
		return proxyFailureEnvelope("FUNC-48", clusterNo, err), nil
	}
	if out == nil {
		out = map[string]any{}
	}
	// 本域只补一条「不是对端语义、只为界面提示」的读侧结论标签,不改写对端字段
	out["cluster_no"] = clusterNo
	out["cluster_name"] = Str(cluster["name"])
	out["peer_unreachable"] = false
	out["local_caliber_note"] = "字段级视图由 O-IGN 承载,本域只代理读、不重算、不合并四层、不留副本(R-25)"
	return out, nil
}

// ---------- FUNC-49 交付配置原文代理只读视图 ----------

// IgnDeliveryConfigRawView FUNC-49(REP-18;EXT-EXTERNAL / alias ign_config_raw):
// hosts / config.yaml 原文只读代理;一次一面;逐次留痕。
//
// 与 FUNC-48 分档:
//   - 独立权限码 PERM-FUNC-49 —— 不随 PERM-FUNC-48 附带,由路由中间件按 m5 挂口
//     (m5 note:CLC-ROLE-01 需另行授权;服务层不重复鉴权,由 middleware.RequirePermission 承担);
//   - 每次调用写一条审计(无论成功或对端不可达)—— FUNC-49 是「谁在何时看了哪个集群哪一面」
//     的强合规视图,不留副本 / 不写本域对象 / 不产生事件,只落审计。
//
// INV-08 输出面延伸:原文中的凭据类字段由对端在流出前完成掩码,本域不二次解析
// (再掩码一次即等于承认本域持有原文);content_sha256 是掩码前原文摘要,既不落库也不重算。
func IgnDeliveryConfigRawView(clusterID int64, fileFace string, user UserInfo) (map[string]any, error) {
	face := strings.ToUpper(strings.TrimSpace(fileFace))
	if face != "HOSTS" && face != "CONFIG" {
		return nil, Fail(errcode.ErrParam,
			"原文视图一次只出一面, file_face 只能是 HOSTS 或 CONFIG(收到 %q)——缺参或越界一律拒绝,不合并、不默认", fileFace)
	}
	cluster, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(cluster["cluster_no"])
	out, readErr := deliveryRT().remote.ReadConfigRaw(context.Background(), clusterNo, face)
	// 逐次留痕:谁(用户 ID + 用户名)/ 何时(审计行 created_at)/ 哪个集群(cluster_no)/ 哪一面(file_face)
	// 无论成功或对端不可达都留痕 —— 读尝试本身就是审计对象(与 FUNC-47 复制行为刻意不留痕相反)。
	auditDetail := fmt.Sprintf("集群 %s 原文视图(%s 面)读取成功", clusterNo, face)
	if readErr != nil {
		code, msg, ok := delivery.PeerStatus(readErr)
		if ok {
			auditDetail = fmt.Sprintf("集群 %s 原文视图(%s 面)读取被对端拒绝(HTTP %d:%s)", clusterNo, face, code, msg)
		} else {
			auditDetail = fmt.Sprintf("集群 %s 原文视图(%s 面)读取失败:对端不可达(%s)", clusterNo, face, readErr.Error())
		}
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-49", auditDetail)
	if readErr != nil {
		env := proxyFailureEnvelope("FUNC-49", clusterNo, readErr)
		env["file_face"] = face
		return env, nil
	}
	if out == nil {
		out = map[string]any{}
	}
	out["cluster_no"] = clusterNo
	out["cluster_name"] = Str(cluster["name"])
	out["peer_unreachable"] = false
	out["local_caliber_note"] = "原文只读、不提供编辑态;凭据形态的行已由对端在流出前整行掩码(INV-08),本域不二次解析、不据原文做判定"
	return out, nil
}

// ---------- 只读代理的公共不可达信封(FUNC-48 / FUNC-49 同一档口径) ----------

// proxyFailureEnvelope 对端不可达 / 对端拒绝时返回的统一信封(200 + 显式结论):
// 不返回缓存值冒充现状(REF-12),不吞掉错误也不升级为业务失败,而是把「现在拿不到」如实
// 呈现给界面 —— 界面据 peer_unreachable 决定是否显式提示 + 保留跳转入口(jump_entry)。
//
// 未配置对端地址 vs 传输层失败 vs 对端返回 4xx/5xx 三种情形在文案上分档(「部署侧未接线」
// 「网络层不可达」「对端已答但拒绝」),因为运维处置路径不同;三者共同点是「本域不缓存冒充」。
//
// 参数 file_face / param_template_ref / version_ref 一律不缓存旧值;jump_entry 从 config 取当前
// 配置的 IgnitionBaseURL,不硬编码。
func proxyFailureEnvelope(funcID, clusterNo string, err error) map[string]any {
	env := map[string]any{
		"cluster_no":       clusterNo,
		"peer_unreachable": true,
		"notice":           "对端不可达 / 对端拒绝回答:界面须显式呈现不可达并保留跳转入口,不缓存旧内容冒充现状(REF-12 / R-25)",
	}
	if code, msg, ok := delivery.PeerStatus(err); ok {
		env["peer_error_code"] = code
		env["peer_error_msg"] = msg
		env["unreachable_kind"] = "peer_answered"
		env["unreachable_reason"] = fmt.Sprintf("对端(O-IGN)已答但拒绝本次读取(HTTP %d:%s)——请核对集群在对端是否已受理、令牌是否有效、参数面 / 版本引用是否命中", code, msg)
	} else {
		env["unreachable_kind"] = "unreachable"
		env["unreachable_reason"] = fmt.Sprintf("对端(O-IGN)不可达:%s——请检查部署配置 delivery.ignition_base_url 与点火中心网络连通性", err.Error())
	}
	if base := strings.TrimSpace(config.Config.Delivery.IgnitionBaseURL); base != "" {
		env["jump_entry"] = fmt.Sprintf("%s/ui/cluster/%s", strings.TrimRight(base, "/"), clusterNo)
	} else {
		env["jump_entry"] = ""
	}
	env["func_ref"] = funcID
	return env
}

// ---------- FUNC-50 交付前置完备性预检 ----------

// DeliveryReadinessCheck FUNC-50(m2 QUERY,queryReportRef: null,不建报表 / 无列表形态):
// 即时判定五档、逐条给中文结论 + 缺哪个补哪个的处置指引。
//
// 只读本域事实与既有降级结论,不新增跨中心同步探活 —— 「引用是否还在」由 FUNC-27 / B-AUTO-02
// 承载;判定项 ① 与 FUNC-40 的启用校验同一配方判据(节点组非空且至少一行 MASTER),不写两套相异
// 判据;判定项 ④ 与 INV-50 / REF-17 的 resolveStepMode / checkStepMode 同一口径。
//
// 结论是即时提示、不拦截提交(拦截由各 INV / R 在写入瞬间执行);不写本域对象、不产生事件。
func DeliveryReadinessCheck(clusterID int64) (map[string]any, error) {
	cluster, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(cluster["cluster_no"])
	runMode := Str(cluster["run_mode"])

	// ① 节点清单非空且含 MASTER —— 与 FUNC-40 的启用校验同一配方判据。
	nodes, err := qList(`SELECT node_role, node_status FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status<>?`,
		clusterNo, NodeStatusRemoved)
	if err != nil {
		return nil, err
	}
	hasMaster := false
	for _, n := range nodes {
		if Str(n["node_role"]) == NodeRoleMaster {
			hasMaster = true
		}
	}
	checkNodes := checkItem{
		id:    "nodes_nonempty_with_master",
		title: "节点清单非空且含 MASTER(FUNC-40 启用校验同一配方判据)",
		pass:  len(nodes) > 0 && hasMaster,
	}
	if !checkNodes.pass {
		if len(nodes) == 0 {
			checkNodes.note = "集群在册节点清单为空(不含 REMOVED)——请先经集群节点添加入口(FUNC-06)登记至少一行;裸机模式请填 IP,资产模式请选资产"
		} else {
			checkNodes.note = "节点清单非空但无 MASTER 角色——请补齐至少一行 MASTER(INV-11 CLC_NODE_ROLE 字典;交付至少要能描述「谁是主控」)"
		}
	}

	// ② 引用齐备或显式 legacy —— 复用 checkRefConsistency(INV-04 / REF-01 同源)。
	refs := map[string]string{
		"version_ref":          Str(cluster["version_ref"]),
		"executor_ref":         Str(cluster["executor_ref"]),
		"registry_ref":         Str(cluster["registry_ref"]),
		"registry_project_ref": Str(cluster["registry_project_ref"]),
	}
	refErr := checkRefConsistency(runMode, refs)
	checkRefs := checkItem{
		id:    "refs_ready_or_explicit_legacy",
		title: "引用齐备或显式 legacy(INV-04;平台禁用 legacy 时须走 MCC,REF-01)",
		pass:  refErr == nil,
	}
	if refErr != nil {
		checkRefs.note = AsBizErr(refErr).Msg
	}

	// ③ 执行链路已登记且可用(REF-16)—— 复用 checkExecPathRegistered 同源判据(空即不可用)。
	execPath := execPathOf(cluster)
	execErr := checkExecPathRegistered(execPath)
	checkExec := checkItem{
		id:    "execution_path_registered_available",
		title: "执行链路已登记且可用(REF-16;LOCAL 需交付工具路径 / REMOTE_EXECUTOR 需点火中心地址)",
		pass:  execErr == nil,
	}
	checkExec.note = fmt.Sprintf("当前登记执行链路:%s", execPath)
	if execErr != nil {
		checkExec.note = "登记执行链路 " + execPath + " —— " + AsBizErr(execErr).Msg
	}

	// ④ stepMode 已定(INV-50 / REF-17)—— 取集群最近一条非终态任务的 step_mode,缺省即 AUTO
	// (REF-17:创建入口缺省;向导入口显式落 STEP)。taskStepMode() 已保证「列缺失/空值按 AUTO」
	// ⇒ 无在途任务时按向导缺省 STEP 报「未起任务、进入向导即落 STEP」。
	running, err := qOne(`SELECT task_no, step_mode FROM clc_task
		WHERE cluster_ref=? AND flag=1 AND task_status NOT IN (?, ?, ?, ?) ORDER BY id DESC LIMIT 1`,
		clusterNo, TaskStatusSucceeded, TaskStatusFailed, TaskStatusCancelled, TaskStatusStalled)
	if err != nil {
		return nil, err
	}
	checkStep := checkItem{id: "step_mode_determined", title: "stepMode 已定(INV-50 / REF-17)"}
	if running != nil {
		mode := taskStepMode(running)
		if err := checkStepMode(mode); err != nil {
			checkStep.note = AsBizErr(err).Msg
		} else {
			checkStep.pass = true
			checkStep.note = fmt.Sprintf("在途任务 %s 已定步进模式 %s(创建瞬间落定,运行中不可互转)", Str(running["task_no"]), mode)
		}
	} else {
		checkStep.pass = true
		checkStep.note = "无在途任务;下一次派发按创建入口缺省落定(REF-17:向导 ⇒ STEP,编排屏 / 审批回调 ⇒ AUTO),不存在「未定」态"
	}

	// ⑤ 无与派生冲突的覆写(R-04)—— 与 clc_binding.go deriveParamsTx 同一落点判据
	// (覆写与派生冲突 ⇒ sync_pending=1 且 derive_note 记录冲突)。
	conflictCount, err := qCount(`SELECT COUNT(*) FROM clc_param
		WHERE cluster_ref=? AND flag=1 AND sync_pending=1 AND param_source=?`,
		clusterNo, ParamSourceOverride)
	if err != nil {
		return nil, err
	}
	checkOverride := checkItem{
		id:    "no_override_conflicts_derived",
		title: "无与派生冲突的覆写(R-04;覆写行不被派生覆盖,但 sync_pending=1 即冲突提示待人工确认)",
		pass:  conflictCount == 0,
	}
	if conflictCount > 0 {
		checkOverride.note = fmt.Sprintf("集群 %s 有 %d 行覆写与上游派生值冲突、待人工同步(FUNC-15);派生值已登注记但未覆盖覆写(R-04 / INV-23),请经参数同步入口人工确认", clusterNo, conflictCount)
	}

	items := []checkItem{checkNodes, checkRefs, checkExec, checkStep, checkOverride}
	allPass := true
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if !it.pass {
			allPass = false
		}
		out = append(out, it.toMap())
	}
	return map[string]any{
		"cluster_no":   clusterNo,
		"cluster_name": Str(cluster["name"]),
		"all_pass":     allPass,
		"checks":       out,
		"caliber_note": "即时提示、不拦截提交(拦截由各 INV / R 在写入瞬间执行);不写本域对象、不产生事件;queryReportRef: null —— M7 唯一显式例外,不建报表、无列表形态",
	}, nil
}

// checkItem FUNC-50 判定项的内部结构(避免把中文字段名散在返回 map 里;渲染由 toMap 统一)。
type checkItem struct {
	id    string
	title string
	pass  bool
	note  string
}

func (c checkItem) toMap() map[string]any {
	return map[string]any{
		"id":    c.id,
		"title": c.title,
		"pass":  c.pass,
		"note":  c.note,
	}
}
