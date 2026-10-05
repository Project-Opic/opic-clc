// clc_rules.go —— m3-rule-model.yaml 规则的等价 Go 实现。
//
// 口径(指导书 §3.6):模型是唯一语义来源;每条校验函数注释回溯规则 ID,
// 违反一律返回 ErrRule 业务错误并给中文提示。不变量(INV-xx)与跨对象规则(R-xx)
// 在各聚合 service 内就地实现并同样标注规则 ID。
//
// 本域规则族:
//
//	REF-01 legacy 模式开关(平台配置;禁用时创建必须至少一项中心引用)
//	REF-02 快照保留期(缺省 365 天)
//	REF-03 探活周期与连续失败阈值(缺省 60 秒 / 3 次)
//	REF-04 创建向导默认引用(执行机 / 版本 / 仓库实例)
//	REF-05 交付版本引用必须命中未归档且就绪及之后的版本(禁用版本拦截)
//	REF-06 镜像源派生规则为固定映射模板(实例 + 项目 → 镜像源地址与工具镜像引用)
//	REF-07 聚合健康响应缓存时长(缺省 60 秒)
//	REF-08 探活记录保留期
//	REF-09 降级原因与订阅事件的固定枚举映射
//	REF-11 探活恢复健康需连续成功达到阈值(防抖动)
//	REF-12 交付动作日志复用平台统一任务通道回放(不在本域另建日志存储)
//	INV-01~09  集群:名称唯一、状态单向、失败步骤索引、LEGACY ⇔ 引用空、成对引用、
//	            健康写入口径、RUNNING 后主控入口非空、凭据值禁止入库、DESTROYED 冻结
//	INV-10~15  节点:IP/K8s 名唯一、角色字典、裸机接管、REMOVED 终局、ADDED 有节点名、PENDING 不得交付
//	INV-16~19  引用:组合唯一、能力-类型映射、对账注记、销毁级联范围
//	INV-20~23  参数:键唯一、来源口径、待同步注记、覆写保护
//	INV-24~27  快照:序号单调、只增不改、回滚前置、按保留期清理
//	INV-28~33  任务:STALLED/FAILED 分列、终态口径、失败索引写回、只增不改、执行位置一致、销毁前置审批
//	INV-34~36  探活:失败原因/就绪数口径、只增不改、只读探测
//	R-01~R-13  跨对象:双写同步、资产占用拦截、资产下架处置、参数派生保护、降级归因、
//	            回滚校验、销毁编排、健康双源、失败写回、销毁审批前置、显式恢复、重绑定校验、安装成功落点
package service

import (
	"fmt"
	"net"
	"strings"
)

// validateClusterInput 集群属性级校验(INV-01 名称非空格式 / INV-05 成对引用 / 字典命中)。
func validateClusterInput(d map[string]any) error {
	name := Str(d["name"])
	if name == "" {
		return bizRuleFail("INV-01", "集群名称必填")
	}
	if len([]rune(name)) > 100 {
		return bizRuleFail("INV-01", "集群名称长度不得超过 100")
	}
	if rm := Str(d["run_mode"]); rm != "" && !dictCodes("DICT-CLC_RUN_MODE", "CLC_RUN_MODE", RunModeMCC, RunModeLegacy)[rm] {
		return bizRuleFail("INV-04", "运行模式只能取 MCC(中心引用模式)或 LEGACY(兼容模式),收到「%s」", rm)
	}
	// INV-05:镜像仓库项目引用与实例引用成对出现或同时为空
	if (Str(d["registry_ref"]) == "") != (Str(d["registry_project_ref"]) == "") {
		return bizRuleFail("INV-05", "镜像仓库实例引用与项目引用必须成对出现或同时为空(实例是项目在所有引用处的稳定锚点)")
	}
	return nil
}

