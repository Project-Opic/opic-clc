// clc_common.go —— 集群域业务公共助手(编号生成/状态常量/事件发布/平台参数)。
//
// 编号规则:OPIC 统一口径「{类型码}-{8 位日期段}-{4 位流水段}」,
// 类型码 CLU(集群)/NOD(节点)/BND(绑定)/SNP(快照)/TSK(任务)/PRB(探活),三位前缀 + 四位流水号。
// 生成须与业务写入同事务,并用事务级咨询锁防并发重号(指导书 §3.1)。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/infra"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// 业务编号类型码(三位大写前缀 + 四位流水号)。
const (
	NoPrefixCluster  = "CLU"
	NoPrefixNode     = "NOD"
	NoPrefixBinding  = "BND"
	NoPrefixSnapshot = "SNP"
	NoPrefixTask     = "TSK"
	NoPrefixProbe    = "PRB"
	NoPrefixTemplate = "TPL"
)

// genBizNo 生成「{类型码}-{YYYYMMDD}-{4 位流水}」业务编号(与 INSERT 同事务调用)。
func genBizNo(db *gorm.DB, typeCode, table, column string) (string, error) {
	dateSeg := time.Now().Format("20060102")
	prefix := typeCode + "-" + dateSeg + "-"
	if _, err := store.Exec(db, `SELECT pg_advisory_xact_lock(hashtext(?))`, "clc_biz_no_"+prefix); err != nil {
		return "", err
	}
	row, err := store.One(db, fmt.Sprintf("SELECT MAX(%s) AS mx FROM %s WHERE %s LIKE ?", column, table, column), prefix+"%")
	if err != nil {
		return "", err
	}
	cur := 0
	if mx := Str(row["mx"]); mx != "" {
		seg := strings.TrimPrefix(mx, prefix)
		_, _ = fmt.Sscanf(seg, "%d", &cur)
	}
	return fmt.Sprintf("%s%04d", prefix, cur+1), nil
}

// ---------- 状态/字典常量(m1 data_dictionaries) ----------

