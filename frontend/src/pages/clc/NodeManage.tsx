// 集群域 · 节点管理(MU frmClcNode,LIST_MAINTENANCE):节点清单 + 添加/移除/重绑定。
// R-02 资产占用拦截、INV-13 REMOVED 终局、INV-12 裸机人工接管。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, ReloadOutlined, SwapOutlined, MinusCircleOutlined } from '@ant-design/icons'
import { clusterApi, nodeApi, type ClcNode } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { NODE_HEALTH, NODE_ROLE, NODE_STATUS, pill } from './common'

const ROLE_OPTIONS = Object.keys(NODE_ROLE).map((k) => ({ value: k, label: NODE_ROLE[k] }))
const STATUS_OPTIONS = Object.keys(NODE_STATUS).map((k) => ({ value: k, label: NODE_STATUS[k].label }))

export default function NodeManage() {
  const hasPerm = usePermission()
  const [form] = Form.useForm()
  const [rebindForm] = Form.useForm()
  const [data, setData] = useState<ClcNode[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; node_role?: string; node_status?: string }>({})
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [addTarget, setAddTarget] = useState<ClcNode | null>(null)
  const [rebindTarget, setRebindTarget] = useState<ClcNode | null>(null)
  const [saving, setSaving] = useState(false)

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    try {
      const res = await nodeApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '节点清单加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name })))).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const handleAdd = async () => {
    if (!addTarget) return
    const values = await form.validateFields()
    setSaving(true)
    try {
      await clusterApi.addNode(values.cluster_id, values)
      message.success('节点已登记为待加入,ADD_NODE 任务排队执行(成功后回填 K8s 节点名 INV-14)')
      setAddTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const handleRebind = async () => {
    if (!rebindTarget) return
    const values = await rebindForm.validateFields()
    try {
      await nodeApi.rebind(rebindTarget.id, values)
      message.success('节点已重绑定(R-12)')
      setRebindTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    }
  }

  const columns: ColumnsType<ClcNode> = [
    { title: '节点编号', dataIndex: 'node_no', key: 'node_no', width: 170 },
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '角色', dataIndex: 'node_role', key: 'node_role', width: 90, render: (v: string) => NODE_ROLE[v] || v },
    { title: 'IP', dataIndex: 'ip_address', key: 'ip_address', width: 130 },
    { title: 'K8s 节点名', dataIndex: 'k8s_nodename', key: 'k8s_nodename', render: (v: string | null) => v || '-' },
    { title: '资产引用', dataIndex: 'asset_ref', key: 'asset_ref', width: 140, render: (v: string | null) => v || <span style={{ color: '#8c8c8c' }}>裸机(IP 人工接管 INV-12)</span> },
    { title: '状态', dataIndex: 'node_status', key: 'node_status', width: 90, render: (v: string) => pill(NODE_STATUS, v) },
    { title: '健康(时间)', key: 'health', width: 140, render: (_, r) => <Space size={4}>{pill(NODE_HEALTH, r.node_health)}<span style={{ fontSize: 12, color: '#8c8c8c' }}>{r.node_health_at || ''}</span></Space> },
    {
      title: '操作', key: 'action', width: 170, fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          {hasPerm('PERM-FUNC-08') && (row.node_status === 'ADDED' || row.node_status === 'PENDING') && (
            <Button type="link" size="small" icon={<SwapOutlined />} onClick={() => { setRebindTarget(row); rebindForm.resetFields() }}>重绑定</Button>
          )}
          {hasPerm('PERM-FUNC-07') && row.node_status !== 'REMOVED' && (
            <Popconfirm title="移除该节点?REMOVED 为终局(INV-13),重新加入须登记新行"
              onConfirm={async () => {
                try {
                  await nodeApi.remove(row.id)
                  message.success('移除已受理,节点置移除中(DEL_NODE 任务成功后置 REMOVED,终局保留行)')
                  load()
                } catch { /* 拦截器已提示 */ }
              }}>
              <Button type="link" size="small" danger icon={<MinusCircleOutlined />}>移除</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Card>
      <TableToolbar
        title="节点管理"
        description="节点清单:同一集群内 IP 唯一(INV-10);资产占用拦截(R-02 一台资产只服务一个活跃集群);移除即终局(INV-13)"
        total={total}
        extra={
          <Space wrap>
            <Select allowClear showSearch placeholder="集群" style={{ width: 190 }}
              options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
              optionFilterProp="label"
              onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="节点角色" style={{ width: 130 }} options={ROLE_OPTIONS}
              onChange={(v) => { const f = { ...filters, node_role: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="状态" style={{ width: 120 }} options={STATUS_OPTIONS}
              onChange={(v) => { const f = { ...filters, node_status: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-06') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => { setAddTarget({ id: 0 } as ClcNode); form.resetFields() }}>添加节点</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1200 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
      <Modal
        title="添加节点(选择目标集群;交付编排发起 ADD_NODE)"
        open={!!addTarget} onOk={handleAdd} onCancel={() => setAddTarget(null)} confirmLoading={saving} width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择目标集群' }]}>
            <Select showSearch optionFilterProp="label" placeholder="选择集群"
              options={clusters.map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Form.Item name="ip_address" label="IP 地址(必填;裸机凭 IP 人工接管 INV-12)" rules={[{ required: true, message: '请输入 IP 地址' }]}>
            <Input placeholder="如 10.26.8.101" />
          </Form.Item>
          <Form.Item name="node_role" label="节点角色(INV-11 字典)" rules={[{ required: true, message: '请选择角色' }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="asset_ref" label="资产引用(可空 = 裸机;R-02 占用拦截)">
            <Input placeholder="AST-xxxx(可空)" />
          </Form.Item>
          <Form.Item name="k8s_nodename" label="K8s 节点名(可空,成功后自动回填)">
            <Input placeholder="可空" />
          </Form.Item>
          <Form.Item name="remark" label="备注"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`节点重绑定 ${rebindTarget?.node_no || ''}(R-12:新资产未被占用且登记值可反查)`}
        open={!!rebindTarget} onOk={handleRebind} onCancel={() => setRebindTarget(null)} width={480}
      >
        <Form form={rebindForm} layout="vertical">
          <Form.Item name="new_asset_ref" label="新资产引用" rules={[{ required: true, message: '请输入新资产引用' }]}>
            <Input placeholder="AST-xxxx" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
