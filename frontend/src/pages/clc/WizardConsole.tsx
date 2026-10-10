// 集群域 · 装机向导台(MU frmClcWizard,MASTER_DETAIL_FORM;AGG-CLC-06/01,FUNC-44/45/46/48/49/50/51,v1.2 + v1.3)
// 落点与口径:
//   el01 集群选择(非终态) + 进入即锁链路徽章(INV-32);el02 templateRef 建成后只读(INV-49);
//   el03 推进模式只在任务创建前可选,创建请求显式带 step_mode(缺省 STEP,REF-17/DEF-07),创建后任务行只读徽章(INV-50);
//   el04/05 完备性预检(FUNC-50)即时提示、不拦截提交;
//   el06 步进器行集合完全来自 REP-12(GET /task/:no/steps),界面不内置七步清单(REF-13,不照抄 ksk8s 缺陷①⑧);
//   el07 执行本步只对队列首个 PENDING 开放,每个置灰必给原因、不谎报排队(R-23/INV-50);
//   el08 重试本步 = 新建续跑任务(INV-31/R-17),原任务行永不改写;STALLED 给「显式换机」去台账(INV-32,绝不静默切换);
//   el09 跳步仅未执行步 + 事由必填 + 后果明示(R-24/INV-37);
//   el10/el18 日志两腿按执行位置二选一、不并存(REF-16):LOCAL 腿复用 TaskDetail 导出的 TaskLogPanel
//     (游标增量 + SSE,禁整表拉取,INV-43);REMOTE_EXECUTOR 腿用 RemoteExecLogPanel 代理直展点火中心的
//     ezctl/ansible 逐行原文(FUNC-51 / REP-19,2~3 秒游标短轮询,响应即弃不落本域,不可达显式说明 + 跳转兜底);
//   el11~13 右侧页签:配置(字段级代理,轴名取对端 file_face_label,缺位回显码值本身,DEF-05)/生命周期台/原文只读(独立权限码 PERM-FUNC-49,读取逐次留痕,不可达不显示历史原文);
//   el14 生命周期台动作集取 CLC_TASK_KIND 既有着陆;动作选项按 hasPerm 分档「不渲染」(DEF-06);
//   el15 提交销毁申请仍走审批 PROC-APP-01,本台永不直派 DESTROY(INV-33/R-10);el16 高危事由必填、el17 影响面即时读本域行。
// 权威在后端:本屏不写第二份语义副本 —— 步骤清单/枚举标签取 common.tsx 既有映射或后端回吐字段;
// 文本保存有真端点(不照抄 ksk8s 缺陷①),状态机置位由后端任务行驱动(缺陷②),日志鉴权走 Authorization 头(缺陷③),
// 无不可达死表(缺陷④),校验/拒绝由后端直出中文 errorMsg(缺陷⑤),无模板含真实 IP 与裸删除(缺陷⑥),
// 步进不覆盖写清单(缺陷⑦),执行位是真实派发端点(缺陷⑧)。
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert, Button, Card, Checkbox, Col, Descriptions, Empty, Form, Input, Modal, Radio, Row,
  Select, Space, Table, Tabs, Tag, Tooltip, Typography, message, theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  AuditOutlined, CaretRightOutlined, DeleteOutlined, ReloadOutlined, SafetyCertificateOutlined,
  StepForwardOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import {
  clusterApi, nodeApi, taskApi, wizardApi,
  type ClcCluster, type ClcIgnConfigFieldRow, type ClcIgnConfigRawView, type ClcIgnConfigView,
  type ClcReadinessReport, type ClcTask, type ClcTaskStep,
} from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import { useDictionaries } from '../../hooks/useDictionaries'
import TableToolbar from '../../admin/components/TableToolbar'
import { RemoteExecLogPanel, TaskLogPanel } from './TaskDetail'
import {
  CLUSTER_STATUS, EXEC_PATH, HIGH_RISK_LIFECYCLE_KINDS, LIFECYCLE_DISPATCH_KINDS, LIVE_TASK_STATUSES,
  STEP_STATUS_TONE, TASK_KIND, TASK_KIND_PERMS, TASK_STATUS, pill, withDictLabels, type ConsoleCtx,
} from './common'

// 「在途」= 尚未出结论、占用 INV-48 单实例位的任务判据，取自 common.tsx 唯一一份(台级徽章/轮询共用);
// STALLED 不占位但保留可见(挂起行给「重试/显式换机」)。
// 步骤「有结论」集(进度分子;权威行来自 REP-12)
const RESOLVED_STEP_STATUSES = ['SUCCEEDED', 'FAILED', 'SKIPPED', 'CANCELLED']

// el03 推进模式的选项与徽章文案取 /api/meta/dictionaries 的 CLC_STEP_MODE 词条(M1 只读投影),
// 界面不留副本 —— 字典改动即随接口生效、不需要重新发版(DEF-09)。

