// 集群域(clc)列表页公共助手:状态胶囊映射(风格对齐 admin/components/StatusPill)。
import type { ClcCluster, ClcTask } from '../../api/clc'
import StatusPill, { type StatusTone } from '../../admin/components/StatusPill'

export function pill(map: Record<string, { tone: StatusTone; label: string }>, value: string | null | undefined) {
  const v = value || '-'
  const meta = map[v]
  return <StatusPill tone={meta?.tone || 'muted'} label={meta?.label || v} />
}

/** 集群生命周期状态(AGG-CLC-01;字典 CLC_CLUSTER_STATUS) */
export const CLUSTER_STATUS: Record<string, { tone: StatusTone; label: string }> = {
  DRAFT: { tone: 'muted', label: '草稿' },
  INSTALLING: { tone: 'info', label: '安装中' },
  RUNNING: { tone: 'success', label: '运行中' },
  INSTALL_FAILED: { tone: 'danger', label: '安装失败' },
  DESTROYING: { tone: 'warning', label: '销毁中' },
  DESTROYED: { tone: 'muted', label: '已销毁' },
}

/** 集群健康结论(字典 CLC_HEALTH) */
export const CLUSTER_HEALTH: Record<string, { tone: StatusTone; label: string }> = {
  UNKNOWN: { tone: 'muted', label: '未知' },
  HEALTHY: { tone: 'success', label: '健康' },
  DEGRADED: { tone: 'warning', label: '已降级' },
  UNREACHABLE: { tone: 'danger', label: '不可达' },
}

/** 降级原因(字典 CLC_DEGRADED_REASON) */
export const DEGRADED_REASON: Record<string, string> = {
  EXECUTOR_LOST: '执行机失联',
  IGNITION_UNAVAILABLE: '交付域不可用',
  REGISTRY_REMOVED: '镜像源移除',
  ASSET_DECOMMISSIONED: '节点资产下架',
}

/** 运行模式(字典 CLC_RUN_MODE) */
export const RUN_MODE: Record<string, { tone: StatusTone; label: string }> = {
  MCC: { tone: 'info', label: '中心引用模式' },
  LEGACY: { tone: 'muted', label: '兼容模式' },
}

/** 节点状态(AGG-CLC-02;字典 CLC_NODE_STATUS) */
export const NODE_STATUS: Record<string, { tone: StatusTone; label: string }> = {
  PENDING: { tone: 'muted', label: '待加入' },
  ADDED: { tone: 'success', label: '已加入' },
  REMOVING: { tone: 'info', label: '移除中' },
  REMOVED: { tone: 'muted', label: '已移除' },
}

/** 节点健康(字典 CLC_NODE_HEALTH) */
export const NODE_HEALTH: Record<string, { tone: StatusTone; label: string }> = {
  UNKNOWN: { tone: 'muted', label: '未知' },
  HEALTHY: { tone: 'success', label: '健康' },
  UNREACHABLE: { tone: 'danger', label: '不可达' },
}

/** 节点角色(字典 CLC_NODE_ROLE) */
export const NODE_ROLE: Record<string, string> = {
  MASTER: '控制面',
  WORKER: '工作负载',
  INFRA: '基础设施',
}

/** 任务类别(AGG-CLC-06;字典 CLC_TASK_KIND) */
export const TASK_KIND: Record<string, string> = {
  INSTALL: '安装',
  RETRY_INSTALL: '续跑安装',
  UPGRADE: '升级',
  ADD_NODE: '加节点',
  DEL_NODE: '减节点',
  BACKUP: '备份',
  RESTORE: '恢复',
  START: '启动',
  STOP: '停止',
  CERT_RENEW: '证书推送',
  DESTROY: '销毁',
}