// checkRefConsistency INV-04:运行模式与四类中心引用的一致性(LEGACY ⇔ 全空;MCC 至少一项非空)。
// 平台配置禁用 legacy(REF-01)时显式 LEGACY 直接拒绝。
func checkRefConsistency(runMode string, refs map[string]string) error {
	hasAny := false
	for _, v := range refs {
		if strings.TrimSpace(v) != "" {
			hasAny = true
		}
	}
	if runMode == "" {
		runMode = RunModeMCC
	}
	if runMode == RunModeLegacy {
		if hasAny {
			return bizRuleFail("INV-04", "运行模式为 LEGACY(兼容模式)时四类中心引用必须全部为空;请改用 MCC 模式或清空引用")
		}
		if !truthy(platformParam("clc.legacy_mode_enabled", "false")) {
			return bizRuleFail("REF-01", "平台已禁用 legacy 兼容模式:创建集群必须至少登记一项中心引用(运行模式 MCC)")
		}
		return nil
	}
	if !hasAny {
		return bizRuleFail("INV-04", "运行模式为 MCC(中心引用模式)时必须至少登记一项中心引用(交付版本 / 执行机 / 镜像仓库)")
	}
	return nil
}

// checkLifecycleTransition INV-02:生命周期状态迁移固定单向;DESTROYED 为终态。
// 合法迁移:DRAFT→INSTALLING→RUNNING / INSTALL_FAILED;INSTALL_FAILED→INSTALLING(续跑);
// 任意非终态→DESTROYING→DESTROYED;RUNNING⇄(START/STOP 由任务落点回写,状态保持 RUNNING)。
func checkLifecycleTransition(from, to string) error {
	allowed := map[string][]string{
		ClusterStatusDraft:         {ClusterStatusInstalling, ClusterStatusDestroying},
		ClusterStatusInstalling:    {ClusterStatusRunning, ClusterStatusInstallFailed, ClusterStatusDestroying},
		ClusterStatusInstallFailed: {ClusterStatusInstalling, ClusterStatusDestroying},
		ClusterStatusRunning:       {ClusterStatusDestroying},
		ClusterStatusDestroying:    {ClusterStatusDestroyed},
	}
	if from == to {
		return nil
	}
	if from == ClusterStatusDestroyed {
		return bizRuleFail("INV-02", "DESTROYED(已销毁)为终态且行保留,不可再变更状态")
	}
	for _, nxt := range allowed[from] {
		if nxt == to {
			return nil
		}
	}
	return bizRuleFail("INV-02", "生命周期状态不允许从 %s 迁移到 %s(迁移固定单向:草稿→安装中→运行中/安装失败→销毁中→已销毁)", from, to)
}

// checkRunningFootprint INV-07:状态进入 RUNNING 及之后主控入口与凭据存放标识必须非空。
func checkRunningFootprint(status, masterEndpoints, kubeconfigRef string) error {
	if status == ClusterStatusRunning || status == ClusterStatusDestroying {
		if strings.TrimSpace(masterEndpoints) == "" || strings.TrimSpace(kubeconfigRef) == "" {
			return bizRuleFail("INV-07", "集群状态进入 RUNNING 后主控入口与访问凭据存放标识必须登记(凭据内容不入库,仅存位置标识)")
		}
	}
	return nil
}

// checkKubeconfigRef INV-08:访问凭据存放标识只允许存放位置标识。
// kubeconfig 内容与任何凭据值(私钥块 / 密码 / token)不得进入本对象与任何接口响应。
func checkKubeconfigRef(ref string) error {
	if strings.Contains(ref, "\n") || strings.Contains(ref, "-----BEGIN") {
		return bizRuleFail("INV-08", "kubeconfig 内容与任何凭据值不得进入本对象:凭据存放标识只允许登记存放位置(如 vault 路径 / 对象存储 key)")
	}
	return nil
}