const showValue = (v: unknown): string => {
  if (v === null || v === undefined || v === '') return '-'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

// 置灰必给原因(R-23):antd 的 disabled Button 不派发 hover 事件,Tooltip 必须套在外层 span 上才可见
function GreyWithReason({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Tooltip title={title}>
      <span style={{ display: 'inline-block', cursor: 'not-allowed' }}>{children}</span>
    </Tooltip>
  )
}

// ---------------------------------------------------------------------------
// el12 配置页签:交付配置字段级代理视图(FUNC-48 / REP-17)。
// 对端 O-IGN flat JSON 原样透传,本域不重算四层、不留副本(R-25);文件轴中文名只取行内
// file_face_label(对端字典回吐),缺位回显 file_face 码值本身 —— 前端不写第二份轴表(DEF-05)。
// 失败口径(§9.9.7④):代理读失败不清空成空白,保留上次表体并标注「不代表现状」+ 跳转入口。
// 本组件唯一的载体屏是装机向导台(MU frmClcWizard el12 配置页签;REP-17)。节点台 / 模板台 / 引用台在冻结 MU 里
// 都没有 act-FUNC-48 元素,故不接入 —— 代理视图不借"复用"之名扩面到别的屏。
// ---------------------------------------------------------------------------

export interface IgnConfigFieldViewProps {
  clusterId: number
}

export function IgnConfigFieldView({ clusterId }: IgnConfigFieldViewProps) {
  const { token } = theme.useToken()
  const [view, setView] = useState<ClcIgnConfigView | null>(null)
  const [failure, setFailure] = useState<ClcIgnConfigView | null>(null)
  const [loading, setLoading] = useState(false)
  const [faceFilter, setFaceFilter] = useState<string>('')
  const [loadedFor, setLoadedFor] = useState<number | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    wizardApi
      .ignConfig(clusterId)
      .then((res) => {
        setLoadedFor(clusterId)
        if (res.peer_unreachable) {
          // 不可达:表体保留上次读取(若有)并标注;失败信封单独渲染
          setFailure(res)
        } else {
          setView(res)
          setFailure(null)
        }
      })
      .catch(() => {
        // 统一拦截器已直出后端中文 errorMsg;表体同样保留不清空(§9.9.7④)
        setLoadedFor(clusterId)
      })
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId])

  useEffect(() => {
    setView(null)
    setFailure(null)
    setFaceFilter('')
    load()
  }, [load])

  const rows = view?.fields ?? []
  // 轴过滤选项从行数据即时聚合(标签优先取对端 file_face_label,缺位用码值),不内置轴表
  const faceOptions = useMemo(() => {
    const seen = new Map<string, string>()
    for (const r of rows) {
      const code = String(r.file_face ?? '')
      if (code && !seen.has(code)) seen.set(code, String(r.file_face_label ?? code))
    }
    return [{ value: '', label: '全部文件面' }, ...Array.from(seen, ([code, label]) => ({ value: code, label: label === code ? code : `${label}（${code}）` }))]
  }, [rows])

  const filtered = faceFilter ? rows.filter((r) => String(r.file_face ?? '') === faceFilter) : rows

  const columns: ColumnsType<ClcIgnConfigFieldRow> = [
    { title: '字段键', dataIndex: 'field_key', key: 'field_key', width: 220, render: (v: unknown, r) => String(v ?? r.ref_id ?? '-') },
    { title: '名称', dataIndex: 'label', key: 'label', width: 160, ellipsis: true, render: (v: unknown) => String(v ?? '-') },
    {
      title: '文件面(轴名取对端回吐)',
      dataIndex: 'file_face',
      key: 'file_face',
      width: 150,
      render: (_: unknown, r) => (r.file_face_label ? `${r.file_face_label}（${r.file_face}）` : String(r.file_face ?? '-')),
    },
    { title: '形态', dataIndex: 'value_shape', key: 'value_shape', width: 90, render: (v: unknown) => String(v ?? '-') },
    { title: '当前值(对端掩码后)', dataIndex: 'value', key: 'value', ellipsis: true, render: (v: unknown) => showValue(v) },
    {
      title: '命中层(来源档由对端给出)',
      dataIndex: 'hit_layer',
      key: 'hit_layer',
      width: 150,
      ellipsis: true,
      render: (_: unknown, r) => String(r.hit_layer_label ?? r.hit_layer ?? '-'),
    },
    { title: '出处引用', dataIndex: 'source_ref', key: 'source_ref', width: 170, ellipsis: true, render: (v: unknown) => String(v ?? '-') },
    {
      title: '应用追溯',
      key: 'applied',
      width: 200,
      render: (_, r) => {
        if (r.applied_display) return String(r.applied_display)
        if (r.applied_task_no) return `已应用 ${r.applied_task_no}${r.applied_at ? ` @ ${r.applied_at}` : ''}`
        if (r.next_effective) return '待下次派发生效'
        return '-'
      },
    },
  ]

  return (
    <Space direction="vertical" size={9} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>重新读取代理视图</Button>
        <Select size="small" style={{ width: 200 }} value={faceFilter} options={faceOptions} onChange={setFaceFilter} />
      </Space>
      {failure && (
        <Alert
          type="warning"
          showIcon
          message={`交付域代理读取失败(${failure.unreachable_kind || 'UNREACHABLE'}):${failure.unreachable_reason || '对端不可达'}`}
          description={
            <Space direction="vertical" size={2}>
              <span>不缓存冒充现状(INV-43 同族口径);下方字段表为上次读取结果,不代表现状。</span>
              {failure.jump_entry && (
                <Typography.Link href={failure.jump_entry} target="_blank" rel="noreferrer">
                  跳转 O-IGN 交付域现场查看(对端口径 {failure.func_ref || 'FUNC-50'})
                </Typography.Link>
              )}
            </Space>
          }
        />
      )}
      {view && (
        <Space direction="vertical" size={2} style={{ fontSize: 12, color: token.colorTextSecondary }}>
          {view.param_template_ref && <span>参数面模板引用：{view.param_template_ref}（只透传标识，本域不复制字段行）</span>}
          {view.param_template_notice && <span>{view.param_template_notice}</span>}
          {view.version_ref && <span>交付版本引用：{view.version_ref}</span>}
          {view.version_notice && <span>{view.version_notice}</span>}
          {view.notice && <span>{view.notice}</span>}
          {view.local_caliber_note && <span>{view.local_caliber_note}</span>}
        </Space>
      )}
      <Table
        rowKey={(r) => `${String(r.file_face ?? '')}|${String(r.field_key ?? r.ref_id ?? Math.random())}`}
        size="small"
        columns={columns}
        dataSource={filtered}
        loading={loading && loadedFor === clusterId && rows.length === 0}
        pagination={false}
        scroll={{ x: 1200, y: 360 }}
        locale={{ emptyText: failure ? '暂无读取成功时的字段快照' : '字段视图加载中或由对端返回空集' }}
      />
      <Typography.Text style={{ fontSize: 12, color: token.colorTextSecondary }}>
        本视图只读、无编辑态;字段修改在 O-IGN 参数面完成,本域下次派发时经代理取最新视图(改后下次生效,不留副本不重算)。
      </Typography.Text>
    </Space>
  )
}

// ---------------------------------------------------------------------------
// el13 原文页签:hosts / config.yaml 原文只读代理(FUNC-49 / REP-18,独立权限码 PERM-FUNC-49)。
// 按钮语义 =「读取原文」,每次点击后端逐次写审计留痕;不缓存、不自动刷新冒充实时。
// 不可达时清空展示(不显示历史原文副本,不留死表 —— ksk8s 缺陷④不照抄),只报错 + 给跳转入口。
// 载体屏只有装机向导台(MU frmClcWizard el13);其余屏在冻结 MU 里没有 act-FUNC-49 元素,不借「复用」扩面。
// ---------------------------------------------------------------------------