const (
	// CLC_CLUSTER_STATUS(INV-02:单向迁移,DESTROYED 终态)
	ClusterStatusDraft         = "DRAFT"
	ClusterStatusInstalling    = "INSTALLING"
	ClusterStatusRunning       = "RUNNING"
	ClusterStatusInstallFailed = "INSTALL_FAILED"
	ClusterStatusDestroying    = "DESTROYING"
	ClusterStatusDestroyed     = "DESTROYED"

	// CLC_HEALTH(INV-06:DEGRADED 只能被显式恢复清除;探活只写其余三态)
	HealthUnknown     = "UNKNOWN"
	HealthHealthy     = "HEALTHY"
	HealthDegraded    = "DEGRADED"
	HealthUnreachable = "UNREACHABLE"

	// CLC_DEGRADED_REASON(R-05/R-09 降级归因)
	DegradedExecutorLost        = "EXECUTOR_LOST"
	DegradedIgnitionUnavailable = "IGNITION_UNAVAILABLE"
	DegradedRegistryRemoved     = "REGISTRY_REMOVED"
	DegradedAssetDecommissioned = "ASSET_DECOMMISSIONED"

	// CLC_NODE_STATUS(INV-13:REMOVED 终局,重新加入 = 登记新行)
	NodeStatusPending  = "PENDING"
	NodeStatusAdded    = "ADDED"
	NodeStatusRemoving = "REMOVING"
	NodeStatusRemoved  = "REMOVED"

	// CLC_NODE_HEALTH
	NodeHealthUnknown     = "UNKNOWN"
	NodeHealthHealthy     = "HEALTHY"
	NodeHealthUnreachable = "UNREACHABLE"

	// CLC_TASK_STATUS(INV-28:STALLED 与 FAILED 分列)
	TaskStatusQueued    = "QUEUED"
	TaskStatusRunning   = "RUNNING"
	TaskStatusSucceeded = "SUCCEEDED"
	TaskStatusFailed    = "FAILED"
	TaskStatusCancelled = "CANCELLED"
	TaskStatusStalled   = "STALLED"

	// CLC_TASK_FAIL_REASON
	TaskFailStepFailed          = "STEP_FAILED"
	TaskFailExecutorLost        = "EXECUTOR_LOST"
	TaskFailIgnitionUnavailable = "IGNITION_UNAVAILABLE"
	TaskFailActionDenied        = "ACTION_DENIED"
	TaskFailExecutorError       = "EXECUTOR_ERROR"

	// CLC_PROBE_RESULT
	ProbeResultHealthy = "HEALTHY"
	ProbeResultFailed  = "FAILED"

	// CLC_RUN_MODE(INV-04:LEGACY ⇔ 四类中心引用全空)
	RunModeMCC    = "MCC"
	RunModeLegacy = "LEGACY"

	// CLC_NODE_ROLE
	NodeRoleMaster = "MASTER"
	NodeRoleWorker = "WORKER"
	NodeRoleInfra  = "INFRA"

	// CLC_TARGET_CAPABILITY(INV-17 组合映射)
	CapabilityIgnition = "IGNITION"
	CapabilityRegistry = "REGISTRY"
	CapabilityCert     = "CERT"
	CapabilityAsset    = "ASSET"

	// CLC_TARGET_KIND
	TargetKindExecutor    = "EXECUTOR"
	TargetKindVersion     = "VERSION"
	TargetKindInstance    = "INSTANCE"
	TargetKindProject     = "PROJECT"
	TargetKindCertificate = "CERTIFICATE"
	TargetKindAsset       = "ASSET"

	// CLC_BINDING_SOURCE
	BindingSourceManual     = "MANUAL"
	BindingSourceAutoDerive = "AUTO_DERIVED"
	BindingSourceEventSync  = "EVENT_SYNC"

	// CLC_PARAM_SOURCE
	ParamSourceDerived  = "DERIVED"
	ParamSourceTemplate = "TEMPLATE"
	ParamSourceOverride = "MANUAL_OVERRIDE"

	// CLC_PARAM_DERIVED_FROM
	ParamFromRegistry = "REGISTRY"
	ParamFromVersion  = "VERSION"
	ParamFromCert     = "CERT"

	// CLC_SNAPSHOT_TYPE(INV-25:快照只增不改)
	SnapshotTypeChange      = "CHANGE"
	SnapshotTypeAutoDerived = "AUTO_DERIVED"
	SnapshotTypeRollback    = "ROLLBACK"

	// CLC_TASK_KIND
	TaskKindInstall      = "INSTALL"
	TaskKindRetryInstall = "RETRY_INSTALL"
	TaskKindUpgrade      = "UPGRADE"
	TaskKindAddNode      = "ADD_NODE"
	TaskKindDelNode      = "DEL_NODE"
	TaskKindBackup       = "BACKUP"
	TaskKindRestore      = "RESTORE"
	TaskKindStart        = "START"
	TaskKindStop         = "STOP"
	TaskKindCertRenew    = "CERT_RENEW"
	TaskKindDestroy      = "DESTROY"

	// CLC_EXEC_PATH(INV-32:登记值须与真实执行位置一致,绝不静默切换)
	ExecPathLocal          = "LOCAL"
	ExecPathRemoteExecutor = "REMOTE_EXECUTOR"

	// CLC_TRIGGER_SOURCE
	ProbeTriggerScheduled = "SCHEDULED"
	ProbeTriggerManual    = "MANUAL"

	// CLC_STEP_MODE(INV-50 / REF-17:任务创建瞬间落定,运行中不可互转)
	StepModeStep = "STEP"
	StepModeAuto = "AUTO"
)

// maxOperationReason ClusterTask.operationReason 列宽(m1 属性 VARCHAR(500);事由累加按字符收口)。
const maxOperationReason = 500

// capabilityKinds INV-17:目标能力 → 合法资源类型映射。
var capabilityKinds = map[string][]string{
	CapabilityIgnition: {TargetKindExecutor, TargetKindVersion},
	CapabilityRegistry: {TargetKindInstance, TargetKindProject},
	CapabilityCert:     {TargetKindCertificate},
	CapabilityAsset:    {TargetKindAsset},
}

// clusterTaskKinds 允许登记的任务类别(非交付任务不得入台账,防脏数据)。
var clusterTaskKinds = map[string]bool{
	TaskKindInstall: true, TaskKindRetryInstall: true, TaskKindUpgrade: true,
	TaskKindAddNode: true, TaskKindDelNode: true, TaskKindBackup: true,
	TaskKindRestore: true, TaskKindStart: true, TaskKindStop: true,
	TaskKindCertRenew: true, TaskKindDestroy: true,
}

