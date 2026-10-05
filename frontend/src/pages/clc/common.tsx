// 集群域(clc)列表页公共助手:状态胶囊映射(风格对齐 admin/components/StatusPill)。
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

/** 任务状态(字典 CLC_TASK_STATUS) */
export const TASK_STATUS: Record<string, { tone: StatusTone; label: string }> = {
  QUEUED: { tone: 'muted', label: '排队中' },
  RUNNING: { tone: 'info', label: '执行中' },
  SUCCEEDED: { tone: 'success', label: '成功' },
  FAILED: { tone: 'danger', label: '失败' },
  CANCELLED: { tone: 'muted', label: '已取消' },
  STALLED: { tone: 'warning', label: '失联挂起' },
}

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
