// 集群域 · 交付任务详情抽屉(MU frmClcOrch「编排操作台」主从三段:主信息 + el18 步骤只读从表 + el19 日志面板)。
// 裁定二「后端权威、前端只渲染」:步骤清单/状态/游标全部来自 GET /steps 与 GET /logs(/stream),
// 前端不内置安装步骤定义、不回写步骤状态。
// 日志两腿按执行位置二选一、不并存(REF-16,v1.3):LOCAL 腿 = TaskLogPanel(REP-13);
// REMOTE_EXECUTOR 腿 = RemoteExecLogPanel(REP-19,FUNC-51 代理直展 O-IGN 的 ezctl 逐行原文)。
import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Space, Table, Tag, Typography, message, theme } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PauseOutlined, PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  openTaskLogStream,
  taskApi,
  wizardApi,
  type ClcRemoteExecLogLine,
  type ClcTask,
  type ClcTaskLogLine,
  type ClcTaskStep,
} from '../../api/clc'
import { EXEC_PATH, LOG_LEVEL_TONE, STEP_STATUS_TONE, TASK_FAIL_REASON, TASK_KIND, TASK_STATUS, pill, withDictLabels } from './common'
import { useDictionaries } from '../../hooks/useDictionaries'
import { usePermission } from '../../hooks/usePermission'

const LOG_FETCH_LIMIT = 500
/** 非终局任务状态(与服务端 SSE 终局判定同口径):此二态下仍可能有新行 */
const LIVE_TASK_STATUSES = ['QUEUED', 'RUNNING']

function StepFlowTable({ taskNo }: { taskNo: string }) {
  // 步骤状态 / 判定依据的中文名取 /api/meta/dictionaries（M1 只读投影），界面不留副本（DEF-09）。
  const dict = useDictionaries()
  const [steps, setSteps] = useState<ClcTaskStep[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = () => {
    setLoading(true)
    setError('')
    taskApi
      .steps(taskNo)
      .then((res) => setSteps(res.list || []))
      .catch((e: Error) => setError(e.message || '步骤加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskNo])

  const stepStatusMap = withDictLabels(STEP_STATUS_TONE, dict.itemsOf('CLC_STEP_STATUS'))

  const columns: ColumnsType<ClcTaskStep> = [
    { title: '序号', dataIndex: 'step_index', key: 'step_index', width: 60 },
    {
      title: '步骤编码与名称',
      key: 'step',
      width: 220,
      render: (_, r) => `${r.step_code} ${r.step_name}`.trim(),
    },
    { title: '状态', dataIndex: 'step_status', key: 'step_status', width: 140, render: (v: string) => pill(stepStatusMap, v) },
    {
      title: '判定依据',
      dataIndex: 'verdict_source',
      key: 'verdict_source',
      width: 140,
      render: (v: string | null, r) => {
        if (!v && (r.exit_code === null || r.exit_code === undefined)) return '-'
        const label = v ? dict.labelOf('CLC_STEP_VERDICT', v) : '-'
        return r.exit_code !== null && r.exit_code !== undefined ? `${label}(${r.exit_code})` : label
      },
    },
    { title: '命令摘要(已脱敏)', dataIndex: 'command_summary', key: 'command_summary', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '起止时间', key: 'time', width: 250, render: (_, r) => `${r.started_at || '-'} ~ ${r.ended_at || '-'}` },
    {
      title: '日志区间',
      key: 'log_range',
      width: 120,
      render: (_, r) => (r.log_start_line === null || r.log_start_line === undefined ? '-' : `#${r.log_start_line}~#${r.log_end_line ?? '-'}`),
    },
    { title: '失败摘要', dataIndex: 'fail_detail', key: 'fail_detail', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '跳过原因', dataIndex: 'skip_reason', key: 'skip_reason', width: 150, ellipsis: true, render: (v: string | null) => v || '-' },
  ]

  return (
    <>
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 9 }} message={error} action={<Button size="small" onClick={load}>重试</Button>} />
      )}
      <Table
        rowKey="step_index"
        size="small"
        columns={columns}
        dataSource={steps}
        loading={loading}
        pagination={false}
        scroll={{ x: 1250 }}
      />
    </>
  )
}