// DEF-06 行为级分档(唯一一份 kind→权限码表,frmClcOrch 发起弹窗与 frmClcWizard 生命周期台共用,
// 不出第二份):与后端 internal/service/clc_task.go 从本体注册表两级派生的 kind→权限码同一口径,
// 不新造权限码。START/STOP 同属 FUNC-12「集群启停」一档;CERT_RENEW 只经生命周期台(FUNC-46)派发,
// 编排口(POST /cluster/:id/task)不受理它。
export const TASK_KIND_PERMS: Record<string, string> = {
  INSTALL: 'PERM-FUNC-03',
  RETRY_INSTALL: 'PERM-FUNC-04',
  UPGRADE: 'PERM-FUNC-09',
  BACKUP: 'PERM-FUNC-10',
  RESTORE: 'PERM-FUNC-11',
  START: 'PERM-FUNC-12',
  STOP: 'PERM-FUNC-12',
  CERT_RENEW: 'PERM-FUNC-46',
}

/** 编排口(/clc/cluster/:id/task)受理集;DESTROY 只经审批流(FUNC-05),任何派发口都不直派 */
export const ORCH_DISPATCH_KINDS = ['INSTALL', 'RETRY_INSTALL', 'UPGRADE', 'BACKUP', 'RESTORE', 'START', 'STOP']

/** FUNC-46 生命周期台派发集(internal/service/clc_wizard.go lifecycleActionKinds 同档;不新增类别值) */
export const LIFECYCLE_DISPATCH_KINDS = ['START', 'STOP', 'UPGRADE', 'BACKUP', 'RESTORE', 'CERT_RENEW']

/** 生命周期台高危动作(R-24 二次确认 + 事由必填;后端 highRiskActionKinds 同档;销毁另走审批不入此列) */
export const HIGH_RISK_LIFECYCLE_KINDS = ['STOP', 'RESTORE']

/** 任务状态(字典 CLC_TASK_STATUS) */
export const TASK_STATUS: Record<string, { tone: StatusTone; label: string }> = {
  QUEUED: { tone: 'muted', label: '排队中' },
  RUNNING: { tone: 'info', label: '执行中' },
  SUCCEEDED: { tone: 'success', label: '成功' },
  FAILED: { tone: 'danger', label: '失败' },
  CANCELLED: { tone: 'muted', label: '已取消' },
  STALLED: { tone: 'warning', label: '失联挂起' },
}

/** 在途任务状态(QUEUED / RUNNING):台级徽章、步进解禁与轮询启停共用这一份判据,不在屏间各写一遍 */
export const LIVE_TASK_STATUSES = ['QUEUED', 'RUNNING']

/** 任务失败原因(字典 CLC_TASK_FAIL_REASON) */
export const TASK_FAIL_REASON: Record<string, string> = {
  STEP_FAILED: '步骤失败',
  EXECUTOR_LOST: '执行机失联',
  IGNITION_UNAVAILABLE: '交付域不可用',
  ACTION_DENIED: '动作被拒绝',
  EXECUTOR_ERROR: '执行机执行错误',
}

/** 执行位置(字典 CLC_EXEC_PATH) */
export const EXEC_PATH: Record<string, string> = {
  LOCAL: '本地链路',
  REMOTE_EXECUTOR: '远程执行机',
}

/** 交付步骤状态(字典 CLC_STEP_STATUS;CANCELLED=未执行即取消 与 SKIPPED=本次跳过(上次已成功) 语义分列)。
 *  中文名取自 /api/meta/dictionaries,本地只留 tone(字典没有配色维度)。 */
export const STEP_STATUS_TONE: Record<string, StatusTone> = {
  PENDING: 'muted',
  RUNNING: 'info',
  SUCCEEDED: 'success',
  FAILED: 'danger',
  CANCELLED: 'muted',
  SKIPPED: 'warning',
}

/** 交付日志级别(字典 CLC_LOG_LEVEL;REP-13 面板着色)——只留 tone,级别名取自接口。
 *  同档还有 CLC_STEP_VERDICT(判定依据):它只有中文名一个维度,故本地不再建表,直接按码值取词条。 */
