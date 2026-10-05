// 集群域 · 健康探活(MU frmClcHealth,MASTER_DETAIL_FORM):探活历史 + 聚合健康 + 人工探活登记。
// INV-34 失败原因/就绪数口径;R-08 双源写回(不覆盖 DEGRADED);探活只读探测(INV-36)。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Checkbox, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, MonitorOutlined } from '@ant-design/icons'
import { clusterApi, probeApi, type ClcProbe } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { CLUSTER_HEALTH, DEGRADED_REASON, PROBE_RESULT, pill } from './common'

const RESULT_OPTIONS = Object.keys(PROBE_RESULT).map((k) => ({ value: k, label: PROBE_RESULT[k].label }))

export default function HealthManage() {
  const hasPerm = usePermission()
  const [form] = Form.useForm()
  const [data, setData] = useState<ClcProbe[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; probe_result?: string }>({})
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string; health: string; degraded_reason: string | null }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [probeTarget, setProbeTarget] = useState<Record<string, unknown> | null>(null)
  const [saving, setSaving] = useState(false)
  const [aggregate, setAggregate] = useState<Record<string, unknown>[]>([])

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    try {
      const res = await probeApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
      const agg = await probeApi.aggregate({})
      setAggregate(agg.list || [])
    } catch (e) {
      setError((e as Error).message || '探活历史加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name, health: c.health, degraded_reason: c.degraded_reason })))).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const handleProbe = async () => {
    const values = await form.validateFields()
    setSaving(true)
    try {
      await clusterApi.probe(values.cluster_id, {
        apiserver_reachable: !!values.apiserver_reachable,
        tls_handshake_ok: !!values.tls_handshake_ok,
        ready_nodes: values.ready_nodes ? Number(values.ready_nodes) : undefined,
        total_nodes: values.total_nodes ? Number(values.total_nodes) : undefined,
        fail_reason: values.fail_reason,
      })
      message.success('探活已登记(健康经 R-08 防抖窗口写回;不覆盖 DEGRADED)')
      setProbeTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const probeColumns: ColumnsType<ClcProbe> = [
    { title: '记录编号', dataIndex: 'probe_no', key: 'probe_no', width: 170 },
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '探活时间', dataIndex: 'probed_at', key: 'probed_at', width: 180 },
    { title: '触发方式', dataIndex: 'trigger_source', key: 'trigger_source', width: 90, render: (v: string) => (v === 'MANUAL' ? '人工' : '定时') },
    { title: '控制面', dataIndex: 'apiserver_reachable', key: 'apiserver_reachable', width: 80, render: (v: number) => (v === 1 ? '可达' : '不可达') },
    { title: 'TLS', dataIndex: 'tls_handshake_ok', key: 'tls_handshake_ok', width: 80, render: (v: number) => (v === 1 ? '通过' : '未过') },
    { title: '就绪/总节点', key: 'nodes', width: 110, render: (_, r) => `${r.ready_nodes}/${r.total_nodes}` },
    { title: '结论', dataIndex: 'probe_result', key: 'probe_result', width: 80, render: (v: string) => pill(PROBE_RESULT, v) },
    { title: '失败原因', dataIndex: 'fail_reason', key: 'fail_reason', ellipsis: true, render: (v: string | null) => v || '-' },
  ]

  const aggColumns: ColumnsType<Record<string, unknown>> = [
    { title: '集群', key: 'cluster', width: 170, render: (_, r) => `${String(r.cluster_name ?? '-')}(${String(r.cluster_no ?? '-')})` },
    { title: '状态', key: 'status', width: 90, render: (_, r) => String(r.status ?? '-') },
    { title: '集群自身健康', key: 'health', width: 150, render: (_, r) => (<Space size={4}>{pill(CLUSTER_HEALTH, String(r.health))}{r.degraded_reason ? <span style={{ fontSize: 12, color: '#d46b08' }}>{DEGRADED_REASON[String(r.degraded_reason)] || String(r.degraded_reason)}</span> : null}</Space>) },
    { title: '交付执行机', dataIndex: 'executor_ref', key: 'executor_ref', render: (v: unknown) => String(v ?? '-') },
    { title: '基础设施证书数', dataIndex: 'cert_count', key: 'cert_count', width: 120, render: (v: unknown) => String(v ?? 0) },
    { title: '镜像仓库', dataIndex: 'registry_ref', key: 'registry_ref', render: (v: unknown) => String(v ?? '-') },
    { title: '节点资产(就绪/总数)', key: 'nodes', width: 150, render: (_, r) => `${r.healthy_nodes}/${r.total_nodes}` },
    { title: '最近探活', dataIndex: 'last_probe_at', key: 'last_probe_at', width: 170, render: (v: unknown) => String(v ?? '-') },
  ]

  return (
    <Card>
      <TableToolbar
        title="健康探活"
        description="探活历史(只读探测 INV-36)与聚合健康;降级归因可见、DEGRADED 仅显式恢复清除(INV-06/R-11)"
        total={total}
        extra={
          <Space wrap>
            <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
              options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
              onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="结论" style={{ width: 110 }} options={RESULT_OPTIONS}
              onChange={(v) => { const f = { ...filters, probe_result: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-26') && (
              <Button type="primary" icon={<MonitorOutlined />} onClick={() => { setProbeTarget({}); form.resetFields() }}>人工探活</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <h4 style={{ marginTop: 0 }}>聚合健康(FUNC-27)</h4>
      <Table size="small" rowKey={(r) => String(r.cluster_no)} columns={aggColumns} dataSource={aggregate} loading={loading} scroll={{ x: 1100 }} pagination={false} style={{ marginBottom: 24 }} />
      <h4>探活历史(FUNC-26)</h4>
      <Table
        rowKey="id" columns={probeColumns} dataSource={data} loading={loading} scroll={{ x: 1100 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
      <Modal title="人工探活登记(INV-34 口径校验)" open={!!probeTarget} onOk={handleProbe} onCancel={() => setProbeTarget(null)} confirmLoading={saving} width={560}>
        <Form form={form} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择' }]}>
            <Select showSearch optionFilterProp="label" options={clusters.map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="apiserver_reachable" valuePropName="checked" label="控制面可达" initialValue={true}>
              <Checkbox />
            </Form.Item>
            <Form.Item name="tls_handshake_ok" valuePropName="checked" label="TLS 握手通过" initialValue={true}>
              <Checkbox />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="total_nodes" label="总节点数(可空=自动取已加入节点)">
              <Input type="number" min={0} />
            </Form.Item>
            <Form.Item name="ready_nodes" label="就绪节点数(INV-34 ≤ 总数)">
              <Input type="number" min={0} />
            </Form.Item>
          </Space>
          <Form.Item name="fail_reason" label="失败原因(结论为失败时必填,INV-34)">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