export function IgnConfigRawView({ clusterId }: { clusterId: number }) {
  const { token } = theme.useToken()
  const [face, setFace] = useState('HOSTS')
  const [view, setView] = useState<ClcIgnConfigRawView | null>(null)
  const [failure, setFailure] = useState<ClcIgnConfigRawView | null>(null)
  const [state, setState] = useState<'idle' | 'loading' | 'ok' | 'unreachable'>('idle')
  const [readAt, setReadAt] = useState('')

  const read = () => {
    setState('loading')
    wizardApi
      .ignConfigRaw(clusterId, { file_face: face })
      .then((res) => {
        if (res.peer_unreachable) {
          setView(null) // 不可达不显示历史原文(不留本域副本)
          setFailure(res)
          setState('unreachable')
        } else {
          setFailure(null)
          setView(res)
          setReadAt(new Date().toLocaleString())
          setState('ok')
        }
      })
      .catch(() => {
        setView(null)
        setFailure(null)
        setState('unreachable')
      })
  }

  return (
    <Space direction="vertical" size={9} style={{ width: '100%' }}>
      <Space wrap>
        {/* 轴选项只列码值(HOSTS/CONFIG 为请求参数码,非中文轴表);读取成功后头部回显对端 file_face_label */}
        <Radio.Group
          size="small"
          optionType="button"
          value={face}
          onChange={(e) => setFace(e.target.value as string)}
          options={[{ value: 'HOSTS', label: 'HOSTS' }, { value: 'CONFIG', label: 'CONFIG' }]}
        />
        <Button size="small" type="primary" icon={<AuditOutlined />} loading={state === 'loading'} onClick={read}>
          读取原文（逐次留痕）
        </Button>
      </Space>
      {state === 'idle' && (
        <Alert type="info" showIcon message="原文非常驻视图：点击「读取原文」每次实时经代理读取(后端逐次写审计)，敏感行在对端流出前已整行掩码(R-16)。" />
      )}
      {(state === 'unreachable' || failure) && (
        <Alert
          type="error"
          showIcon
          message={failure ? `交付域不可达(${failure.unreachable_kind || 'UNREACHABLE'})：${failure.unreachable_reason || '对端读取失败'}` : '原文读取请求失败(中文原因已由后端直出)'}
          description={
            failure?.jump_entry ? (
              <Typography.Link href={failure.jump_entry} target="_blank" rel="Noreferrer" >
                跳转 O-IGN 交付域现场查看(对端口径 {failure.func_ref || 'FUNC-46'})
              </Typography.Link>
            ) : (
              <span>本域不保留历史原文副本，不可达时不显示任何旧内容(不留死表)。</span>
            )
          }
        />
      )}
      {view && state === 'ok' && (
        <>
          <Descriptions
            size="small"
            column={2}
            bordered
            items={[
              { key: 'face', label: '文件面', children: view.file_face_label ? `${view.file_face_label}（${view.file_face}）` : view.file_face || '-' },
              { key: 'name', label: '文件名', children: view.file_name || '-' },
              { key: 'lines', label: '行数', children: showValue(view.line_count) },
              { key: 'masked', label: '已掩码行数', children: showValue(view.masked_line_count) },
              { key: 'sha', label: '内容指纹 sha256', children: view.content_sha256 || '-' },
              { key: 'at', label: '本域读取时刻', children: readAt || '-' },
              { key: 'origin', label: '读取口径', span: 2, children: view.read_origin || view.notice || '-' },
            ]}
          />
          <pre
            style={{
              margin: 0,
              maxHeight: 380,
              overflow: 'auto',
              background: token.colorFillQuaternary,
              border: `1px solid ${token.colorBorderSecondary}`,
              borderRadius: token.borderRadiusSM,
              padding: '6px 9px',
              fontFamily: 'monospace',
              fontSize: 12,
              lineHeight: 1.7,
              whiteSpace: 'pre-wrap',
            }}
          >
            {(view.lines || []).map((l) => `${l.no}\t${l.text}`).join('\n') || '(空文件)'}
          </pre>
        </>
      )}
    </Space>
  )
}

// ---------------------------------------------------------------------------
// el14/15/16/17 生命周期台(主屏右侧页签之一;v1.3 起同一组件也是集群台「生命周期」页签的动作卡载体
//   —— 9.10.3 所记「唯一一处主登记位置换」的落点，FUNC-46 自身写入腿与权限码档位不变，不是宿主屏新开写口)
// ---------------------------------------------------------------------------