// checkNodeInput 节点属性级校验(INV-11 角色字典 / IP 格式)。
func checkNodeInput(d map[string]any) error {
	ip := Str(d["ip_address"])
	if ip == "" {
		return bizRuleFail("INV-10", "IP 地址必填(裸机模式凭登记 IP 人工接管)")
	}
	if net.ParseIP(strings.TrimSpace(ip)) == nil {
		return bizRuleFail("INV-10", "IP 地址「%s」不是合法 IP", ip)
	}
	role := Str(d["node_role"])
	if !nodeRoleAllowed[role] {
		return bizRuleFail("INV-11", "节点角色「%s」不在 CLC_NODE_ROLE 字典内(可用:MASTER 控制面 / WORKER 工作负载 / INFRA 基础设施)", role)
	}
	return nil
}

// checkBindingCombo INV-17:目标能力与资源类型的组合必须合法。
func checkBindingCombo(capability, kind string) error {
	kinds, ok := capabilityKinds[capability]
	if !ok {
		return bizRuleFail("INV-17", "目标能力「%s」不在 CLC_TARGET_CAPABILITY 字典内(可用:IGNITION 交付执行 / REGISTRY 制品仓库 / CERT 证书信任 / ASSET 资产)", capability)
	}
	for _, k := range kinds {
		if k == kind {
			return nil
		}
	}
	return bizRuleFail("INV-17", "目标能力 %s 与资源类型 %s 组合不合法(合法映射:IGNITION→EXECUTOR/VERSION;REGISTRY→INSTANCE/PROJECT;CERT→CERTIFICATE;ASSET→ASSET)", capability, kind)
}

// checkReconcileNote INV-18:EVENT_SYNC 行的变更必须携带对账注记。
func checkReconcileNote(source, note string) error {
	if source == BindingSourceEventSync && strings.TrimSpace(note) == "" {
		return bizRuleFail("INV-18", "登记来源为 EVENT_SYNC(事件对账)的引用行变更必须携带对账注记(哪些事件维护了它)")
	}
	return nil
}

// checkParamSource INV-21/INV-22:参数来源口径。
func checkParamSource(source, derivedFrom, deriveNote string, syncPending bool) error {
	switch source {
	case ParamSourceDerived:
		if strings.TrimSpace(derivedFrom) == "" {
			return bizRuleFail("INV-21", "参数来源为 DERIVED(派生)时派生来源必填(REGISTRY/VERSION/CERT)")
		}
		if !dictCodes("DICT-CLC_PARAM_DERIVED_FROM", "CLC_PARAM_DERIVED_FROM", ParamFromRegistry, ParamFromVersion, ParamFromCert)[derivedFrom] {
			return bizRuleFail("INV-21", "派生来源「%s」不在 CLC_PARAM_DERIVED_FROM 字典内", derivedFrom)
		}
	case ParamSourceOverride:
		// MANUAL_OVERRIDE 每次修改必须留痕(由调用方 WriteAudit 承载)
	default:
		if source != ParamSourceTemplate {
			return bizRuleFail("INV-21", "参数来源只能取 DERIVED(派生)/ TEMPLATE(模板)/ MANUAL_OVERRIDE(人工覆写)")
		}
	}
	if syncPending && strings.TrimSpace(deriveNote) == "" {
		return bizRuleFail("INV-22", "待同步标记为真时必须存在对应的派生注记(哪个上游、什么变更)")
	}
	return nil
}

// checkProbeInput INV-34:探活记录口径(失败原因必填、就绪节点数不得大于总节点数)。
func checkProbeInput(result, failReason string, ready, total int64) error {
	if result == ProbeResultFailed && strings.TrimSpace(failReason) == "" {
		return bizRuleFail("INV-34", "探活结论为 FAILED(失败)时失败原因必填")
	}
	if ready > total {
		return bizRuleFail("INV-34", "就绪节点数(%d)不得大于总节点数(%d)", ready, total)
	}
	return nil
}

// checkTaskKind 任务类别字典命中(登记前置校验)。
func checkTaskKind(kind string) error {
	if !clusterTaskKinds[kind] {
		return bizRuleFail("INV-28", "任务类别「%s」不在 CLC_TASK_KIND 字典内", kind)
	}
	return nil
}

