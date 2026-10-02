import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  useEdgesState,
  useNodesState,
  useReactFlow,
  MarkerType,
  type Connection,
  type Edge,
  type Node,
  type NodeProps,
  Handle,
  Position,
} from '@xyflow/react'
import { Button, Drawer, Form, Input, InputNumber, message, Select, Space, Table, Tag } from 'antd'
import { http } from '../../api/request'

/* ── DSL 类型（pkg/temporalflow 对应）────────────────────────────────────── */

type TFNodeType = 'START' | 'STEP' | 'TIMER' | 'CONDITION' | 'END'

interface TFNodeData extends Record<string, unknown> {
  label: string
  name?: string
  seconds?: number
  expr?: string
  webhook_url?: string
}

type TFNode = Node<TFNodeData>
type TFEdge = Edge<{ branch?: 'true' | 'false' }>

const NODE_TYPES: { type: TFNodeType; label: string; color: string }[] = [
  { type: 'START', label: '开始', color: '#16a34a' },
  { type: 'STEP', label: '步骤', color: '#2266e3' },
  { type: 'TIMER', label: '定时', color: '#8b5cf6' },
  { type: 'CONDITION', label: '条件', color: '#f43f5e' },
  { type: 'END', label: '结束', color: '#ef4444' },
]

const LABEL: Record<string, string> = Object.fromEntries(NODE_TYPES.map((n) => [n.type, n.label]))
const COLOR: Record<string, string> = Object.fromEntries(NODE_TYPES.map((n) => [n.type, n.color]))
const MARKER = { type: MarkerType.ArrowClosed, width: 14, height: 14 } as const

/* ── 节点渲染 ────────────────────────────────────────────────────────────── */

function TFNodeView({ data, selected, type }: NodeProps<TFNode> & { type: TFNodeType }) {
  const color = COLOR[type]
  return (
    <div
      style={{
        padding: '8px 14px',
        borderRadius: 8,
        border: `2px solid ${color}`,
        background: 'var(--ant-color-bg-container, #fff)',
        color: 'inherit',
        minWidth: 110,
        textAlign: 'center',
        boxShadow: selected ? `0 0 0 3px ${color}33` : 'none',
      }}
    >
      {type !== 'START' && <Handle type="target" position={Position.Top} />}
      <div style={{ fontWeight: 600, fontSize: 13 }}>{LABEL[type]}</div>
      {data.label && data.label !== LABEL[type] && (
        <div style={{ fontSize: 12, opacity: 0.75 }}>{data.label}</div>
      )}
      {type === 'TIMER' && data.seconds != null && (
        <div style={{ fontSize: 11, opacity: 0.6 }}>{data.seconds}s</div>
      )}
      {type === 'CONDITION' && data.expr && (
        <div style={{ fontSize: 11, fontFamily: 'monospace' }}>{data.expr}</div>
      )}
      {type !== 'END' && <Handle type="source" position={Position.Bottom} />}
      {type === 'CONDITION' && (
        <>
          <Handle id="true" type="source" position={Position.Bottom} style={{ left: '30%' }} />
          <Handle id="false" type="source" position={Position.Bottom} style={{ left: '70%' }} />
        </>
      )}
    </div>
  )
}

const nodeTypes = Object.fromEntries(
  NODE_TYPES.map((n) => [n.type, (p: NodeProps<TFNode>) => <TFNodeView {...p} type={n.type} />]),
)

/* ── 序列化 ──────────────────────────────────────────────────────────────── */

function serialize(nodes: TFNode[], edges: TFEdge[]) {
  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      type: n.type,
      name: n.data.label,
      ...(n.type === 'TIMER' ? { seconds: n.data.seconds ?? 1 } : {}),
      ...(n.type === 'CONDITION' ? { expr: n.data.expr ?? '' } : {}),
      ...(n.type === 'STEP' && n.data.webhook_url ? { webhook_url: n.data.webhook_url } : {}),
      x: Math.round(n.position.x),
      y: Math.round(n.position.y),
    })),
    edges: edges.map((e) => ({
      source: e.source,
      target: e.target,
      ...(e.data?.branch ? { source_handle: e.data.branch } : {}),
    })),
  }
}

/* ── 设计器主体 ──────────────────────────────────────────────────────────── */

