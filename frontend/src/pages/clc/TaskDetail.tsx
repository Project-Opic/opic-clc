// 集群域 · 交付任务详情抽屉(MU frmClcOrch 主从三段:主信息 + el21 步骤只读从表 + el22 日志回放面板)。
// 裁定二「后端权威、前端只渲染」:步骤清单/状态/游标全部来自 GET /steps 与 GET /logs(/stream),
// 前端不内置安装步骤定义、不回写步骤状态。
import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Space, Table, Tag, message, theme } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PauseOutlined, PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  openTaskLogStream,
  taskApi,
  type ClcTask,
  type ClcTaskLogLine,
  type ClcTaskStep,
} from '../../api/clc'
import { EXEC_PATH, LOG_LEVEL, STEP_STATUS, STEP_VERDICT, TASK_FAIL_REASON, TASK_KIND, TASK_STATUS, pill } from './common'

const LOG_FETCH_LIMIT = 500
/** 非终局任务状态(与服务端 SSE 终局判定同口径):此二态下仍可能有新行 */
const LIVE_TASK_STATUSES = ['QUEUED', 'RUNNING']

function StepFlowTable({ taskNo }: { taskNo: string }) {
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

  const columns: ColumnsType<ClcTaskStep> = [
    { title: '序号', dataIndex: 'step_index', key: 'step_index', width: 60 },
    {
      title: '步骤编码与名称',
      key: 'step',
      width: 220,
      render: (_, r) => `${r.step_code} ${r.step_name}`.trim(),
    },
    { title: '状态', dataIndex: 'step_status', key: 'step_status', width: 140, render: (v: string) => pill(STEP_STATUS, v) },
    {
      title: '判定依据',
      dataIndex: 'verdict_source',
      key: 'verdict_source',
      width: 140,
      render: (v: string | null, r) => {
        if (!v && (r.exit_code === null || r.exit_code === undefined)) return '-'
        const label = v ? STEP_VERDICT[v] || v : '-'
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

function TaskLogPanel({ task }: { task: ClcTask }) {
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
    const tone = LOG_LEVEL[level]?.tone
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
      {task.execution_path === 'REMOTE_EXECUTOR' && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 9 }}
          message="远程链路的执行侧原始日志留存于交付域(O-IGN),本面板仅回放本域登记的交付日志(REF-12)"
        />
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

export interface TaskDetailDrawerProps {
  task: ClcTask
  /** 行内入口:查看步骤(act-FUNC-34)/查看日志(act-FUNC-35)分别定位对应区段 */
  focus: 'steps' | 'logs'
  onClose: () => void
}

export default function TaskDetailDrawer({ task, focus, onClose }: TaskDetailDrawerProps) {
  const stepsRef = useRef<HTMLDivElement | null>(null)
  const logsRef = useRef<HTMLDivElement | null>(null)

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
        <TaskLogPanel task={task} />
      </div>
    </Drawer>
  )
}
