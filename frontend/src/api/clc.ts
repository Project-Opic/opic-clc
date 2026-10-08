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
  /** （v1.2 新增）来源模板引用：只在 FUNC-01 创建瞬间写入一次，此后只读、不回写（INV-49 / R-20）；空 = 未走模板 */
  template_ref?: string | null
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
  taskAction(id: number, data: { task_kind: string; execution_path?: string; version_ref?: string; master_endpoints?: string; kubeconfig_ref?: string; fail_step_index?: number; node_no?: string; step_mode?: string; operation_reason?: string }) {
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
  /** （v1.2 新增，字典 CLC_STEP_MODE）任务创建瞬间落定、此后只读，运行中不可互转（INV-50 / REF-17） */
  step_mode?: string
  /** （v1.2 新增）人工派发事由：跳步与高危生命周期动作必填，随任务行留痕（R-24） */
  operation_reason?: string | null
  /** 远端任务引用（INV-46：REMOTE 链路必非空；REF-12 跳转回放定位用） */
  remote_task_ref?: string | null
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

// ---------- 集群模板(AGG-CLC-08,FUNC-36~43;MU frmClcTemplate) ----------
// 契约实现见 internal/api/business/clc_template.go + internal/service/clc_template.go。
// 两处口径差异必须记牢（照抄姊妹仓 ignition-center 的参数面模板屏会错）：
//   1. 详情按「模板编号 template_no」读，不是自增 id —— GET /api/clc/template/:no；
//   2. 治理动作的 reason：enable/disable/default 走 JSON body，
//      而 DELETE 的 reason 走 query string（控制器是 c.Query("reason")）。本仓 http.del 只接
//      path（无 body 形参），故此处把 reason 拼进 path 的查询串，不动公共请求层语义。

/** 模板头(REP-14 列表行 / REP-15 详情主表；列表页不含参数行) */
export interface ClcTemplate {
  id: number
  template_no: string
  name: string
  description: string | null
  /** O-IGN 交付版本引用：只透传标识、不校验内容（R-21） */
  version_ref: string | null
  /** O-IGN 参数面模板标识（对端 AGG-IGN-07；只透传标识，本域不复制字段行 R-21） */
  param_template_ref: string | null
  /** 模板状态(字典 CLC_TEMPLATE_STATE：DRAFT / ENABLED / DISABLED；INV-51 迁移固定) */
  template_status: string
  is_default: boolean
  /** 来源集群引用(FUNC-43 集群分支留痕；el18 建成后只读，空 = 人工登记) */
  source_cluster_ref: string | null
  /** 服务层派生列：flag=0 即已软删（无物理删除入口） */
  deleted: boolean
  /** 服务层派生列：默认徽标只在「存活 + 启用 + is_default」时为真（INV-51 界面侧投影） */
  default_badge: boolean
  /** 服务层派生列：角色分布摘要（如 MASTER×3 + WORKER×5），由节点组即时聚合、不落库 */
  role_summary: string
  created_at: string
  updated_at: string
  /** REP-15 从表：节点组整组（仅详情接口返回） */
  node_groups?: ClcTemplateNodeGroup[]
  /** REP-15 从表：参数默认行整组（仅详情接口返回；值已过 INV-08 掩码兜底） */
  params?: ClcTemplateParamLine[]
  /** el16 命中引用反查：未销毁集群的引用清单（与 R-22 删除保护同一条反查） */
  used_by_clusters?: ClcTemplateUsedCluster[]
}

/** 模板节点组行(CLC-CLC-08-S1)：角色拓扑配方，无地址 / 无凭据列 */
export interface ClcTemplateNodeGroup {
  group_name: string
  node_role: string
  expected_count: number
  sort_no?: number
}

/** 模板参数默认行(CLC-CLC-08-S2)：键模板内唯一，键不建字典 */
export interface ClcTemplateParamLine {
  param_key: string
  param_value: string
  /** 详情接口返回：过掩码后的展示值（敏感值不整页展开） */
  param_value_masked?: string
  /** 详情接口返回：原值命中凭据形态（库里脏值兜底；正常登记侧已按 INV-08 拒收） */
  masked_hit?: boolean
  /** 详情接口返回：空串值的口径提示（须现场填，不是缺失） */
  display_note?: string
  description?: string | null
  sort_no?: number
}

/** 命中引用出处行(el16)：出处即下一步动作的位置（集群台账屏按来源模板筛） */
export interface ClcTemplateUsedCluster {
  cluster_no: string
  name: string
  status: string
}

const tplPath = (no: string) => `/api/clc/template/${encodeURIComponent(no)}`

export const templateApi = {
  /** act-FUNC-36 列表(REP-14)：status/is_default/deleted/keyword；status 只支持单值 */
  list(params: { page?: number; size?: number; status?: string; is_default?: string; deleted?: string; keyword?: string }) {
    return http.get<PageResult<ClcTemplate>>('/api/clc/template', params)
  },
  /** act-FUNC-37 详情(REP-15)：主表头 + 节点组 + 参数行 + 命中引用反查；按模板编号读取 */
  get(no: string) {
    return http.get<ClcTemplate>(tplPath(no))
  },
  /** act-FUNC-38 登记：草稿态，同事务可带整组子实体（node_groups / params） */
  create(data: {
    name: string
    description?: string
    version_ref?: string
    param_template_ref?: string
    node_groups?: ClcTemplateNodeGroup[]
    params?: Pick<ClcTemplateParamLine, 'param_key' | 'param_value' | 'description'>[]
  }) {
    return http.post<ClcTemplate>('/api/clc/template', data)
  },
  /** act-FUNC-39 整组编辑(INV-52)：节点组 + 参数行整组替换，reason 必填；头字段就地更新 */
  saveGroups(no: string, data: {
    reason: string
    name?: string
    description?: string
    version_ref?: string
    param_template_ref?: string
    node_groups: ClcTemplateNodeGroup[]
    params: Pick<ClcTemplateParamLine, 'param_key' | 'param_value' | 'description'>[]
  }) {
    return http.put<ClcTemplate>(`${tplPath(no)}/groups`, data)
  },
  /** act-FUNC-40 启用(reason 走 body；前置：节点组非空且至少一个 MASTER) */
  enable(no: string, reason: string) {
    return http.post<ClcTemplate>(`${tplPath(no)}/enable`, { reason })
  },
  /** act-FUNC-40 停用(reason 走 body；默认项须先撤默认，停用不回收集群引用 R-22) */
  disable(no: string, reason: string) {
    return http.post<ClcTemplate>(`${tplPath(no)}/disable`, { reason })
  },
  /** act-FUNC-41 设为默认(reason 走 body；仅 CLC-ROLE-02 发牌 PERM-FUNC-41) */
  setDefault(no: string, reason: string) {
    return http.post<ClcTemplate>(`${tplPath(no)}/default`, { reason })
  },
  /** act-FUNC-42 软删除：reason 走 query string（控制器 c.Query("reason")，与 body 语义不同） */
  del(no: string, reason: string) {
    return http.del<ClcTemplate>(`${tplPath(no)}?reason=${encodeURIComponent(reason)}`)
  },
  /**
   * act-FUNC-43 克隆：独立静态路径 /api/clc/template-clone（gin 同一方法树里静态段与 :no 同级会 panic）。
   * 来源二选一：source_template_no（从既有模板）/ source_cluster_no（el17 从既有集群存为模板，
   * 读集群当前节点组与参数行做初值、不带地址与凭据）。
   */
  clone(data: { name: string; description?: string; source_template_no?: string; source_cluster_no?: string }) {
    return http.post<ClcTemplate>('/api/clc/template-clone', data)
  },
}

// ---------- 装机向导与生命周期台(FUNC-44/45/46/47/48/49/50,v1.2;MU frmClcWizard) ----------
// 契约实现见 internal/api/business/clc_wizard.go + clc_readonly.go + internal/service/clc_wizard.go + clc_readonly.go。
// 请求体键名逐字对照后端 handler 实际读取的键（step_index / reason / task_kind / operation_reason /
// confirmed / file_face 等），响应走统一信封（列表键 list；只读代理为对端 flat JSON 透传 + 本域补充键）。

/** FUNC-50 预检判定项(internal/service/clc_readonly.go checkItem.toMap) */
export interface ClcReadinessCheckItem {
  id: string
  title: string
  pass: boolean
  note?: string
}

/** FUNC-50 交付前置完备性预检结论（即时提示、不拦截提交） */
export interface ClcReadinessReport {
  cluster_no: string
  cluster_name?: string
  all_pass: boolean
  checks: ClcReadinessCheckItem[]
  caliber_note?: string
}

/** FUNC-47 节点登录标识行（REP-16：只出「登录用户@节点地址」，零凭据出口，复制不留痕） */
export interface ClcLoginIdentity {
  node_no: string
  node_role: string
  ip_address: string
  login_identity: string
  /** R-03 人工接管注记（资产引用已置空、地址保留） */
  takeover: boolean
  takeover_note?: string
}

export interface ClcLoginIdentitiesResult {
  cluster_no: string
  cluster_name?: string
  ssh_login_user: string
  items: ClcLoginIdentity[]
  identity_format?: string
  caliber_note?: string
}

/**
 * FUNC-48 交付配置字段级视图行（REP-17；对端 O-IGN FUNC-50 跨中心通道 flat JSON 原样透传，
 * 本域不重算、不合并四层、不留副本 R-25）。文件轴中文名一律取行内 file_face_label（对端字典回吐，
 * DEF-05 裁定口径）；该键缺位时回显 file_face 码值本身——前端不出第二份轴表。
 */
export interface ClcIgnConfigFieldRow {
  field_key?: string
  ref_id?: string
  label?: string
  file_face?: string
  file_face_label?: string
  value_shape?: string
  value?: unknown
  hit_layer?: string
  hit_layer_label?: string
  source_ref?: string
  next_effective?: boolean
  applied_task_no?: string | null
  applied_at?: string | null
  applied_display?: string | null
  layers?: unknown
  [k: string]: unknown
}

/** FUNC-48 响应：成功=对端字段视图+本域补充键；不可达=200+peer_unreachable 信封（不缓存冒充现状） */
export interface ClcIgnConfigView {
  cluster_no?: string
  cluster_name?: string
  cluster_ref?: string
  file_face?: string
  param_template_ref?: string
  param_template_notice?: string
  version_ref?: string
  version_notice?: string
  notice?: string
  fields?: ClcIgnConfigFieldRow[]
  peer_unreachable?: boolean
  unreachable_kind?: string
  unreachable_reason?: string
  jump_entry?: string
  func_ref?: string
  local_caliber_note?: string
}

/** FUNC-49 原文行（对端流出前已按 R-16 凭据形态整行掩码；本域不二次解析 INV-08） */
export interface ClcIgnRawLine {
  no: number
  text: string
}

/** FUNC-49 响应：一次一面（file_face 必带 HOSTS/CONFIG）；每次读取后端逐次留痕（R-24 同族的合规档） */
export interface ClcIgnConfigRawView {
  cluster_no?: string
  cluster_name?: string
  cluster_ref?: string
  file_face?: string
  file_face_label?: string
  file_name?: string
  line_count?: number
  masked_line_count?: number
  content_sha256?: string
  lines?: ClcIgnRawLine[]
  read_origin?: string
  notice?: string
  peer_unreachable?: boolean
  unreachable_kind?: string
  unreachable_reason?: string
  jump_entry?: string
  func_ref?: string
  local_caliber_note?: string
}

const taskPath = (no: string) => `/api/clc/task/${encodeURIComponent(no)}`
const clusterPath = (id: number) => `/api/clc/cluster/${id}`

export const wizardApi = {
  /**
   * act-FUNC-44「执行本步」：STEP 任务派发队列首个 PENDING 后停在人工确认点（REF-17/B-AUTO-10）。
   * step_index 可选（界面期望序号）；与首个 PENDING 不符由服务层按 INV-50 拒绝并回「当前可执行的是第几步」。
   */
  stepExecute(no: string, data?: { step_index?: number }) {
    return http.post<Partial<ClcTask>>(`${taskPath(no)}/step-execute`, data ?? {})
  },
  /**
   * act-FUNC-44「重试本步」= 新建续跑任务（INV-31 原任务行不改写；已成功序号 R-17 置 SKIPPED）。
   * stepMode 由后端强制按向导入口取 STEP（REF-17），body 可带 execution_path / operation_reason 等 runClusterAction 既有键。
   */
  stepRetry(id: number, data?: Record<string, unknown>) {
    return http.post<Record<string, unknown>>(`${clusterPath(id)}/wizard-retry`, data ?? {})
  },
  /** act-FUNC-45 跳步：仅未执行（PENDING）步可跳，reason 事由必填（R-24，累加写任务头 operationReason） */
  stepSkip(no: string, data: { step_index: number; reason: string }) {
    return http.post<Record<string, unknown>>(`${taskPath(no)}/step-skip`, data)
  },
  /**
   * act-FUNC-46 生命周期台动作派发：动作集 START/STOP/UPGRADE/BACKUP/RESTORE/CERT_RENEW
   * （取 CLC_TASK_KIND 既有着陆，不新增类别值）；高危（停止/恢复）须 operation_reason + confirmed=true；
   * DESTROY 永不从本口直派——销毁唯一经审批流 PROC-APP-01（FUNC-05，INV-33/R-10）。
   */
  lifecycleAction(id: number, data: { task_kind: string; operation_reason?: string; confirmed?: boolean; version_ref?: string; execution_path?: string }) {
    return http.post<Record<string, unknown>>(`${clusterPath(id)}/lifecycle-action`, data)
  },
  /** act-FUNC-47 节点 SSH 登录标识（REP-16；PERM-FUNC-47 对只读角色开放；复制=前端剪贴板、后端不留痕） */
  nodeLoginIdentities(id: number) {
    return http.get<ClcLoginIdentitiesResult>(`${clusterPath(id)}/node-login-identities`)
  },
  /** act-FUNC-48 交付配置字段级代理视图（REP-17；PERM-FUNC-48；file_face/param_template_ref/version_ref 皆可选，留空由对端按自己口径回落） */
  ignConfig(id: number, params?: { file_face?: string; param_template_ref?: string; version_ref?: string }) {
    return http.get<ClcIgnConfigView>(`${clusterPath(id)}/ign-config`, params)
  },
  /** act-FUNC-49 交付配置原文只读代理（REP-18；独立权限码 PERM-FUNC-49；一次一面必带 file_face=HOSTS|CONFIG；每次读取逐次留痕——按钮语义是「读取原文」） */
  ignConfigRaw(id: number, params: { file_face: string }) {
    return http.get<ClcIgnConfigRawView>(`${clusterPath(id)}/ign-config-raw`, params)
  },
  /** act-FUNC-50 交付前置完备性预检（即时判定五档；结论不拦截，缺项如实列出；只读本域事实，不做跨中心同步探活） */
  deliveryReadiness(id: number) {
    return http.get<ClcReadinessReport>(`${clusterPath(id)}/delivery-readiness`)
  },
}

// ---------- 跨中心集群视图(FUNC-28) ----------

export const crossApi = {
  clusters(params: { page?: number; size?: number; status?: string; cluster_no?: string }) {
    return http.get<PageResult<Record<string, unknown>>>('/api/clc/cross/clusters', params)
  },
}