export const LOG_LEVEL_TONE: Record<string, 'info' | 'warn' | 'error'> = {
  INFO: 'info',
  WARN: 'warn',
  ERROR: 'error',
}

/** 用本地 tone 档 + 接口下发的词条拼出胶囊映射(DEF-09:界面不留字典副本)。 */
export function withDictLabels(map: Record<string, StatusTone>, items: { code: string; label: string }[]) {
  const out: Record<string, { tone: StatusTone; label: string }> = {}
  for (const i of items) out[i.code] = { tone: map[i.code] || 'muted', label: i.label }
  return out
}

/** 快照类型(AGG-CLC-05;字典 CLC_SNAPSHOT_TYPE) */
export const SNAPSHOT_TYPE: Record<string, { tone: StatusTone; label: string }> = {
  CHANGE: { tone: 'info', label: '变更快照' },
  AUTO_DERIVED: { tone: 'muted', label: '派生快照' },
  ROLLBACK: { tone: 'warning', label: '回滚快照' },
}

/** 目标能力(AGG-CLC-03;字典 CLC_TARGET_CAPABILITY) */
export const TARGET_CAPABILITY: Record<string, string> = {
  IGNITION: '交付执行',
  REGISTRY: '制品仓库',
  CERT: '证书信任',
  ASSET: '资产',
}

/** 资源类型(字典 CLC_TARGET_KIND) */
export const TARGET_KIND: Record<string, string> = {
  EXECUTOR: '执行机',
  VERSION: '交付版本',
  INSTANCE: '仓库实例',
  PROJECT: '仓库项目',
  CERTIFICATE: '证书',
  ASSET: '资产',
}

/** 用途(字典 CLC_BINDING_PURPOSE) */
export const BINDING_PURPOSE: Record<string, string> = {
  EXECUTOR_RUNTIME: '交付运行时',
  VERSION_BASE: '版本基座',
  IMAGE_MIRROR: '镜像源',
  HARBOR_CLIENT: '镜像源客户端凭据',
  MONITORING_MTLS: '监控 mTLS',
  INGRESS_WILDCARD: '入口泛域名',
  NODE_ASSET: '节点资产',
}

/** 登记来源(字典 CLC_BINDING_SOURCE) */
export const BINDING_SOURCE: Record<string, { tone: StatusTone; label: string }> = {
  MANUAL: { tone: 'info', label: '人工' },
  AUTO_DERIVED: { tone: 'muted', label: '派生' },
  EVENT_SYNC: { tone: 'warning', label: '事件对账' },
}

/** 参数来源(AGG-CLC-04;字典 CLC_PARAM_SOURCE) */
export const PARAM_SOURCE: Record<string, { tone: StatusTone; label: string }> = {
  DERIVED: { tone: 'info', label: '派生' },
  TEMPLATE: { tone: 'muted', label: '模板' },
  MANUAL_OVERRIDE: { tone: 'warning', label: '人工覆写' },
}

/** 探活结论(AGG-CLC-07;字典 CLC_PROBE_RESULT) */
export const PROBE_RESULT: Record<string, { tone: StatusTone; label: string }> = {
  HEALTHY: { tone: 'success', label: '通过' },
  FAILED: { tone: 'danger', label: '失败' },
}

/**
 * 集群台(frmClcClusterConsole,v1.3 载体置换)注入给页签内既有屏的上下文。
 * 集群由台级唯一确定 ⇒ 屏内不再渲染集群选择器 / 跨集群筛选;任务行由台级持有一份
 * (REP-07 的本集群 scope),「安装」「日志」两页签与徽章条共用同一份行,不在页签内各拉一次。
 * refresh = 9.10.7 ① 的落点:页签内任何状态变更后重取徽章条(REP-02)。
 */
export interface ConsoleCtx {
  cluster: ClcCluster
  tasks: ClcTask[]
  refresh: () => void
}