// nodeRoleAllowed INV-11:节点角色字典。
var nodeRoleAllowed = map[string]bool{NodeRoleMaster: true, NodeRoleWorker: true, NodeRoleInfra: true}

// bizRuleFail 规则校验失败(统一 ErrRule + 中文提示,可回溯规则 ID)。
func bizRuleFail(ruleID, format string, args ...any) *BizErr {
	return Fail(errcode.ErrRule, "规则 %s 校验未通过:%s", append([]any{ruleID}, fmt.Sprintf(format, args...))...)
}

// paramInt 平台参数字符串 → int64(Int() 只认数值型;TEXT 列与表单值均为字符串,须显式解析)。
// 踩坑记录:Int("50") 回退 0,导致阈值/保留期等上限误判为 0(AST/ADD 均踩过)。
func paramInt(v any) int64 {
	if s := Str(v); s != "" {
		var n int64
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err == nil {
			return n
		}
	}
	if n := Int(v); n != 0 {
		return n
	}
	// JSON 数字经 float64 到达时 Int 已覆盖;字符串小数形态兜底
	if s := strings.TrimSpace(Str(v)); s != "" {
		var f float64
		if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
			return int64(f)
		}
	}
	return 0
}

// nowText 当前时间(TEXT 列口径,与底座 to_char(now(),'YYYY-MM-DD HH24:MI:SS') 一致)。
func nowText() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// clampRunes 按字符数截断(rune 安全;库侧 VARCHAR(500) 是字节还是字符由列定义决定,
// 中文事由在服务层就按字符收口,避免落库即报错)。
func clampRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// appendReasonText 事由累加(FUNC-45 多次跳步 / FUNC-46 高危事由都写任务头 operationReason,
// 后一次不覆盖前一次:R-24 要求代价留在数据上,覆盖即丢历史)。
func appendReasonText(old, add string, max int) string {
	old = strings.TrimSpace(old)
	add = strings.TrimSpace(add)
	switch {
	case old == "":
		return clampRunes(add, max)
	case add == "":
		return old
	case strings.Contains(old, add):
		return old // 同一事由重复登记不累加(重试同一请求不产生第二份)
	default:
		return clampRunes(old+"；"+add, max)
	}
}

// boolSmallInt bool → smallint(0/1)。
func boolSmallInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// truthy 兼容 0/1 smallint、bool 与字符串布尔。
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || strings.EqualFold(x, "true") || x == "是"
	}
	return Int(v) == 1
}

// jsonMarshal JSON 序列化为文本(存储用;失败回退 "[]")。
func jsonMarshal(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// ontologyDictItems 本体字典项(经注册表)。
func ontologyDictItems(dictID, typeCode string) []map[string]string {
	return ontology.Load().GetDictionaryItems(dictID, typeCode)
}

// dictCodes 从本体注册表取字典 code 集(取不到时回退 fallback)。
func dictCodes(dictID, typeCode string, fallback ...string) map[string]bool {
	out := map[string]bool{}
	for _, it := range ontologyDictItems(dictID, typeCode) {
		out[it["code"]] = true
	}
	if len(out) == 0 {
		for _, f := range fallback {
			out[f] = true
		}
	}
	return out
}

// positiveInt 平台参数正值校验器(REF-xx 阈值/保留期口径)。
func positiveInt(ruleID, label string) func(string) error {
	return func(v string) error {
		if paramInt(v) < 1 {
			return bizRuleFail(ruleID, "%s须为不小于 1 的整数,收到「%s」", label, v)
		}
		return nil
	}
}

// publishClcEvent 业务事件旁路发布(主题按中心域码隔离 opic.clc.business.*)。// 旁路语义:发布失败仅记日志,不阻塞、不回滚主事务(m6「本域不等任何回执」)。
func publishClcEvent(event string, payload map[string]any) {
	eb := infra.EB()
	if eb == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["event"] = event
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	subject := fmt.Sprintf("opic.%s.business.%s", config.Config.CenterCode, event)
	if err := eb.Publish(context.Background(), subject, b); err != nil {
		log.Printf("[clc-events] 发布失败 %s: %v", subject, err)
	}
}
