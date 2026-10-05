import { http, type PageResult } from './request'

/** 集群域(/api/clc/*)接口封装;契约见 internal/api/business/clc.go */

// ---------- 集群(AGG-CLC-01) ----------

export interface ClcCluster {
  id: number
  cluster_no: string
  name: string
  description: string | null
  cluster_dns_domain: string | null
  version_ref: string | null
  executor_ref: string | null
  registry_ref: string | null
  registry_project_ref: string | null
  run_mode: string
  status: string
  install_step_index: number | null
  health: string
  degraded_reason: string | null
  last_probe_at: string | null
  kubeconfig_ref: string | null
  master_endpoints: string | null
  destroy_reason: string | null
  node_count?: number
  binding_count?: number
  latest_task?: string | null
  bindings?: ClcBinding[]
  created_at: string
  destroyed_at: string | null
}

export const clusterApi = {
  list(params: { page?: number; size?: number; status?: string; run_mode?: string; health?: string; keyword?: string }) {
    return http.get<PageResult<ClcCluster>>('/api/clc/cluster', params)
  },
  get(id: number) {
    return http.get<ClcCluster>(`/api/clc/cluster/${id}`)
  },
  create(data: Record<string, unknown>) {
    return http.post<ClcCluster>('/api/clc/cluster', data)
  },
  update(id: number, data: Record<string, unknown>) {
    return http.put<ClcCluster>(`/api/clc/cluster/${id}`, data)
  },
  destroyImpact(id: number) {
    return http.get<Record<string, unknown>>(`/api/clc/cluster/${id}/destroy-impact`)
  },
  destroyDraft(id: number, data: Record<string, unknown>) {
    return http.put<ClcCluster>(`/api/clc/cluster/${id}/destroy-draft`, data)
  },
  destroySubmit(id: number, data: { reason: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/destroy-submit`, data)
  },
  recover(id: number, data: { mode: string; target_capability?: string; target_kind?: string; new_target_ref?: string; purpose?: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/recover`, data)
  },
  addNode(id: number, data: { ip_address: string; node_role: string; asset_ref?: string; remark?: string; k8s_nodename?: string }) {
    return http.post<ClcNode>(`/api/clc/cluster/${id}/node`, data)
  },
  adjustBinding(id: number, data: { target_capability: string; target_kind: string; target_ref: string; purpose: string; binding_source?: string; reconcile_note?: string; binding_no?: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/binding`, data)
  },
  overrideParam(id: number, data: { param_key: string; param_value: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/param`, data)
  },
  paramSync(id: number) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/param/sync`, {})
  },
  snapshot(id: number, data: { change_summary: string; snapshot_type?: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/snapshot`, data)
  },
  taskAction(id: number, data: { task_kind: string; execution_path?: string; version_ref?: string; master_endpoints?: string; kubeconfig_ref?: string; fail_step_index?: number; node_no?: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/task`, data)
  },
  probe(id: number, data: { apiserver_reachable: boolean; tls_handshake_ok: boolean; ready_nodes?: number; total_nodes?: number; fail_reason?: string }) {
    return http.post<ClcCluster>(`/api/clc/cluster/${id}/probe`, data)
  },
}

// ---------- 节点(AGG-CLC-02) ----------

export interface ClcNode {
  id: number
  node_no: string
  cluster_ref: string
  cluster_name?: string
  asset_ref: string | null
  node_role: string
  ip_address: string
  k8s_nodename: string | null
  node_status: string
  node_health: string
  node_health_at: string | null
  remark: string | null
}

export const nodeApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; node_role?: string; node_status?: string; node_health?: string; keyword?: string }) {
    return http.get<PageResult<ClcNode>>('/api/clc/node', params)
  },
  remove(id: number) {
    return http.post<ClcNode>(`/api/clc/node/${id}/remove`, {})
  },
  rebind(id: number, data: { new_asset_ref: string }) {
    return http.post<ClcNode>(`/api/clc/node/${id}/rebind`, data)
  },
}

// ---------- 引用与参数(AGG-CLC-03/04) ----------

export interface ClcBinding {
  id: number
  binding_no: string
  cluster_ref: string
  cluster_name?: string
  target_capability: string
  target_kind: string
  target_ref: string
  purpose: string
  binding_source: string
  reconcile_note: string | null
}

export const bindingApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; target_capability?: string; purpose?: string; binding_source?: string; keyword?: string }) {
    return http.get<PageResult<ClcBinding>>('/api/clc/binding', params)
  },
}

export interface ClcParam {
  id: number
  cluster_ref: string
  cluster_name?: string
  param_key: string
  param_value: string
  param_source: string
  derived_from: string | null
  derive_note: string | null
  sync_pending: number
  updated_at: string
}

export const paramApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; param_source?: string; sync_pending?: string; keyword?: string }) {
    return http.get<PageResult<ClcParam>>('/api/clc/param', params)
  },
}

// ---------- 快照(AGG-CLC-05) ----------

export interface ClcSnapshot {
  id: number
  snapshot_no: string
  cluster_ref: string
  cluster_name?: string
  revision_no: number
  change_summary: string
  snapshot_type: string
  created_at: string
}

export const snapshotApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; snapshot_type?: string; keyword?: string }) {
    return http.get<PageResult<ClcSnapshot>>('/api/clc/snapshot', params)
  },
  rollback(id: number, data: { abandon_refs?: boolean }) {
    return http.post<ClcCluster>(`/api/clc/snapshot/${id}/rollback`, data)
  },
}

// ---------- 任务(AGG-CLC-06) ----------

export interface ClcTask {
  id: number
  task_no: string
  cluster_ref: string
  cluster_name?: string
  task_kind: string
  task_status: string
  execution_path: string
  fail_step_index: number | null
  fail_reason: string | null
  result_summary: string | null
  started_at: string | null
  ended_at: string | null
  submitted_by: string
}

export const taskApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; task_kind?: string; task_status?: string; keyword?: string }) {
    return http.get<PageResult<ClcTask>>('/api/clc/task', params)
  },
}

// ---------- 探活(AGG-CLC-07) ----------

export interface ClcProbe {
  id: number
  probe_no: string
  cluster_ref: string
  cluster_name?: string
  probed_at: string
  trigger_source: string
  apiserver_reachable: number
  tls_handshake_ok: number
  ready_nodes: number
  total_nodes: number
  probe_result: string
  fail_reason: string | null
}

export const probeApi = {
  list(params: { page?: number; size?: number; cluster_ref?: string; probe_result?: string; from?: string; to?: string; keyword?: string }) {
    return http.get<PageResult<ClcProbe>>('/api/clc/probe', params)
  },
  aggregate(params: { cluster_ref?: string; status?: string }) {
    return http.get<{ list: Record<string, unknown>[]; total: number }>('/api/clc/health/aggregate', params)
  },
}

// ---------- 统计与平台参数(FUNC-29/30) ----------

export interface ClcStatistics {
  total: number
  running: number
  degraded: number
  unreachable: number
  destroyed: number
  sync_pending: number
  probe_total: number
  probe_failed: number
  probe_fail_rate: string
  revocation_pending: number
  snapshot_total: number
}

export const opsApi = {
  statistics() {
    return http.get<ClcStatistics>('/api/clc/statistics')
  },
  params() {
    return http.get<{ param_key: string; param_value: string; param_group: string; description: string; updated_at: string }[]>('/api/clc/params')
  },
  saveParams(items: Record<string, string>) {
    return http.put<{ param_key: string }[]>('/api/clc/params', { items })
  },
}

// ---------- 跨中心集群视图(FUNC-28) ----------

export const crossApi = {
  clusters(params: { page?: number; size?: number; status?: string; cluster_no?: string }) {
    return http.get<PageResult<Record<string, unknown>>>('/api/clc/cross/clusters', params)
  },
}