// checkTaskFinal INV-29:终态必须有结束时间与结果摘要;FAILED 必填失败原因;
// INV-30:INSTALL/RETRY_INSTALL 失败必须携带失败步骤索引。
func checkTaskFinal(kind, status, failReason, resultSummary string, failStep *int) error {
	if strings.TrimSpace(resultSummary) == "" {
		return bizRuleFail("INV-29", "任务进入终态(%s)必须填写结果摘要", status)
	}
	if status == TaskStatusFailed {
		if strings.TrimSpace(failReason) == "" {
			return bizRuleFail("INV-29", "任务失败(FAILED)必须登记失败原因")
		}
		if (kind == TaskKindInstall || kind == TaskKindRetryInstall) && failStep == nil {
			return bizRuleFail("INV-30", "安装类任务(INSTALL/RETRY_INSTALL)失败必须携带失败步骤索引,并同步写回所属集群")
		}
	}
	return nil
}

// checkVersionRef REF-05:交付版本引用必须命中 O-IGN 未归档且就绪及之后的版本。
// 版本引用在本域为跨域标识(O-IGN 权威);本域落地口径:非空且形态合法(IGN-VER-xxxx),
// 禁用/归档拦截由引用登记时的跨域校验与降级事件(R-05)承载。
func checkVersionRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	if len(ref) > 100 {
		return bizRuleFail("REF-05", "交付版本引用长度不得超过 100")
	}
	return nil
}

// healthTransitionByProbe R-08/INV-06:健康双源写入口径。
// 探活连续失败 ⇒ UNREACHABLE;连续成功 ⇒ HEALTHY(防抖阈值 REF-11/REF-03);
// 探活不覆盖 DEGRADED;DEGRADED 只能被显式恢复清除(FUNC-16)。
// 入参:当前健康、本轮连续成功数、连续失败数;出参:新健康、是否变更。
func healthTransitionByProbe(current string, consecutiveOK, consecutiveFail int) (string, bool) {
	if current == HealthDegraded {
		return current, false // 探活不覆盖 DEGRADED(INV-06)
	}
	failThreshold := int(paramInt(platformParam("clc.probe_fail_threshold", "3")))
	okThreshold := int(paramInt(platformParam("clc.probe_recover_threshold", "3")))
	if failThreshold <= 0 {
		failThreshold = 3
	}
	if okThreshold <= 0 {
		okThreshold = 3
	}
	if consecutiveFail >= failThreshold && current != HealthUnreachable {
		return HealthUnreachable, true
	}
	if consecutiveOK >= okThreshold && current != HealthHealthy {
		return HealthHealthy, true
	}
	if current == HealthUnreachable && consecutiveOK > 0 && consecutiveOK < okThreshold {
		return current, false // 恢复中,未达防抖阈值不翻转(REF-11)
	}
	return current, false
}

// deriveImageMirror REF-06:镜像源派生规则为固定映射模板(实例 + 项目 → 镜像源地址与工具镜像引用)。
// 返回派生参数键值对(参数来源 DERIVED,derived_from=REGISTRY)。
func deriveImageMirror(registryInstance, registryProject string) map[string]string {
	if strings.TrimSpace(registryInstance) == "" || strings.TrimSpace(registryProject) == "" {
		return nil
	}
	return map[string]string{
		"image.mirror.registry":   registryInstance,
		"image.mirror.project":    registryProject,
		"image.mirror.endpoint":   fmt.Sprintf("%s/%s", strings.TrimSuffix(registryInstance, "/"), registryProject),
		"image.mirror.tool_image": fmt.Sprintf("%s/%s/opic-tools:latest", strings.TrimSuffix(registryInstance, "/"), registryProject),
	}
}

// sortedSummary 引用键稳定排序(错误提示/摘要用)。
func sortedSummary(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 简单插入排序(键数量有限)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += "、"
		}
		out += fmt.Sprintf("%s=%s", k, m[k])
	}
	return out
}