type StreamPhase = 'loading' | 'streaming' | 'paused' | 'finished'

/**
 * REP-13 日志回放面板：游标增量 + SSE 流式追加（禁整表拉取，口径 8；上滑暂停跟随）。
 * 只服务 LOCAL 腿 —— REMOTE_EXECUTOR 腿改由下面的 RemoteExecLogPanel 代理直展（REF-16 两腿二选一不并存）。
 * 装机向导台（frmClcWizard el10）复用同一面板 —— 同一日志通道不在两处各写一遍（#69 落点，#70 同判据）。
 */
export function TaskLogPanel({ task }: { task: ClcTask }) {
  const { token } = theme.useToken()
  const [lines, setLines] = useState<ClcTaskLogLine[]>([])
  const [phase, setPhase] = useState<StreamPhase>('loading')
  const [cursor, setCursor] = useState(0)
  const [taskStatus, setTaskStatus] = useState(task.task_status)
  const cursorRef = useRef(0) // lastLineNo:只增不减,重复帧丢弃(与后端 INV-43 同口径)
  const statusRef = useRef(task.task_status)
  const aliveRef = useRef(true)
  const closeStreamRef = useRef<(() => void) | null>(null)
  const bodyRef = useRef<HTMLDivElement | null>(null)

  const setStatus = (s: string) => {
    if (s) {
      statusRef.current = s
      setTaskStatus(s)
    }
  }

  const appendRows = (rows: ClcTaskLogLine[]) => {
    const fresh: ClcTaskLogLine[] = []
    for (const row of rows) {
      const ln = Number(row.line_no)
      if (!Number.isFinite(ln) || ln <= cursorRef.current) continue
      cursorRef.current = ln
      fresh.push(row)
    }
    if (fresh.length > 0) {
      setLines((prev) => [...prev, ...fresh])
      setCursor(fresh[fresh.length - 1].line_no)
    }
  }

  const closeStream = () => {
    closeStreamRef.current?.()
    closeStreamRef.current = null
  }

  // 断线/落后重入口:先 GET /logs?offset=游标 补齐历史,再以最新游标开 SSE
  const backfillAndStream = () => {
    closeStream()
    setPhase('loading')
    void (async () => {
      try {
        for (;;) {
          const res = await taskApi.logs(task.task_no, { offset: cursorRef.current, limit: LOG_FETCH_LIMIT })
          if (!aliveRef.current) return
          appendRows(res.list || [])
          setStatus(res.task_status)
          if ((res.list || []).length < LOG_FETCH_LIMIT) break
        }
      } catch {
        if (aliveRef.current) setPhase('paused') // 中文提示已由统一拦截器直出
        return
      }
      if (!aliveRef.current) return
      setPhase('streaming')
      closeStreamRef.current = openTaskLogStream(task.task_no, cursorRef.current, {
        onLine: (row) => appendRows([row]),
        onDone: (p) => {
          if (!aliveRef.current) return
          setStatus(p.task_status)
          setPhase('finished')
        },
        onError: (msg) => {
          if (!aliveRef.current) return
          message.error(msg)
          setPhase('paused')
        },
        onClose: () => {
          if (!aliveRef.current) return
          if (!LIVE_TASK_STATUSES.includes(statusRef.current)) {
            setPhase('finished')
            return
          }
          backfillAndStream() // 非终局断流:自动按游标补齐后续播
        },
      })
    })()
  }

  const pause = () => {
    closeStream()
    setPhase('paused')
  }

  const replay = () => {
    closeStream()
    cursorRef.current = 0
    setLines([])
    setCursor(0)
    backfillAndStream()
  }

  useEffect(() => {
    aliveRef.current = true
    backfillAndStream()
    return () => {
      aliveRef.current = false
      closeStream()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.task_no])

  useEffect(() => {
    const el = bodyRef.current
    if (el && el.scrollHeight - el.scrollTop - el.clientHeight < 80) el.scrollTop = el.scrollHeight
  }, [lines])

  const levelColor = (level: string) => {
    const tone = LOG_LEVEL_TONE[level]
    if (tone === 'error') return token.colorError
    if (tone === 'warn') return token.colorWarning
    return token.colorText
  }

  const phaseTag =
    phase === 'loading' ? <Tag color="processing">游标补齐中</Tag>
    : phase === 'streaming' ? <Tag color="processing">流式回放中</Tag>
    : phase === 'paused' ? <Tag>已暂停</Tag>
    : <Tag color="default">回放结束</Tag>

  return (
    <Card
      size="small"
      title={<Space>日志回放面板(游标增量,只读){phaseTag}</Space>}
      extra={
        <Space>
          <span style={{ fontSize: 12, color: token.colorTextSecondary }}>游标 #{cursor}</span>
          {phase === 'streaming' || phase === 'loading' ? (
            <Button size="small" icon={<PauseOutlined />} onClick={pause}>暂停</Button>
          ) : (
            <Button size="small" type="primary" icon={<PlayCircleOutlined />} onClick={backfillAndStream}>续播</Button>
          )}
          <Button size="small" icon={<ReloadOutlined />} onClick={replay} disabled={phase === 'loading' || phase === 'streaming'}>从头回放</Button>
        </Space>
      }
    >
      <div
        ref={bodyRef}
        style={{
          height: 320,
          overflow: 'auto',
          background: token.colorBgContainer,
          border: `1px solid ${token.colorBorderSecondary}`,
          borderRadius: token.borderRadiusSM,
          padding: '4px 9px',
          fontFamily: 'monospace',
          fontSize: 12,
          lineHeight: 1.7,
        }}
      >
        {lines.length === 0 && phase !== 'loading' && (
          <div style={{ color: token.colorTextTertiary }}>暂无日志(任务尚未开跑,或已按保留期清理)</div>
        )}
        {lines.map((l) => (
          <div key={l.line_no} style={{ color: levelColor(l.log_level) }}>
            <span style={{ opacity: 0.65 }}>{l.logged_at}</span> #{l.line_no}
            {l.step_index !== null && l.step_index !== undefined ? ` S${l.step_index}` : ''} {l.content}
          </div>
        ))}
      </div>
    </Card>
  )
}

const REMOTE_LOG_POLL_MS = 2500
const REMOTE_LOG_PAGE_LIMIT = 200

/**
 * REP-19 远程执行日志代理直展面板（v1.3 / Q-125，frmClcWizard.el18 的主载体）。
 * 与上面的 TaskLogPanel（REP-13 本地腿）按任务执行位置二选一、不并存（REF-16）：
 * REMOTE_EXECUTOR 腿的原文不在本域，面板每 2~3 秒带游标向 O-IGN 代理取增量，
 * 响应即弃 —— 行不写本地状态、不做缓存冒充，刷新即从当前游标重取（R-25 日志腿 / REF-12）。
 * 列只渲染对端真给的 seq / logged_at / direction / masked / log_line，不造「级别」「所属步骤序号」
 * （口径 9：代理显示即对端事实）；不可达或无远端引用 ⇒ 显式说明 + 跳转兜底，不留空面板。
 */
export interface RemoteExecLogPanelProps {
  /** 任务头 remoteTaskRef（INV-46）；缺位即「远程腿尚未落地」档，显式说明 + 跳转兜底 */
  remoteTaskRef?: string | null
  /** 任务是否仍在产出（QUEUED/RUNNING）；终态任务一次性回放至末尾后停止轮询（REP-19 交付形态） */
  live: boolean
}

type RemoteLogPhase = 'polling' | 'paused' | 'stale' | 'finished' | 'no_ref'

export function RemoteExecLogPanel({ remoteTaskRef, live }: RemoteExecLogPanelProps) {
  const { token } = theme.useToken()
  const ref = (remoteTaskRef || '').trim()
  const [lines, setLines] = useState<ClcRemoteExecLogLine[]>([])
  const [cursor, setCursor] = useState(0)
  const [phase, setPhase] = useState<RemoteLogPhase>('polling')
  const [notice, setNotice] = useState('')
  const [jumpEntry, setJumpEntry] = useState('')

  const cursorRef = useRef(0) // 上一轮取到的最大 seq：只增不减，重复行丢弃
  const aliveRef = useRef(true)
  const liveRef = useRef(live)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const runningRef = useRef(false)
  const pausedRef = useRef(false)
  const haltRef = useRef(false) // 无远端引用档：不再安排下一轮
  const bodyRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    liveRef.current = live
  }, [live])

  const appendRows = (rows: ClcRemoteExecLogLine[]) => {
    const fresh: ClcRemoteExecLogLine[] = []
    for (const row of rows) {
      const sq = Number(row.seq)
      if (!Number.isFinite(sq) || sq <= cursorRef.current) continue
      cursorRef.current = sq
      fresh.push(row)
    }
    if (fresh.length > 0) {
      setLines((prev) => [...prev, ...fresh])
      setCursor(cursorRef.current)
    }
  }

  // 单轮：取回增量；对端被 limit 封顶（truncated）就连取到本轮边界，再回到 2~3 秒节拍
  const pollOnce = useCallback(async () => {
    if (!ref || runningRef.current || !aliveRef.current) return
    runningRef.current = true
    try {
      for (;;) {
        const res = await wizardApi.execLog(ref, { since_seq: cursorRef.current, limit: REMOTE_LOG_PAGE_LIMIT })
        if (!aliveRef.current) return
        setJumpEntry(res.jump_entry || '')
        if (res.leg_available === false) {
          haltRef.current = true
          setNotice(res.notice || '该任务没有远端任务引用，远程日志腿暂不可读')
          setPhase('no_ref')
          return
        }
        if (res.peer_unreachable) {
          // 失败不清空已回放行，但必须标注「不代表现状」（与字段级代理视图 §9.9.7④ 同口径）
          setNotice(res.unreachable_reason || res.notice || '交付域不可达')
          setPhase('stale')
          return
        }
        appendRows(res.lines || [])
        const next = Number(res.next_seq ?? cursorRef.current)
        if (Number.isFinite(next) && next > cursorRef.current) {
          cursorRef.current = next
          setCursor(next)
        }
        setNotice('')
        setPhase(liveRef.current ? 'polling' : 'finished')
        if (!res.truncated) break
      }
    } catch {
      // 统一拦截器已直出后端中文 errorMsg；本轮失败只标注陈旧，不动已回放行
      if (aliveRef.current) setPhase((p) => (p === 'finished' ? 'finished' : 'stale'))
    } finally {
      runningRef.current = false
      if (aliveRef.current && liveRef.current && !pausedRef.current && !haltRef.current) {
        timerRef.current = setTimeout(() => void pollOnce(), REMOTE_LOG_POLL_MS)
      }
    }
  }, [ref])

  const stopTimer = () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }

  const reset = () => {
    stopTimer()
    haltRef.current = false
    cursorRef.current = 0
    setLines([])
    setCursor(0)
    setNotice('')
  }

  useEffect(() => {
    aliveRef.current = true
    pausedRef.current = false
    reset()
    if (!ref) {
      haltRef.current = true
      setPhase('no_ref')
      setNotice('远程日志暂不可读：该任务还没有远端任务引用（远程任务尚未受理，或本任务走 LOCAL 链路 —— LOCAL 腿的日志由上方面板回放）')
      return () => {
        aliveRef.current = false
        stopTimer()
      }
    }
    setPhase('polling')
    void pollOnce()
    return () => {
      aliveRef.current = false
      stopTimer()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref])

  // 任务由在途转终态：补一轮把末尾取完，之后不再安排下一轮
  useEffect(() => {
    if (live || !ref || haltRef.current) return
    stopTimer()
    void pollOnce()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, ref])

  useEffect(() => {
    const el = bodyRef.current
    if (el && el.scrollHeight - el.scrollTop - el.clientHeight < 80) el.scrollTop = el.scrollHeight
  }, [lines])

  const pause = () => {
    pausedRef.current = true
    stopTimer()
    setPhase('paused')
  }

  const resume = () => {
    pausedRef.current = false
    haltRef.current = false
    setPhase(liveRef.current ? 'polling' : 'finished')
    void pollOnce()
  }

  const replay = () => {
    pausedRef.current = false
    stopTimer()
    reset()
    setPhase('polling')
    void pollOnce()
  }

  const phaseTag =
    phase === 'polling' ? <Tag color="purple">游标轮询中</Tag>
    : phase === 'stale' ? <Tag color="warning">本轮读取失败</Tag>
    : phase === 'paused' ? <Tag>已暂停</Tag>
    : phase === 'no_ref' ? <Tag>无远端引用</Tag>
    : <Tag color="default">回放结束</Tag>

  return (
    <Card
      size="small"
      title={
        <Space>
          远程执行日志（点火中心 ezctl 逐行原文 · 代理直展）
          {phaseTag}
          <Typography.Text style={{ fontSize: 12, color: token.colorTextSecondary }}>
            当前腿：REMOTE_EXECUTOR · 内容权威在 O-IGN（本域不落副本、不二次脱敏，R-25 / INV-08）
          </Typography.Text>
        </Space>
      }
      extra={
        <Space>
          <span style={{ fontSize: 12, color: token.colorTextSecondary }}>游标 #{cursor}</span>
          {phase === 'polling' ? (
            <Button size="small" icon={<PauseOutlined />} onClick={pause}>暂停</Button>
          ) : (
            <Button size="small" type="primary" icon={<PlayCircleOutlined />} onClick={resume} disabled={!ref}>续播</Button>
          )}
          <Button size="small" icon={<ReloadOutlined />} onClick={replay} disabled={!ref}>从头回放</Button>
        </Space>
      }
    >
      {phase === 'stale' && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 9 }}
          message={`本轮代理读取失败：${notice || '交付域不可达'}`}
          description={
            <Space direction="vertical" size={2}>
              <span>下方行截至游标 #{cursor}，不代表现状（不缓存冒充，REF-12）。</span>
              {jumpEntry && (
                <Typography.Link href={jumpEntry} target="_blank" rel="noreferrer">跳转 O-IGN 交付域现场查看</Typography.Link>
              )}
            </Space>
          }
        />
      )}
      {phase === 'no_ref' && (
        <Alert type="info" showIcon style={{ marginBottom: 9 }} message={notice} description="受理成功后本面板自动开始增量回放。" />
      )}
      <div
        ref={bodyRef}
        style={{
          height: 320,
          overflow: 'auto',
          background: token.colorBgContainer,
          border: `1px solid ${token.colorBorderSecondary}`,
          borderRadius: token.borderRadiusSM,
          padding: '4px 9px',
          fontFamily: 'monospace',
          fontSize: 12,
          lineHeight: 1.7,
        }}
      >
        {lines.length === 0 && phase !== 'polling' && phase !== 'stale' && (
          <div style={{ color: token.colorTextTertiary }}>暂无远程执行日志行</div>
        )}
        {lines.map((l) => (
          <div key={l.seq} style={{ color: l.masked ? token.colorWarning : token.colorText }}>
            <span style={{ opacity: 0.65 }}>{l.logged_at || '-'}</span> #{l.seq}
            {l.direction ? ` ${l.direction}` : ''} {l.log_line}
          </div>
        ))}
      </div>
    </Card>
  )
}