export function LifecycleConsole({ cluster, onDispatched }: { cluster: ClcCluster; onDispatched: () => void }) {
  const hasPerm = usePermission()
  const [highTarget, setHighTarget] = useState<string | null>(null)
  const [reason, setReason] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [impact, setImpact] = useState<{ masterCount: number | null; liveTasks: number | null } | null>(null)
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  const [versionRef, setVersionRef] = useState('')
  const [dispatching, setDispatching] = useState(false)
  const [destroyOpen, setDestroyOpen] = useState(false)
  const [destroyImpact, setDestroyImpact] = useState<Record<string, unknown> | null>(null)
  const [destroyReason, setDestroyReason] = useState('')

  // DEF-06:动作选项按「路由牌 PERM-FUNC-46 + kind 档 TASK_KIND_PERMS[k]」双重分档,缺档不渲染(非置灰);
  // kind→权限码取 common.tsx 唯一一份表,与 frmClcOrch 共用,不出第二份。
  const kinds = LIFECYCLE_DISPATCH_KINDS.filter((k) => hasPerm('PERM-FUNC-46') && hasPerm(TASK_KIND_PERMS[k]))

  const dispatch = async (body: { task_kind: string; operation_reason?: string; confirmed?: boolean; version_ref?: string }) => {
    setDispatching(true)
    try {
      await wizardApi.lifecycleAction(cluster.id, body)
      message.success(`生命周期动作「${TASK_KIND[body.task_kind] || body.task_kind}」已受理,任务排队执行(台账留痕,只增不改)`)
      setHighTarget(null)
      setUpgradeOpen(false)
      setReason('')
      setConfirmed(false)
      setVersionRef('')
      onDispatched()
    } catch {
      /* 拦截器已直出后端中文 errorMsg(如 DESTROY 拒绝 INV-33) */
    } finally {
      setDispatching(false)
    }
  }

  const openHigh = (kind: string) => {
    setHighTarget(kind)
    setReason('')
    setConfirmed(false)
    setImpact(null)
    // el17 影响面即时读本域集群与任务行(不留旧快照)
    void (async () => {
      try {
        const [nodes, tasks] = await Promise.all([
          nodeApi.list({ page: 1, size: 1, cluster_ref: cluster.cluster_no, node_role: 'MASTER' }),
          taskApi.list({ page: 1, size: 20, cluster_ref: cluster.cluster_no }),
        ])
        setImpact({ masterCount: nodes.total ?? 0, liveTasks: (tasks.list || []).filter((t) => LIVE_TASK_STATUSES.includes(t.task_status)).length })
      } catch {
        setImpact(null)
      }
    })()
  }

  const openDestroy = async () => {
    setDestroyOpen(true)
    setDestroyReason('')
    setDestroyImpact(null)
    try {
      setDestroyImpact(await clusterApi.destroyImpact(cluster.id))
    } catch {
      setDestroyImpact(null)
    }
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Alert
        type="info"
        showIcon
        message="动作集取 CLC_TASK_KIND 既有着陆,不新增类别值;启停/升级/备份/恢复与编排台同源(复用既有任务派发语义),证书续期经生命周期口(FUNC-46)。"
        description="销毁永远不从此台直派(INV-33/R-10)——只有下方「提交销毁申请」经审批流 PROC-APP-01。"
      />
      {kinds.length === 0 ? (
        <Typography.Text type="secondary">当前角色未持有本台动作档权限(PERM-FUNC-46 与对应动作档),按 DEF-06 不渲染任何动作按钮。</Typography.Text>
      ) : (
        <Space wrap>
          {kinds.map((k) =>
            k === 'CERT_RENEW' ? (
              <Button key={k} type="primary" icon={<SafetyCertificateOutlined />} loading={dispatching} onClick={() => dispatch({ task_kind: k })}>
                {TASK_KIND[k]}（证书续期）
              </Button>
            ) : HIGH_RISK_LIFECYCLE_KINDS.includes(k) ? (
              <Button key={k} danger loading={dispatching} onClick={() => openHigh(k)}>
                {TASK_KIND[k]}
              </Button>
            ) : k === 'UPGRADE' ? (
              <Button key={k} loading={dispatching} onClick={() => { setVersionRef(cluster.version_ref || ''); setUpgradeOpen(true) }}>
                {TASK_KIND[k]}
              </Button>
            ) : (
              <Button key={k} loading={dispatching} onClick={() => dispatch({ task_kind: k })}>
                {TASK_KIND[k]}
              </Button>
            ),
          )}
        </Space>
      )}
      {hasPerm('PERM-FUNC-05') && (
        <Button danger icon={<DeleteOutlined />} onClick={openDestroy}>
          提交销毁申请（仍走审批）
        </Button>
      )}

      {/* 高危二次确认弹层:el17 影响面(即时只读)+ el16 事由必填 + 显式确认勾选(R-24) */}
      <Modal
        title={`高危动作二次确认：${highTarget ? TASK_KIND[highTarget] || highTarget : ''} → ${cluster.name}(${cluster.cluster_no})`}
        open={!!highTarget}
        okText="确认派发"
        okButtonProps={{ danger: true, disabled: !reason.trim() || !confirmed, loading: dispatching }}
        onOk={() => highTarget && dispatch({ task_kind: highTarget, operation_reason: reason.trim(), confirmed: true })}
        onCancel={() => setHighTarget(null)}
        width={560}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={
            highTarget === 'STOP'
              ? '停止集群将使全部节点上的在运负载不可访问;事由将写入任务头只读留痕(R-24),不填不放行。'
              : '恢复将以快照回写集群状态,现有运行时状态将被覆盖;事由将写入任务头只读留痕(R-24),不填不放行。'
          }
        />
      <Descriptions
          size="small"
          column={1}
          bordered
          style={{ marginBottom: 12 }}
          items={[
            { key: 'm', label: '控制面台数', children: impact ? `${impact.masterCount ?? '-'} 台(即时读节点台账)` : '影响面加载中…' },
            { key: 'l', label: '在途任务', children: impact ? `${impact.liveTasks ?? '-'} 个(在途派发会与本动作竞争,INV-48 同集群单实例位)` : '-' },
            { key: 'w', label: '负载迁移提示', children: '工作负载需由业务侧先行迁移或接受中断;本台不代为迁移、不静默切换执行链路(INV-32)。' },
          ]}
        />
        <Space direction="vertical" size={9} style={{ width: '100%' }}>
          <div>
            <Typography.Text strong>事由(必填,el16)</Typography.Text>
            <Input.TextArea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="如：故障演练窗口内停机，业务已迁移" />
          </div>
          <Checkbox checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)}>
            我已阅读影响面并确认承担上述后果(el17 后果明示)
          </Checkbox>
        </Space>
      </Modal>

      <Modal
        title={`升级派发 → ${cluster.name}(${cluster.cluster_no})`}
        open={upgradeOpen}
        okText="派发升级任务"
        confirmLoading={dispatching}
        onOk={() => dispatch({ task_kind: 'UPGRADE', version_ref: versionRef.trim() || undefined })}
        onCancel={() => setUpgradeOpen(false)}
        width={520}
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Typography.Text type="secondary">目标交付版本引用可空(空 = 对端按当前默认版本口径);只透传标识,本域不校验版本内容(R-21 同族)。</Typography.Text>
          <Input value={versionRef} onChange={(e) => setVersionRef(e.target.value)} placeholder="IGN-VER-xxxx(可空)" />
        </Space>
      </Modal>

      {/* el15:销毁只经审批(PROC-APP-01),影响面强制展示(R-10),本台永不直派 DESTROY(INV-33) */}
      <Modal
        title={`提交销毁申请 → ${cluster.name}(${cluster.cluster_no})`}
        open={destroyOpen}
        okText="提交申请（进审批）"
        okButtonProps={{ danger: true, disabled: !destroyReason.trim(), loading: dispatching }}
        onOk={async () => {
          setDispatching(true)
          try {
            await clusterApi.destroySubmit(cluster.id, { reason: destroyReason.trim() })
            message.success('销毁申请已提交,等待审批(CLC-ROLE-02,PROC-APP-01;审批通过前集群状态不变)')
            setDestroyOpen(false)
            onDispatched()
          } catch {
            /* 拦截器已提示 */
          } finally {
            setDispatching(false)
          }
        }}
        onCancel={() => setDestroyOpen(false)}
        width={600}
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message="销毁仍走审批(PROC-APP-01),本生命周期台永不直派 DESTROY(INV-33/R-10);影响面强制展示(R-10)。" />
        {destroyImpact ? (
          <Descriptions
            size="small"
            column={1}
            bordered
            style={{ marginBottom: 12 }}
            items={[
              { key: 'n', label: '节点', children: `${showValue(destroyImpact.node_count)} 个` },
              { key: 'b', label: '引用台账', children: `${showValue(destroyImpact.binding_count)} 项(审批通过后级联清理)` },
              { key: 'c', label: '基础设施证书', children: `${showValue(destroyImpact.cert_count)} 项(销毁链吊销,残留可见)` },
              { key: 'd', label: '销毁编排', children: showValue(destroyImpact.destroy_orch) },
            ]}
          />
        ) : (
          <Alert type="info" showIcon style={{ marginBottom: 12 }} message="影响面加载中或集群暂不可销毁" />
        )}
        <div>
          <Typography.Text strong>销毁原因(必填)</Typography.Text>
          <Input.TextArea rows={3} value={destroyReason} onChange={(e) => setDestroyReason(e.target.value)} placeholder="说明销毁理由与影响面确认结论" />
        </div>
      </Modal>
    </Space>
  )
}

// ---------------------------------------------------------------------------
// 主屏
// ---------------------------------------------------------------------------

