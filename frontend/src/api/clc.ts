import { http, clearToken, getToken, type PageResult } from './request'

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
  /** FUNC-33 取消执行中任务(取消 ≠ 回滚;原因必填,失败提示由统一拦截器直出后端中文 errorMsg) */
  cancel(no: string, data: { reason: string }) {
    return http.post<ClcTask>(`/api/clc/task/${encodeURIComponent(no)}/cancel`, data)
  },
  /** FUNC-34 交付步骤查询(REP-12 c1~c9;步骤权威在后端,不含整表原始日志正文) */
  steps(no: string, params?: { step_status?: string; verdict_source?: string }) {
    return http.get<{ list: ClcTaskStep[]; total: number }>(`/api/clc/task/${encodeURIComponent(no)}/steps`, params)
  },
  /** FUNC-35 日志回放·游标增量补齐(offset = 已读最大 line_no;禁整表拉取,INV-43) */
  logs(no: string, params: { offset?: number; limit?: number; log_level?: string; step_index?: number }) {
    return http.get<ClcTaskLogsResult>(`/api/clc/task/${encodeURIComponent(no)}/logs`, params)
  },
}

// ---------- 交付执行内核数据契约(FUNC-33/34/35,v1.1;字段名与后端逐字对齐) ----------

/** 交付步骤行(REP-12 c1~c9;internal/service/clc_delivery.go ListTaskSteps) */
export interface ClcTaskStep {
  step_index: number
  step_code: string
  step_name: string
  step_status: string
  command_summary: string | null
  verdict_source: string | null
  exit_code: number | null
  started_at: string | null
  ended_at: string | null
  log_start_line: number | null
  log_end_line: number | null
  fail_detail: string | null
  skip_reason: string | null
}

/** 任务日志行(REP-13;internal/service/clc_delivery.go ReadTaskLogs) */
export interface ClcTaskLogLine {
  line_no: number
  step_index: number | null
  logged_at: string
  log_level: string
  content: string
}

export interface ClcTaskLogsResult {
  list: ClcTaskLogLine[]
  total: number
  offset: number
  task_status: string
}

export interface TaskLogStreamHandlers {
  onLine: (line: ClcTaskLogLine) => void
  onDone: (payload: { task_status: string; cursor: number }) => void
  onError: (message: string) => void
  /** 服务端关闭读流(终局 done 或网络断开);调用方据此决定是否续播 */
  onClose: () => void
}

/**
 * FUNC-35 日志回放 SSE。登录态走 Authorization 头,原生 EventSource 无法携带,
 * 故用 fetch + ReadableStream 手工按空行切帧(直连必 401)。返回关闭函数,卸载必须 abort。
 */
export function openTaskLogStream(taskNo: string, offset: number, handlers: TaskLogStreamHandlers): () => void {
  const controller = new AbortController()
  void runTaskLogStream(taskNo, offset, controller, handlers)
  return () => controller.abort()
}

async function runTaskLogStream(taskNo: string, offset: number, controller: AbortController, handlers: TaskLogStreamHandlers): Promise<void> {
  try {
    const token = getToken()
    const resp = await fetch(`/api/clc/task/${encodeURIComponent(taskNo)}/logs/stream?offset=${encodeURIComponent(String(offset))}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      signal: controller.signal,
    })
    if (resp.status === 401) {
      clearToken()
      window.location.href = '/login'
      return
    }
    if (!resp.ok || !resp.body) {
      handlers.onError(`日志订阅连接失败(HTTP ${resp.status})`)
      return
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      let sep = buffer.indexOf('\n\n')
      while (sep >= 0) {
        dispatchTaskLogFrame(buffer.slice(0, sep), handlers)
        buffer = buffer.slice(sep + 2)
        sep = buffer.indexOf('\n\n')
      }
    }
    handlers.onClose()
  } catch (err) {
    if (controller.signal.aborted) return
    handlers.onError(err instanceof Error ? err.message : '日志订阅连接异常')
  }
}

function dispatchTaskLogFrame(frame: string, handlers: TaskLogStreamHandlers): void {
  let event = 'message'
  const dataParts: string[] = []
  for (const rawLine of frame.split('\n')) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
    if (!line || line.startsWith(':')) continue // 心跳注释帧(: ping)
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataParts.push(line.slice(5).replace(/^ /, ''))
  }
  if (dataParts.length === 0) return
  let data: unknown
  try {
    data = JSON.parse(dataParts.join('\n'))
  } catch {
    return
  }
  if (event === 'log') {
    handlers.onLine(data as ClcTaskLogLine)
  } else if (event === 'done') {
    const d = data as { task_status?: string; cursor?: number }
    handlers.onDone({ task_status: d.task_status ?? '', cursor: Number(d.cursor ?? 0) })
  } else if (event === 'error') {
    handlers.onError(String((data as { message?: unknown }).message ?? '日志订阅异常'))
  }
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
