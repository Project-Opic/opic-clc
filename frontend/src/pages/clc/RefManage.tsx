// 集群域 · 引用与参数(MU frmClcRef,LIST_MAINTENANCE + TAB):引用台账 + 参数清单(待同步页签)。
// R-01 双写、R-04 派生与覆写保护、FUNC-15 显式确认同步(派生归派生、生效归运维)。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tabs, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, ReloadOutlined, SyncOutlined } from '@ant-design/icons'
import { bindingApi, clusterApi, paramApi, type ClcBinding, type ClcParam } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { BINDING_PURPOSE, BINDING_SOURCE, PARAM_SOURCE, TARGET_CAPABILITY, TARGET_KIND, pill } from './common'

const CAP_OPTIONS = Object.keys(TARGET_CAPABILITY).map((k) => ({ value: k, label: TARGET_CAPABILITY[k] }))
const KIND_OPTIONS = Object.keys(TARGET_KIND).map((k) => ({ value: k, label: TARGET_KIND[k] }))
const PURPOSE_OPTIONS = Object.keys(BINDING_PURPOSE).map((k) => ({ value: k, label: BINDING_PURPOSE[k] }))

export default function RefManage() {
  const hasPerm = usePermission()
  const [bindingForm] = Form.useForm()
  const [paramForm] = Form.useForm()
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string; sync_pending_count?: number }[]>([])
  const [bindingData, setBindingData] = useState<ClcBinding[]>([])
  const [bindingTotal, setBindingTotal] = useState(0)
  const [paramData, setParamData] = useState<ClcParam[]>([])
  const [paramTotal, setParamTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [bFilters, setBFilters] = useState<{ cluster_ref?: string; target_capability?: string }>({})
  const [pFilters, setPFilters] = useState<{ cluster_ref?: string; sync_pending?: string }>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [bindingTarget, setBindingTarget] = useState<Record<string, unknown> | null>(null)
  const [paramTarget, setParamTarget] = useState<Record<string, unknown> | null>(null)
  const [syncTarget, setSyncTarget] = useState<Record<string, unknown> | null>(null)
  const [saving, setSaving] = useState(false)

  const loadClusters = () => {
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name })))).catch(() => undefined)
  }

  const loadBindings = async (p = page, s = size, f = bFilters) => {
    setLoading(true)
    setError('')
    try {
      const res = await bindingApi.list({ page: p, size: s, ...f })
      setBindingData(res.list || [])
      setBindingTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '引用台账加载失败')
    } finally {
      setLoading(false)
    }
  }

  const loadParams = async (p = page, s = size, f = pFilters) => {
    setLoading(true)
    setError('')
    try {
      const res = await paramApi.list({ page: p, size: s, ...f })
      setParamData(res.list || [])
      setParamTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '参数清单加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadBindings()
    loadParams()
    loadClusters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const handleBinding = async () => {
    const values = await bindingForm.validateFields()
    setSaving(true)
    try {
      await clusterApi.adjustBinding(values.cluster_id, values)
      message.success('引用已调整(内联列与台账双写 R-01,参数派生 R-04)')
      setBindingTarget(null)
      loadBindings()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const handleParam = async () => {
    const values = await paramForm.validateFields()
    try {
      await clusterApi.overrideParam(values.cluster_id, { param_key: values.param_key, param_value: values.param_value })
      message.success('参数已人工覆写(留痕,不覆盖保护 R-04)')
      setParamTarget(null)
      loadParams()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    }
  }

  const handleSync = async () => {
    if (!syncTarget) return
    try {
      await clusterApi.paramSync(syncTarget.id as number)
      message.success('参数同步已确认执行(待同步标记清除)')
      setSyncTarget(null)
      loadParams()
    } catch { /* 拦截器已提示 */ }
  }

  const syncClusterLabel = syncTarget ? `${syncTarget.name || ''}(${syncTarget.cluster_no || ''})` : ''

  const bindingColumns: ColumnsType<ClcBinding> = [
    { title: '绑定编号', dataIndex: 'binding_no', key: 'binding_no', width: 170 },
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '目标能力', dataIndex: 'target_capability', key: 'target_capability', width: 100, render: (v: string) => TARGET_CAPABILITY[v] || v },
    { title: '资源类型', dataIndex: 'target_kind', key: 'target_kind', width: 100, render: (v: string) => TARGET_KIND[v] || v },
    { title: '资源标识(跳原域)', dataIndex: 'target_ref', key: 'target_ref', width: 160 },
    { title: '用途', dataIndex: 'purpose', key: 'purpose', width: 140, render: (v: string) => BINDING_PURPOSE[v] || v },
    { title: '来源', dataIndex: 'binding_source', key: 'binding_source', width: 100, render: (v: string) => pill(BINDING_SOURCE, v) },
    { title: '对账注记', dataIndex: 'reconcile_note', key: 'reconcile_note', ellipsis: true, render: (v: string | null) => v || '-' },
  ]

  const paramColumns: ColumnsType<ClcParam> = [
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '参数键', dataIndex: 'param_key', key: 'param_key', width: 200 },
    { title: '值', dataIndex: 'param_value', key: 'param_value', ellipsis: true },
    { title: '来源', dataIndex: 'param_source', key: 'param_source', width: 100, render: (v: string) => pill(PARAM_SOURCE, v) },
    { title: '派生来源', dataIndex: 'derived_from', key: 'derived_from', width: 100, render: (v: string | null) => v || '-' },
    {
      title: '待同步/冲突提示', key: 'sync', width: 240,
      render: (_, r) =>
        r.sync_pending === 1 ? (
          <Space size={4}>
            <Button type="link" size="small" icon={<SyncOutlined />} disabled={!hasPerm('PERM-FUNC-15')}
              onClick={() => {
                const cluster = clusters.find((c) => c.cluster_no === r.cluster_ref)
                if (cluster) setSyncTarget(cluster as unknown as Record<string, unknown>)
                else message.warning('集群清单未加载,请刷新后重试')
              }}>确认同步</Button>
            <span style={{ fontSize: 12, color: '#d46b08' }}>{r.derive_note || '-'}</span>
          </Space>
        ) : (
          <span style={{ color: '#8c8c8c', fontSize: 12 }}>-</span>
        ),
    },
  ]

  return (
    <Card>
      <Tabs
        items={[
          {
            key: 'binding', label: '引用台账',
            children: (
              <>
                <TableToolbar
                  title="引用台账"
                  description="内联引用列与台账双写(R-01);组合唯一(INV-16);EVENT_SYNC 行变更须携带对账注记(INV-18)"
                  total={bindingTotal}
                  extra={
                    <Space wrap>
                      <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
                        options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
                        onChange={(v) => { const f = { ...bFilters, cluster_ref: v }; setBFilters(f); setPage(1); loadBindings(1, size, f) }} />
                      <Select allowClear placeholder="目标能力" style={{ width: 130 }} options={CAP_OPTIONS}
                        onChange={(v) => { const f = { ...bFilters, target_capability: v }; setBFilters(f); setPage(1); loadBindings(1, size, f) }} />
                      <Button icon={<ReloadOutlined />} onClick={() => loadBindings()}>刷新</Button>
                      {hasPerm('PERM-FUNC-13') && (
                        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setBindingTarget({}); bindingForm.resetFields() }}>调整引用</Button>
                      )}
                    </Space>
                  }
                />
                {error && <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error} action={<Button size="small" onClick={() => loadBindings()}>重试</Button>} />}
                <Table rowKey="id" columns={bindingColumns} dataSource={bindingData} loading={loading} scroll={{ x: 1100 }}
                  pagination={{ current: page, pageSize: size, total: bindingTotal, showSizeChanger: true, onChange: (p, s) => { setPage(p); setSize(s) } }} />
              </>
            ),
          },
          {
            key: 'param', label: '参数清单 / 待同步',
            children: (
              <>
                <TableToolbar
                  title="参数清单"
                  description="参数派生与覆写保护(R-04):派生只置待同步、不覆盖覆写行;生效须显式确认(FUNC-15)"
                  total={paramTotal}
                  extra={
                    <Space wrap>
                      <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
                        options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
                        onChange={(v) => { const f = { ...pFilters, cluster_ref: v }; setPFilters(f); setPage(1); loadParams(1, size, f) }} />
                      <Select allowClear placeholder="待同步" style={{ width: 120 }}
                        options={[{ value: '1', label: '待同步' }, { value: '0', label: '已生效' }]}
                        onChange={(v) => { const f = { ...pFilters, sync_pending: v }; setPFilters(f); setPage(1); loadParams(1, size, f) }} />
                      <Button icon={<ReloadOutlined />} onClick={() => loadParams()}>刷新</Button>
                      {hasPerm('PERM-FUNC-14') && (
                        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setParamTarget({}); paramForm.resetFields() }}>参数人工覆写</Button>
                      )}
                    </Space>
                  }
                />
                {error && <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error} action={<Button size="small" onClick={() => loadParams()}>重试</Button>} />}
                <Table rowKey="id" columns={paramColumns} dataSource={paramData} loading={loading} scroll={{ x: 1000 }}
                  pagination={{ current: page, pageSize: size, total: paramTotal, showSizeChanger: true, onChange: (p, s) => { setPage(p); setSize(s) } }} />
              </>
            ),
          },
        ]}
      />
      <Modal title="调整中心引用(新增/替换;R-01 双写)" open={!!bindingTarget} onOk={handleBinding} onCancel={() => setBindingTarget(null)} confirmLoading={saving} width={560}>
        <Form form={bindingForm} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择' }]}>
            <Select showSearch optionFilterProp="label" options={clusters.map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Form.Item name="target_capability" label="目标能力" rules={[{ required: true, message: '必填' }]}>
            <Select options={CAP_OPTIONS} />
          </Form.Item>
          <Form.Item name="target_kind" label="资源类型(组合合法性 INV-17)" rules={[{ required: true, message: '必填' }]}>
            <Select options={KIND_OPTIONS} />
          </Form.Item>
          <Form.Item name="target_ref" label="资源标识" rules={[{ required: true, message: '必填' }]}>
            <Input placeholder="如 REG-INST-0001" />
          </Form.Item>
          <Form.Item name="purpose" label="用途" rules={[{ required: true, message: '必填' }]}>
            <Select options={PURPOSE_OPTIONS} />
          </Form.Item>
          <Form.Item name="binding_source" label="登记来源(默认 MANUAL)" initialValue="MANUAL">
            <Select options={Object.keys(BINDING_SOURCE).map((k) => ({ value: k, label: BINDING_SOURCE[k].label }))} />
          </Form.Item>
          <Form.Item name="reconcile_note" label="对账注记(EVENT_SYNC 来源必填,INV-18)">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal title="参数人工覆写(INV-21 留痕)" open={!!paramTarget} onOk={handleParam} onCancel={() => setParamTarget(null)} width={520}>
        <Form form={paramForm} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择' }]}>
            <Select showSearch optionFilterProp="label" options={clusters.map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Form.Item name="param_key" label="参数键" rules={[{ required: true, message: '必填' }]}>
            <Input placeholder="如 image.mirror.endpoint" />
          </Form.Item>
          <Form.Item name="param_value" label="参数值" rules={[{ required: true, message: '必填' }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`触发参数同步(确认执行) → ${syncClusterLabel}`}
        open={!!syncTarget} onOk={handleSync} onCancel={() => setSyncTarget(null)} okText="确认执行"
      >
        <Alert type="info" showIcon message="确认后清除该集群全部待同步标记并生成派生快照;交付生效经平台任务通道(REF-12)。空集合会被拒绝(勿拼 IN ())。" />
      </Modal>
    </Card>
  )
}