// 集群台嵌入态(v1.3):传入 ctx 时本屏是「安装」页签的载体 —— 集群与任务行取自台级(REP-07 同一份行,
// 不在页签内各拉一次),el01 集群选择器与右侧「配置/生命周期台/原文只读」页签组不渲染:
// 前两块上移到台级同级页签(「配置」「生命周期」),el13 原文只读仍留在本「安装」页签、由宿主屏装配在步进与日志之下
// (Q-117 / 9.10.1:安装页签 = 步进器 + 两腿日志 + 预检 + 原文只读),步进与日志本体、预检、派发链路一字不动。
export default function WizardConsole({ ctx }: { ctx?: ConsoleCtx } = {}) {
  const hasPerm = usePermission()
  const navigate = useNavigate()
  const { token } = theme.useToken()
  // 步骤状态 / 判定依据 / 推进模式的中文名一律取接口词条(DEF-09:界面不留字典副本)。
  const dict = useDictionaries()

  const [clusters, setClusters] = useState<ClcCluster[]>([])
  const [clusterIdSelf, setClusterId] = useState<number | null>(null)
  const [clusterSelf, setCluster] = useState<ClcCluster | null>(null)
  const [tasksSelf, setTasks] = useState<ClcTask[]>([])
  const clusterId = ctx?.cluster.id ?? clusterIdSelf
  const cluster = ctx?.cluster ?? clusterSelf
  const tasks = ctx?.tasks ?? tasksSelf
  const [steps, setSteps] = useState<ClcTaskStep[]>([])
  const [stepsError, setStepsError] = useState('')
  const [stepModeIntent, setStepModeIntent] = useState('STEP')
  const [readiness, setReadiness] = useState<ClcReadinessReport | null>(null)
  const [readinessLoading, setReadinessLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [working, setWorking] = useState(false)
  const [skipTarget, setSkipTarget] = useState<ClcTaskStep | null>(null)
  const [skipReason, setSkipReason] = useState('')
  const [cancelOpen, setCancelOpen] = useState(false)
  const [cancelReason, setCancelReason] = useState('')

  const attached = useMemo<ClcTask | null>(
    () => tasks.find((t) => LIVE_TASK_STATUSES.includes(t.task_status)) ?? tasks[0] ?? null,
    [tasks],
  )
  const attachedLive = !!attached && LIVE_TASK_STATUSES.includes(attached.task_status)

  // 嵌入态下 REP-07 的任务行由台级持有 ⇒ 本屏不再各拉一次；cluster 行同理，刷新即 ctx.refresh()（9.10.7 ①）。
  const reloadTasks = useCallback((clusterNo: string) => {
    if (ctx || !hasPerm('PERM-FUNC-25')) return
    taskApi
      .list({ cluster_ref: clusterNo, page: 1, size: 20 })
      .then((r) => setTasks(r.list || []))
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasPerm, ctx])

  const reloadCluster = useCallback((id: number) => {
    if (ctx) {
      ctx.refresh()
      return
    }
    if (!hasPerm('PERM-FUNC-20')) return
    clusterApi.get(id).then((c) => setCluster(c)).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasPerm, ctx])

  const loadReadiness = useCallback((id: number) => {
    setReadinessLoading(true)
    wizardApi
      .deliveryReadiness(id)
      .then((r) => setReadiness(r))
      .catch(() => setReadiness(null))
      .finally(() => setReadinessLoading(false))
  }, [])

  // 集群切换:拉详情 + 任务序列 + 后台预检(§9.9.6:预检后台读取不遮挡;结论不拦截,el05)
  // 嵌入态下行与任务序列已在台级就位,挂载只补预检,不再回头触发一次 ctx.refresh()。
  useEffect(() => {
    if (clusterId === null) return
    setSteps([])
    setStepsError('')
    if (!ctx) {
      reloadCluster(clusterId)
      reloadTasks(cluster?.cluster_no || '')
    }
    if (hasPerm('PERM-FUNC-50')) loadReadiness(clusterId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId])

  // 集群列表(el01:非终态集群;入口数据接口 PERM-FUNC-19)。嵌入态集群由台级唯一确定,el01 不渲染也就不必拉清单。
  useEffect(() => {
    if (ctx) return
    if (!hasPerm('PERM-FUNC-19')) return
    clusterApi
      .list({ page: 1, size: 100 })
      .then((r) => {
        const list = (r.list || []).filter((c) => c.status !== 'DESTROYED')
        setClusters(list)
        if (list.length > 0) setClusterId((prev) => prev ?? list[0].id)
      })
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // clusterNo 变化时同步任务查询键(cluster 详情晚于 id 到达)
  const clusterNo = cluster?.cluster_no || ''
  useEffect(() => {
    if (clusterId !== null && clusterNo) reloadTasks(clusterNo)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterNo])

  // 步进器行权威 = REP-12(GET /task/:no/steps,分母不写死七步,REF-13);步骤查看权限 PERM-FUNC-34,缺牌不渲染步进器
  const attachedNo = attached?.task_no || ''
  const loadSteps = useCallback(() => {
    if (!attachedNo || !hasPerm('PERM-FUNC-34')) return
    taskApi
      .steps(attachedNo)
      .then((r) => {
        const list = (r.list || []).slice().sort((a, b) => a.step_index - b.step_index)
        setSteps(list)
        setStepsError('')
      })
      .catch((e: Error) => setStepsError(e.message || '步骤行加载失败'))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachedNo, hasPerm])

  useEffect(() => {
    setSteps([])
    loadSteps()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachedNo])

  // 在途任务 5s 轮询刷新任务行 + 步骤行(步进解禁无需整屏刷新,§9.9.7③);日志面板自带 SSE 不在此列。
  // 嵌入态下 REP-02/REP-07 的轮询归台级(ClusterConsole),本页签只补步骤行,否则同一份行两个周期各拉一遍。
  useEffect(() => {
    if (!attachedLive || !clusterNo) return
    const timer = window.setInterval(() => {
      if (!ctx) {
        reloadTasks(clusterNo)
        if (clusterId !== null) reloadCluster(clusterId)
      }
      loadSteps()
    }, 5000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachedLive, attachedNo, clusterNo, clusterId, ctx])

  const refreshAll = () => {
    if (ctx) {
      ctx.refresh()
      loadSteps()
      return
    }
    if (clusterId !== null) reloadCluster(clusterId)
    if (clusterNo) reloadTasks(clusterNo)
    loadSteps()
  }

  // el07 执行判定:队列首个 PENDING 才可派发;每一档置灰都给原因,不谎报「已排队」(R-23)
  // 注意:比较用 step_index(REP-12 行权威序号,从 1 起),不是数组下标
  const firstPendingStep = useMemo(() => steps.find((s) => s.step_status === 'PENDING') || null, [steps])
  const runningStep = useMemo(() => steps.find((s) => s.step_status === 'RUNNING') || null, [steps])

  const executeBlockReason = (row: ClcTaskStep): string | null => {
    if (!attached) return '未附着任务'
    if (attached.step_mode && attached.step_mode !== 'STEP') return `${dict.labelOf('CLC_STEP_MODE', 'AUTO')}（AUTO）任务由执行内核连跑，「执行本步」只对${dict.labelOf('CLC_STEP_MODE', 'STEP')}（STEP）任务开放（REF-17）`
    if (attached.task_status === 'STALLED') return '失联挂起：不可静默续跑，请用「重试本步」或前往集群台账「显式换机」（INV-32）'
    if (!LIVE_TASK_STATUSES.includes(attached.task_status)) return `任务已终态（${TASK_STATUS[attached.task_status]?.label || attached.task_status}），不再派发新步（INV-29）`
    if (runningStep) return `第 ${runningStep.step_index} 步执行中，待本步结论后再派发下一步（R-23 一步一收）`
    if (firstPendingStep === null || row.step_index !== firstPendingStep.step_index) return `仅队列首个 PENDING（第 ${firstPendingStep?.step_index ?? '-'} 步）可派发；其后待执行步暂不开放（REF-17/INV-50）`
    return null
  }

  const onExecute = (row: ClcTaskStep) => {
    if (!attached) return
    setWorking(true)
    wizardApi
      .stepExecute(attached.task_no, { step_index: row.step_index })
      .then(() => {
        message.success(`第 ${row.step_index} 步「${row.step_name}」已受理派发,完成后停在人工确认点`)
        refreshAll()
      })
      .catch(() => undefined)
      .finally(() => setWorking(false))
  }

  // el08 重试 = 新建续跑任务(INV-31 原行不改写;后端强制按向导入口取 STEP,REF-17;已成功序号 R-17 置 SKIPPED)
  const onRetry = () => {
    if (!cluster) return
    setWorking(true)
    wizardApi
      .stepRetry(cluster.id, {})
      .then(() => {
        message.success('已新建续跑任务(原任务行不改写,INV-31;上次已成功序号置 SKIPPED,R-17;推进模式按向导入口取 STEP)')
        reloadCluster(cluster.id)
        reloadTasks(cluster.cluster_no)
      })
      .catch(() => undefined)
      .finally(() => setWorking(false))
  }

  const retryBlockReason = attached
    ? attachedLive
      ? '任务仍在途,待其出结论后再走重试(一步一收,R-23)'
      : cluster && cluster.status !== 'INSTALL_FAILED'
        ? `「重试本步」= 新建续跑任务(INV-31),前置为集群处于「安装失败」;当前集群状态:${CLUSTER_STATUS[cluster.status]?.label || cluster.status}`
        : null
    : null

  const stepStatusMap = withDictLabels(STEP_STATUS_TONE, dict.itemsOf('CLC_STEP_STATUS'))

  const columns: ColumnsType<ClcTaskStep> = [
    { title: '序号', dataIndex: 'step_index', key: 'step_index', width: 56 },
    { title: '步骤', key: 'step', width: 200, render: (_, r) => `${r.step_code} ${r.step_name}`.trim() },
    {
      title: '状态',
      dataIndex: 'step_status',
      key: 'step_status',
      width: 150,
      render: (v: string, r) => (
        <Tooltip title={v === 'SKIPPED' && r.skip_reason ? `跳过来源透传:${r.skip_reason}` : undefined}>
          <span>{pill(stepStatusMap, v)}</span>
        </Tooltip>
      ),
    },
    {
      title: '判定依据',
      dataIndex: 'verdict_source',
      key: 'verdict_source',
      width: 150,
      render: (v: string | null, r) => {
        if (!v && (r.exit_code === null || r.exit_code === undefined)) return '-'
        const label = v ? dict.labelOf('CLC_STEP_VERDICT', v) : '-'
        return r.exit_code !== null && r.exit_code !== undefined ? `${label}(${r.exit_code})` : label
      },
    },
    { title: '失败摘要', dataIndex: 'fail_detail', key: 'fail_detail', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '跳过原因(两来源分显)', dataIndex: 'skip_reason', key: 'skip_reason', width: 170, ellipsis: true, render: (v: string | null) => v || '-' },
    {
      title: '日志区间',
      key: 'log_range',
      width: 110,
      render: (_, r) => (r.log_start_line === null || r.log_start_line === undefined ? '-' : `#${r.log_start_line}~#${r.log_end_line ?? '-'}`),
    },
    { title: '起止时间', key: 'time', width: 230, render: (_, r) => `${r.started_at || '-'} ~ ${r.ended_at || '-'}` },
    {
      title: '动作',
      key: 'actions',
      width: 300,
      fixed: 'right',
      render: (_, r) => {
        const btns: ReactNode[] = []
        if (hasPerm('PERM-FUNC-44') && r.step_status === 'PENDING') {
          const block = executeBlockReason(r)
          btns.push(
            block ? (
              <GreyWithReason key="exec" title={block}>
                <Button size="small" disabled icon={<StepForwardOutlined />}>执行本步</Button>
              </GreyWithReason>
            ) : (
              <Button key="exec" size="small" type="primary" icon={<StepForwardOutlined />} loading={working} onClick={() => onExecute(r)}>
                执行本步
              </Button>
            ),
          )
        }
        if (hasPerm('PERM-FUNC-44') && r.step_status === 'FAILED') {
          btns.push(
            retryBlockReason ? (
              <GreyWithReason key="retry" title={retryBlockReason}>
                <Button size="small" disabled>重试本步</Button>
              </GreyWithReason>
            ) : (
              <Button key="retry" size="small" danger onClick={onRetry}>
                重试本步（新建续跑）
              </Button>
            ),
          )
        }
        if (hasPerm('PERM-FUNC-45')) {
          const skipBlock =
            r.step_status === 'PENDING'
              ? null
              : `仅未执行(PENDING)步可跳;本行${dict.labelOf('CLC_STEP_STATUS', r.step_status)},不可对已执行/终态行跳步(INV-37/INV-41 不做终态回滚)`
          btns.push(
            skipBlock ? (
              <GreyWithReason key="skip" title={skipBlock}>
                <Button size="small" disabled>跳过此步</Button>
              </GreyWithReason>
            ) : (
              <Button key="skip" size="small" onClick={() => { setSkipTarget(r); setSkipReason('') }}>
                跳过此步
              </Button>
            ),
          )
        }
        return <Space size={4} wrap>{btns.length > 0 ? btns : <Typography.Text type="secondary">-</Typography.Text>}</Space>
      },
    },
  ]

  const concluded = steps.filter((s) => RESOLVED_STEP_STATUSES.includes(s.step_status)).length

  const startInstall = async () => {
    if (!cluster) return
    setCreating(true)
    try {
      // DEF-07:向导屏创建任务必须显式带 step_mode(缺省 STEP);AUTO 既有连跑语义由后端执行内核承担,不改
      await clusterApi.taskAction(cluster.id, { task_kind: 'INSTALL', step_mode: stepModeIntent })
      message.success('安装任务已创建(推进模式落定只读,INV-50);步骤清单由后端任务创建后给出(REP-12)')
      reloadCluster(cluster.id)
      reloadTasks(cluster.cluster_no)
      if (hasPerm('PERM-FUNC-50')) loadReadiness(cluster.id)
    } catch {
      /* 拦截器已提示(前置校验在后端:仅 DRAFT/INSTALL_FAILED 可 INSTALL) */
    } finally {
      setCreating(false)
    }
  }

  // PERM-FUNC-19 只挡独立形态的 el01 集群清单;嵌入态集群由台级(REP-02)带来,不再以 19 为前置。
  if (!ctx && !hasPerm('PERM-FUNC-19')) {
    return (
      <Card>
        <Alert type="warning" showIcon message="当前角色未持有集群台账读取权限(PERM-FUNC-19),装机向导台无主数据可展示。" />
      </Card>
    )
  }

  const tabItems = []
  if (cluster && hasPerm('PERM-FUNC-48')) {
    tabItems.push({
      key: 'config',
      label: '配置（代理字段视图）',
      children: <IgnConfigFieldView key={cluster.id} clusterId={cluster.id} />,
    })
  }
  if (cluster) {
    tabItems.push({ key: 'lifecycle', label: '生命周期台', children: <LifecycleConsole key={cluster.id} cluster={cluster} onDispatched={refreshAll} /> })
  }
  if (cluster && hasPerm('PERM-FUNC-49')) {
    tabItems.push({ key: 'raw', label: '原文只读', children: <IgnConfigRawView key={cluster.id} clusterId={cluster.id} /> })
  }

  return (
    <Card>
      <TableToolbar
        title="装机向导台"
        description="步进器行集合完全来自后端 REP-12(不内置七步);执行位进入即锁(INV-32);步进/跳步/生命周期台全部真实端点派发,状态权威在后端"
        extra={
          <Space wrap>
            {/* el01 集群选择(非终态)。嵌入态:集群由台级唯一确定,9.10.1 明写不再重复回答「我在看哪个集群」。 */}
            {!ctx && (
              <Select
                showSearch
                style={{ width: 260 }}
                placeholder="选择集群(非终态)"
                optionFilterProp="label"
                value={clusterId ?? undefined}
                onChange={(v: number) => { setClusterId(v); setCluster(null); setTasks([]) }}
                options={clusters.map((c) => ({ value: c.id, label: `${c.name}（${c.cluster_no}｜${CLUSTER_STATUS[c.status]?.label || c.status}）` }))}
              />
            )}
            <Button icon={<ReloadOutlined />} onClick={() => { refreshAll(); if (clusterId !== null && hasPerm('PERM-FUNC-50')) loadReadiness(clusterId) }}>刷新</Button>
          </Space>
        }
      />
      {!hasPerm('PERM-FUNC-20') && !hasPerm('PERM-FUNC-25') && (
        <Alert type="info" showIcon style={{ marginBottom: 9 }} message="当前角色缺少集群详情/任务台账读取牌(PERM-FUNC-20/25),本屏仅可见集群选择与预检(如持牌)。" />
      )}

      {cluster && (
        <Row gutter={9} style={{ marginTop: 12 }}>
          {/* ---------------- 主区:任务附着 + 步进器 + 日志 ---------------- */}
          <Col xs={24} xl={ctx ? 24 : 15}>
            <Card
              size="small"
              style={{ marginBottom: 9 }}
              title={
                <Space wrap size={6}>
                  {/* 嵌入态:名称/编号/生命周期状态归台级徽章条,此处不重播(9.10.1);执行位置锁仍是本区间的判据,保留 */}
                  {!ctx && <span>{cluster.name}（{cluster.cluster_no}）</span>}
                  {!ctx && pill(CLUSTER_STATUS, cluster.status)}
                  {/* 进入即锁链路徽章(§9.9.6 / INV-32):任务行权威;无任务时说明落定时机 */}
                  {attached?.execution_path ? (
                    <Tag color={attached.execution_path === 'REMOTE_EXECUTOR' ? 'purple' : 'blue'}>执行位置：{EXEC_PATH[attached.execution_path] || attached.execution_path}（已锁定，INV-32）</Tag>
                  ) : (
                    <Tag>执行位置：派发时按集群登记事实落定，此后不可变更（INV-32）</Tag>
                  )}
                </Space>
              }
            >
              {/* el02 templateRef 建成后只读(INV-49),不给改;嵌入态由徽章条「来源模板」呈现,此处不重复 */}
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                {!ctx && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    来源模板:{cluster.template_ref ? `${cluster.template_ref}（建成后只读，INV-49，不回写模板）` : '未使用模板'}
                  </Typography.Text>
                )}

                {/* el04 预检按钮 + el05 结论条:即时提示、不拦截(§9.9.6) */}
                {hasPerm('PERM-FUNC-50') && (
                  <Space wrap>
                    <Button size="small" icon={<SafetyCertificateOutlined />} loading={readinessLoading} onClick={() => loadReadiness(cluster.id)}>
                      完备性预检（不拦截）
                    </Button>
                    {readiness && (
                      <Alert
                        style={{ flex: 1, minWidth: 320 }}
                        type={readiness.all_pass ? 'success' : 'warning'}
                        showIcon
                        message={readiness.all_pass ? '交付前置全部通过(即时提示,不拦截提交)' : '存在缺项(即时提示,不拦截;缺项如实列出)'}
                        description={
                          <Space direction="vertical" size={2}>
                            {(readiness.checks || []).map((c) => (
                              <span key={c.id}>
                                {c.pass ? '✓' : '✗'} {c.title}
                                {c.note ? `：${c.note}` : ''}
                              </span>
                            ))}
                            {readiness.caliber_note && <span style={{ opacity: 0.75 }}>{readiness.caliber_note}</span>}
                          </Space>
                        }
                      />
                    )}
                  </Space>
                )}

                {/* 创建区:仅在无在途任务且集群 DRAFT 时渲染;发起类按钮 gate = PERM-FUNC-03(el01 主屏口径) */}
                {!attachedLive && cluster.status === 'DRAFT' && hasPerm('PERM-FUNC-03') && (
                  <Space wrap align="end">
                    {/* el03 推进模式:只在任务创建前可选,缺省 STEP(REF-17);创建后转任务行只读徽章(INV-50) */}
                    <Select style={{ width: 220 }} value={stepModeIntent} options={dict.optionsOf('CLC_STEP_MODE')} onChange={setStepModeIntent} />
                    <Button type="primary" icon={<CaretRightOutlined />} loading={creating} onClick={startInstall}>
                      发起安装
                    </Button>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      下一步将创建的任务:安装({dict.labelOf('CLC_STEP_MODE', stepModeIntent)}={stepModeIntent === 'STEP' ? '每步停在人工确认点' : '步骤展开后由执行内核一次跑完'};向导入口缺省 {dict.labelOf('CLC_STEP_MODE', 'STEP')},REF-17);七步清单由后端 REP-12 在创建后给出,界面不预置。
                    </Typography.Text>
                  </Space>
                )}
                {!attachedLive && cluster.status === 'DRAFT' && !hasPerm('PERM-FUNC-03') && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>当前角色无安装派发牌(PERM-FUNC-03),不渲染发起类按钮。</Typography.Text>
                )}
                {cluster.status === 'INSTALL_FAILED' && (
                  <Alert type="warning" showIcon message="集群处于「安装失败」:请在下方步骤表失败行使用「重试本步」——将新建续跑任务(INV-31/R-17),原任务行永不改写。" />
                )}
              </Space>
            </Card>

            {/* el06 步进器:行完全来自 REP-12;SKIPPED/CANCELLED、STALLED/FAILED 分列渲染(STEP_STATUS 映射既分列) */}
            {hasPerm('PERM-FUNC-34') ? (
              <Card
                size="small"
                style={{ marginBottom: 9 }}
                title={
                  attached ? (
                    <Space wrap size={6}>
                      <span>步进器 {attached.task_no}</span>
                      <Tag>{TASK_KIND[attached.task_kind] || attached.task_kind}</Tag>
                      {pill(TASK_STATUS, attached.task_status)}
                      <Tag color="geekblue">
                        推进模式：{attached.step_mode ? dict.labelOf('CLC_STEP_MODE', attached.step_mode) : '未回吐'}（创建时落定，只读，INV-50）
                      </Tag>
                      {steps.length > 0 && (
                        <Tag>{`进度 ${concluded}/${steps.length}（分母=REP-12 行数，不写死七步）`}</Tag>
                      )}
                    </Space>
                  ) : (
                    <span>步进器（未附着任务）</span>
                  )
                }
                extra={
                  attached && !attachedLive && hasPerm('PERM-FUNC-33') && (
                    <Button size="small" onClick={() => { setCancelOpen(true); setCancelReason('') }}>任务详情/日志见下</Button>
                  )
                }
              >
                {!attached && <Empty description="该集群尚无任务;发起安装后由后端创建任务并给出步骤清单(REP-12)" />}
                {attached && (
                  <Space direction="vertical" size={6} style={{ width: '100%' }}>
                    {attached.remote_task_ref && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        远端任务引用:{attached.remote_task_ref}（REMOTE 链路 INV-46;回放定位用,本域不镜像远端状态）
                      </Typography.Text>
                    )}
                    {attached.operation_reason && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        派发事由留痕:{attached.operation_reason}（R-24,随任务头只读）
                      </Typography.Text>
                    )}
                    {attached.task_status === 'STALLED' && (
                      <Alert
                        type="warning"
                        showIcon
                        message="失联挂起(STALLED)与失败(FAILED)分列:执行机失联导致挂起,禁止静默续跑(INV-32)。"
                        action={
                          <Typography.Link onClick={() => navigate('/clc/cluster')}>显式换机：去集群台账改登记后重试（FUNC-02/04）</Typography.Link>
                        }
                      />
                    )}
                    {stepsError && <Alert type="error" showIcon message={stepsError} action={<Button size="small" onClick={loadSteps}>重试</Button>} />}
                    <Table
                      rowKey="step_index"
                      size="small"
                      columns={columns}
                      dataSource={steps}
                      loading={attachedLive && steps.length === 0}
                      pagination={false}
                      scroll={{ x: 1350 }}
                    />
                    {/* el10 取消入口说明:取消 ≠ 回滚,只对 QUEUED/RUNNING 的在途任务开放(挂接既有 FUNC-33) */}
                    {attachedLive && hasPerm('PERM-FUNC-33') && (
                      <Button size="small" danger icon={<ThunderboltOutlined />} onClick={() => setCancelOpen(true)}>
                        取消在途任务（取消 ≠ 回滚,R-15）
                      </Button>
                    )}
                  </Space>
                )}
              </Card>
            ) : (
              <Alert type="info" showIcon style={{ marginBottom: 9 }} message="当前角色无步骤查看牌(PERM-FUNC-34),步进器与行内动作不渲染(行权威=REP-12)。" />
            )}

            {/* 日志两腿按执行位置二选一、不并存(REF-16)：
                LOCAL = el10 复用 TaskDetail 导出的 TaskLogPanel(游标增量 + SSE,禁整表拉取,INV-43;上滑暂停跟随);
                REMOTE_EXECUTOR = el18 RemoteExecLogPanel(FUNC-51 代理直展 O-IGN 的 ezctl 逐行原文,2~3 秒游标短轮询,
                响应即弃不落本域;不可达/无引用 ⇒ 显式说明 + 跳转兜底,不留空面板、不拿上一轮行冒充现状)。 */}
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              {attached &&
                (attached.execution_path === 'REMOTE_EXECUTOR' ? (
                  hasPerm('PERM-FUNC-51') ? (
                    <RemoteExecLogPanel
                      key={attached.task_no}
                      remoteTaskRef={attached.remote_task_ref}
                      live={attachedLive}
                    />
                  ) : (
                    <Alert
                      type="info"
                      showIcon
                      message="当前角色无远程执行日志牌(PERM-FUNC-51),el18 面板不渲染(行权威=REP-19,内容在 O-IGN)。"
                    />
                  )
                ) : (
                  hasPerm('PERM-FUNC-35') && (
                    <TaskLogPanel key={attached.task_no} task={attached} />
                  )
                ))}
            </Space>
          </Col>

          {/* ---------------- 右侧页签组(el11) ----------------
              嵌入态不渲染:「配置」「生命周期」两块上移集群台同级页签,「原文只读」(el13)按 §9.10.1 / Q-117
              留在本「安装」页签、由宿主屏装配在步进与日志之下;主区因此放宽为整行,
              独立形态(旧深链兼容一版)保持左右分栏不变。 */}
          {!ctx && (
            <Col xs={24} xl={9}>
              <Card size="small" title="配置 / 生命周期台 / 原文只读">
                <Tabs items={tabItems} destroyOnHidden />
              </Card>
            </Col>
          )}
        </Row>
      )}
      {!ctx && !cluster && clusters.length === 0 && (
        <Alert type="info" showIcon style={{ marginTop: 12 }} message="暂无非终态集群:请先在集群台账建档(FUNC-01),回到本屏选择。" />
      )}

      {/* el09 跳步弹层:后果明示 + 事由必填(R-24/INV-37 仅未执行可跳) */}
      <Modal
        title={`跳过此步 → 第 ${skipTarget?.step_index ?? ''} 步 ${skipTarget?.step_name ?? ''}`}
        open={!!skipTarget}
        okText="确认跳步（写入留痕）"
        okButtonProps={{ disabled: !skipReason.trim(), danger: true }}
        confirmLoading={working}
        onOk={async () => {
          if (!attached || !skipTarget) return
          setWorking(true)
          try {
            const res = await wizardApi.stepSkip(attached.task_no, { step_index: skipTarget.step_index, reason: skipReason.trim() })
            const verdict = typeof res?.verdict === 'string' ? res.verdict : '人工跳步(FUNC-45/R-24)'
            message.success(`第 ${skipTarget.step_index} 步已跳过(判定:${verdict};事由已写入任务头留痕)`)
            setSkipTarget(null)
            setSkipReason('')
            refreshAll()
          } catch {
            /* 拦截器已直出后端中文 errorMsg */
          } finally {
            setWorking(false)
          }
        }}
        onCancel={() => setSkipTarget(null)}
        width={540}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="后果明示(R-24):跳过后该步在本任务内永不执行,后续步骤若依赖其产物将如实失败;判定记「人工跳步」,事由累加写入任务头 operationReason 只读留痕。仅未执行(PENDING)步可跳,已执行/终态行不可跳(INV-37/INV-41)。"
        />
        <div>
          <Typography.Text strong>跳步事由(必填)</Typography.Text>
          <Input.TextArea rows={3} value={skipReason} onChange={(e) => setSkipReason(e.target.value)} placeholder="如:该组件已由既有环境提供,本集群无需重复安装" />
        </div>
      </Modal>

      {/* FUNC-33 取消:复用既有端点,取消 ≠ 回滚(R-15),原因必填 */}
      <Modal
        title={`取消在途任务 → ${attached?.task_no ?? ''}`}
        open={cancelOpen}
        okText="确认取消任务"
        okButtonProps={{ danger: true, disabled: !cancelReason.trim(), loading: working }}
        onOk={async () => {
          if (!attached) return
          setWorking(true)
          try {
            await taskApi.cancel(attached.task_no, { reason: cancelReason.trim() })
            message.success('任务已取消(取消不是回滚;未执行步骤行按 CANCELLED 分列展示)')
            setCancelOpen(false)
            setCancelReason('')
            refreshAll()
          } catch {
            /* 拦截器已提示 */
          } finally {
            setWorking(false)
          }
        }}
        onCancel={() => setCancelOpen(false)}
        width={520}
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message="取消 ≠ 回滚(R-15):已执行步骤的效果由交付工具幂等性承担;原因写入审计(R-14)。终态任务不可取消(INV-29)。" />
        <Form layout="vertical">
          <Form.Item label="取消原因(必填)" required>
            <Input.TextArea rows={3} value={cancelReason} onChange={(e) => setCancelReason(e.target.value)} placeholder="如:变更窗口调整,暂停本次交付" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