export interface TaskDetailDrawerProps {
  task: ClcTask
  /** 行内入口:查看步骤(act-FUNC-34)/查看日志(act-FUNC-35 本地腿、act-FUNC-51 远程腿)分别定位对应区段 */
  focus: 'steps' | 'logs'
  onClose: () => void
}

export default function TaskDetailDrawer({ task, focus, onClose }: TaskDetailDrawerProps) {
  const hasPerm = usePermission()
  const stepsRef = useRef<HTMLDivElement | null>(null)
  const logsRef = useRef<HTMLDivElement | null>(null)
  // REF-16 两腿按执行位置二选一、不并存：LOCAL 腿 = REP-13（本域 clc_task_log 游标回放）；
  // REMOTE_EXECUTOR 腿 = REP-19（FUNC-51 代理直展对端 ezctl 逐行原文，响应即弃）。
  const remoteLeg = task.execution_path === 'REMOTE_EXECUTOR'

  useEffect(() => {
    const target = focus === 'steps' ? stepsRef.current : logsRef.current
    target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [focus])

  return (
    <Drawer title={`任务详情 ${task.task_no}`} width={960} open onClose={onClose}>
      <Descriptions column={3} size="small" bordered labelStyle={{ textAlign: 'right', width: 96 }}>
        <Descriptions.Item label="任务编号">{task.task_no}</Descriptions.Item>
        <Descriptions.Item label="集群">{task.cluster_name ? `${task.cluster_name}(${task.cluster_ref})` : task.cluster_ref}</Descriptions.Item>
        <Descriptions.Item label="类别">{TASK_KIND[task.task_kind] || task.task_kind}</Descriptions.Item>
        <Descriptions.Item label="状态">{pill(TASK_STATUS, task.task_status)}</Descriptions.Item>
        <Descriptions.Item label="执行位置">{EXEC_PATH[task.execution_path] || task.execution_path}</Descriptions.Item>
        <Descriptions.Item label="提交人">{task.submitted_by || '-'}</Descriptions.Item>
        <Descriptions.Item label="开始时间">{task.started_at || '-'}</Descriptions.Item>
        <Descriptions.Item label="结束时间">{task.ended_at || '-'}</Descriptions.Item>
        <Descriptions.Item label="失败原因">{task.fail_reason ? TASK_FAIL_REASON[task.fail_reason] || task.fail_reason : '-'}</Descriptions.Item>
        <Descriptions.Item label="结果摘要" span={3}>{task.result_summary || '-'}</Descriptions.Item>
      </Descriptions>
      <div ref={stepsRef}>
        <Card
          size="small"
          title="交付步骤流水线(只读)"
          style={{ marginTop: 9 }}
          extra={<span style={{ fontSize: 12 }}>步骤权威在后端(REP-12 c1~c9,不含整表原始日志)</span>}
        >
          <StepFlowTable taskNo={task.task_no} />
        </Card>
      </div>
      <div ref={logsRef} style={{ marginTop: 9 }}>
        {remoteLeg ? (
          hasPerm('PERM-FUNC-51') ? (
            <RemoteExecLogPanel remoteTaskRef={task.remote_task_ref} live={LIVE_TASK_STATUSES.includes(task.task_status)} />
          ) : (
            <Alert type="info" showIcon message="当前角色无远程日志牌(PERM-FUNC-51),远程腿面板不渲染(行权威=REP-19,在 O-IGN)。" />
          )
        ) : (
          hasPerm('PERM-FUNC-35') ? (
            <TaskLogPanel task={task} />
          ) : (
            <Alert type="info" showIcon message="当前角色无日志回放牌(PERM-FUNC-35),本地腿面板不渲染(行权威=REP-13)。" />
          )
        )}
      </div>
    </Drawer>
  )
}