function DesignerInner() {
  const { id } = useParams()
  const navigate = useNavigate()
  const defId = id ? Number(id) : null
  const isNew = !defId
  const { screenToFlowPosition } = useReactFlow()

  const [nodes, setNodes, onNodesChange] = useNodesState<TFNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<TFEdge>([])
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<number | null>(defId)
  const [sel, setSel] = useState<{ kind: 'node' | 'edge'; id: string } | null>(null)
  const counter = useRef(0)

  const load = useCallback(async () => {
    if (!defId) return
    try {
      const d = await http.get<Record<string, unknown>>(`/api/admin/temporalflow/definitions/${defId}`)
      setCode(String(d.code || ''))
      setName(String(d.name || ''))
      setDescription(String(d.description || ''))
      const g = typeof d.node_graph === 'string' ? JSON.parse(d.node_graph) : d.node_graph
      const gNodes = (g.nodes || []) as { id: string; type: TFNodeType; name?: string; x?: number; y?: number; seconds?: number; expr?: string; webhook_url?: string }[]
      const gEdges = (g.edges || []) as { source: string; target: string; source_handle?: string }[]
      setNodes(
        gNodes.map((n) => ({
          id: n.id,
          type: n.type,
          position: { x: n.x ?? 100, y: n.y ?? 100 },
          data: { label: n.name || LABEL[n.type], seconds: n.seconds, expr: n.expr, webhook_url: n.webhook_url },
        })),
      )
      setEdges(
        gEdges.map((e, i) => ({
          id: `e-${i}`,
          source: e.source,
          target: e.target,
          type: 'smoothstep',
          markerEnd: MARKER,
          data: { branch: e.source_handle as 'true' | 'false' | undefined },
          label: e.source_handle,
        })),
      )
    } catch {
      message.error('加载失败')
    }
  }, [defId])

  useEffect(() => {
    load()
  }, [load])

  const addNode = (type: TFNodeType) => {
    counter.current += 1
    const nid = `${type.toLowerCase()}-${counter.current}`
    setNodes((ns) => [
      ...ns,
      {
        id: nid,
        type,
        position: screenToFlowPosition({ x: 260 + Math.random() * 120, y: 140 + Math.random() * 160 }),
        data: { label: LABEL[type], ...(type === 'TIMER' ? { seconds: 30 } : {}), ...(type === 'CONDITION' ? { expr: 'amount > 100' } : {}) },
      },
    ])
  }

  const onConnect = useCallback(
    (c: Connection) =>
      setEdges((es) =>
        addEdge(
          {
            id: `e-${c.source}-${c.target}-${Date.now()}`,
            source: c.source!,
            target: c.target!,
            sourceHandle: c.sourceHandle ?? null,
            targetHandle: c.targetHandle ?? null,
            type: 'smoothstep',
            markerEnd: MARKER,
            data: { branch: c.sourceHandle === 'true' || c.sourceHandle === 'false' ? c.sourceHandle : undefined },
            label: c.sourceHandle === 'true' || c.sourceHandle === 'false' ? c.sourceHandle : undefined,
            style: c.sourceHandle === 'true' ? { stroke: '#16a34a' } : c.sourceHandle === 'false' ? { stroke: '#ef4444' } : undefined,
          } as TFEdge,
          es,
        ),
      ),
    [setEdges],
  )

  const updateSel = (patch: Record<string, unknown>) => {
    if (sel?.kind === 'node') {
      setNodes((ns) => ns.map((n) => (n.id === sel.id ? { ...n, data: { ...n.data, ...patch } } : n)))
    }
  }

  const save = async (thenPublish?: boolean) => {
    if (!code || !name) {
      message.warning('code 与名称必填')
      return
    }
    setSaving(true)
    try {
      const body = { code, name, description, node_graph: serialize(nodes, edges) }
      let rid = savedId ?? defId
      if (rid) {
        await http.put(`/api/admin/temporalflow/definitions/${rid}`, body as Record<string, unknown>)
      } else {
        const r = await http.post<{ id: number }>('/api/admin/temporalflow/definitions', body)
        rid = r.id
        setSavedId(rid)
      }
      if (thenPublish) {
        await http.post(`/api/admin/temporalflow/definitions/${rid}/publish`)
        message.success('已保存并发布')
      } else {
        message.success('保存成功')
      }
      if (!defId && rid) navigate(`/admin/temporalflow/designer/${rid}`, { replace: true })
    } catch (e) {
      message.error(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const [runVars, setRunVars] = useState('{"amount": 500}')
  const [runOpen, setRunOpen] = useState(false)
  const doRun = async () => {
    const rid = savedId ?? defId
    if (!rid) return
    try {
      let vars: Record<string, unknown> = {}
      try {
        vars = JSON.parse(runVars)
      } catch {
        message.warning('变量不是合法 JSON')
        return
      }
      await http.post(`/api/admin/temporalflow/definitions/${rid}/run`, { vars })
      message.success('试运行已启动（Temporal 命名空间 opic）')
      setRunOpen(false)
    } catch (e) {
      message.error(e instanceof Error ? e.message : '试运行失败')
    }
  }

  const selNode = sel?.kind === 'node' ? nodes.find((n) => n.id === sel.id) : null
  const selEdge = sel?.kind === 'edge' ? edges.find((e) => e.id === sel.id) : null

  return (
    <div style={{ height: 'calc(100vh - 120px)', display: 'flex', flexDirection: 'column' }}>
      <Space style={{ padding: '8px 12px' }} wrap>
        <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="编码 (code)" style={{ width: 180 }} disabled={!!defId} />
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="名称" style={{ width: 200 }} />
        <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="描述" style={{ width: 240 }} />
        <Button type="primary" loading={saving} onClick={() => save(false)}>保存</Button>
        <Button loading={saving} onClick={() => save(true)}>保存并发布</Button>
        <Button onClick={() => setRunOpen(true)} disabled={!(savedId ?? defId)}>试运行</Button>
        <Button onClick={() => navigate('/admin/temporalflow')}>返回列表</Button>
      </Space>
      <div style={{ flex: 1, display: 'flex' }}>
        {/* 左：节点面板 */}
        <div style={{ width: 130, borderRight: '1px solid #ddd', padding: 8 }}>
          {NODE_TYPES.map((n) => (
            <div
              key={n.type}
              draggable
              onDragStart={(e) => e.dataTransfer.setData('application/opic-tf-node', n.type)}
              style={{ margin: '6px 0', padding: '6px 10px', border: `2px solid ${n.color}`, borderRadius: 6, cursor: 'grab', textAlign: 'center', fontWeight: 600 }}
            >
              {n.label}
            </div>
          ))}
        </div>
        {/* 中：画布 */}
        <div style={{ flex: 1 }}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodeTypes={nodeTypes}
            onNodeClick={(_, n) => setSel({ kind: 'node', id: n.id })}
            onEdgeClick={(_, e) => setSel({ kind: 'edge', id: e.id })}
            onPaneClick={() => setSel(null)}
            onDragOver={(e) => {
              e.preventDefault()
              e.dataTransfer.dropEffect = 'move'
            }}
            onDrop={(e) => {
              e.preventDefault()
              const type = e.dataTransfer.getData('application/opic-tf-node') as TFNodeType
              if (type) addNode(type)
            }}
            fitView
          >
            <Background variant={BackgroundVariant.Dots} gap={18} />
            <Controls />
            <MiniMap pannable />
          </ReactFlow>
        </div>
        {/* 右：属性面板 */}
        <div style={{ width: 300, borderLeft: '1px solid #ddd', padding: 12 }}>
          {selNode && (
            <Form layout="vertical" size="small">
              <div style={{ fontWeight: 600, marginBottom: 8 }}>{LABEL[selNode.type as TFNodeType]} 属性</div>
              <Form.Item label="名称">
                <Input value={selNode.data.label} onChange={(e) => updateSel({ label: e.target.value })} />
              </Form.Item>
              {(selNode.type as TFNodeType) === 'STEP' && (
                <Form.Item label="Webhook URL（可选，执行时 POST）">
                  <Input value={selNode.data.webhook_url || ''} onChange={(e) => updateSel({ webhook_url: e.target.value })} />
                </Form.Item>
              )}
              {(selNode.type as TFNodeType) === 'TIMER' && (
                <Form.Item label="等待秒数">
                  <InputNumber value={selNode.data.seconds ?? 30} min={0} step={1} onChange={(v) => updateSel({ seconds: v ?? 30 })} style={{ width: '100%' }} />
                </Form.Item>
              )}
              {(selNode.type as TFNodeType) === 'CONDITION' && (
                <Form.Item label="条件表达式（对启动变量求值）">
                  <Input.TextArea rows={2} value={selNode.data.expr || ''} onChange={(e) => updateSel({ expr: e.target.value })} />
                </Form.Item>
              )}
              {(selNode.type as TFNodeType) === 'CONDITION' && (
                <div style={{ fontSize: 12, color: '#888' }}>
                  出边连线：从 <b>true</b>（绿）/ <b>false</b>（红）锚点连出
                </div>
              )}
            </Form>
          )}
          {selEdge && selEdge.data?.branch && (
            <div style={{ fontSize: 13 }}>
              条件分支：<Tag color={selEdge.data.branch === 'true' ? 'green' : 'red'}>{selEdge.data.branch}</Tag>
              <div style={{ color: '#888', marginTop: 6 }}>改分支 = 删除连线重连对应锚点</div>
            </div>
          )}
          {!selNode && !selEdge && <div style={{ color: '#999', fontSize: 13 }}>选中节点/连线后编辑属性</div>}
        </div>
      </div>
      <Drawer title="试运行" open={runOpen} onClose={() => setRunOpen(false)} width={420}
        extra={<Button type="primary" onClick={doRun}>启动</Button>}>
        <p>以当前已保存图启动 Temporal 解释器工作流（命名空间 opic，TaskQueue techbase）。</p>
        <Form layout="vertical">
          <Form.Item label="启动变量（JSON）">
            <Input.TextArea rows={5} value={runVars} onChange={(e) => setRunVars(e.target.value)} />
          </Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}

/* ── 列表页 ──────────────────────────────────────────────────────────────── */

function ListInner() {
  const [rows, setRows] = useState<Record<string, unknown>[]>([])
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  const load = useCallback(() => {
    setLoading(true)
    http
      .get<Record<string, unknown>[]>('/api/admin/temporalflow/definitions')
      .then((d) => setRows(Array.isArray(d) ? d : []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false))
  }, [])
  useEffect(() => {
    load()
  }, [load])

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => navigate('/admin/temporalflow/designer/new')}>新建 Temporal 流程</Button>
        <Button onClick={load}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={rows}
        columns={[
          { title: 'ID', dataIndex: 'id', width: 60 },
          { title: '编码', dataIndex: 'code' },
          { title: '名称', dataIndex: 'name' },
          { title: '描述', dataIndex: 'description' },
          {
            title: '状态',
            dataIndex: 'status',
            width: 90,
            render: (v: number) => (v === 1 ? <Tag color="green">已发布</Tag> : <Tag>草稿</Tag>),
          },
          { title: '更新时间', dataIndex: 'updated_at' },
          {
            title: '操作',
            width: 160,
            render: (_: unknown, r: Record<string, unknown>) => (
              <Space>
                <Button size="small" onClick={() => navigate(`/admin/temporalflow/designer/${r.id}`)}>设计</Button>
                <Button
                  size="small"
                  onClick={async () => {
                    try {
                      await http.post(`/api/admin/temporalflow/definitions/${r.id}/run`, { vars: {} })
                      message.success('试运行已启动')
                    } catch (e) {
                      message.error(e instanceof Error ? e.message : '失败')
                    }
                  }}
                >
                  试运行
                </Button>
              </Space>
            ),
          },
        ]}
      />
    </div>
  )
}

import { useNavigate, useParams } from 'react-router-dom'
import { useRef } from 'react'
import { useThemeMode } from '../../theme/ThemeContext'
import { usePermission } from '../../hooks/usePermission'

// useThemeMode/usePermission 保留引用（布局一致性；当前设计器为受控浅色画布）
void useThemeMode
void usePermission

export default function TemporalFlowDesignerPage() {
  return (
    <ReactFlowProvider>
      <DesignerInner />
    </ReactFlowProvider>
  )
}

export function TemporalFlowListPage() {
  return (
    <ReactFlowProvider>
      <ListInner />
    </ReactFlowProvider>
  )
}
