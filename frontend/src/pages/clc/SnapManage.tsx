// 集群域 · 快照与回滚(MU frmClcSnap,QUERY_LIST):快照清单 + 创建快照 + 回滚(R-06 现存性校验 / 放弃引用回滚)。
// INV-24 序号单调;INV-25 只增不改、回滚产生新快照。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CameraOutlined, ReloadOutlined, RollbackOutlined } from '@ant-design/icons'
import { clusterApi, snapshotApi, type ClcSnapshot } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { SNAPSHOT_TYPE, pill, type ConsoleCtx } from './common'

const TYPE_OPTIONS = Object.keys(SNAPSHOT_TYPE).map((k) => ({ value: k, label: SNAPSHOT_TYPE[k].label }))

// 集群台嵌入态(v1.3):「快照与回滚」区段落在生命周期页签(9.10.2),集群由台级唯一确定 ⇒ 不出集群选择器。
export default function SnapManage({ ctx }: { ctx?: ConsoleCtx } = {}) {
  const hasPerm = usePermission()
  const [form] = Form.useForm()
  const [data, setData] = useState<ClcSnapshot[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; snapshot_type?: string }>(
    () => (ctx ? { cluster_ref: ctx.cluster.cluster_no } : {}),
  )
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [createTarget, setCreateTarget] = useState<Record<string, unknown> | null>(null)
  const [rollbackTarget, setRollbackTarget] = useState<ClcSnapshot | null>(null)
  const [saving, setSaving] = useState(false)

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    try {
      const res = await snapshotApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '快照清单加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    if (!ctx) {
      clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name })))).catch(() => undefined)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const handleCreate = async () => {
    const values = await form.validateFields()
    setSaving(true)
    try {
      await clusterApi.snapshot(values.cluster_id, values)
      message.success('快照已创建(INV-24 序号单调递增)')
      setCreateTarget(null)
      load()
      ctx?.refresh()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const handleRollback = async (abandon: boolean) => {
    if (!rollbackTarget) return
    try {
      await snapshotApi.rollback(rollbackTarget.id, { abandon_refs: abandon })
      message.success(abandon ? '回滚完成(放弃引用,引用置空并留痕)' : '回滚完成(已产生新快照,INV-25)')
      setRollbackTarget(null)
      load()
      ctx?.refresh()
    } catch { /* 拦截器已提示 */ }
  }

  // `REP-06` 的登记列结构（5.7）不含集群列；台内（9.10.1 徽章条唯一确定集群）不渲染该列，独立形态保留跨集群识别。
  const clusterColumn: ColumnsType<ClcSnapshot> = ctx ? [] : [
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
  ]

  const columns: ColumnsType<ClcSnapshot> = [
    { title: '快照编号', dataIndex: 'snapshot_no', key: 'snapshot_no', width: 170 },
    ...clusterColumn,
    { title: '序号', dataIndex: 'revision_no', key: 'revision_no', width: 70 },
    { title: '类型', dataIndex: 'snapshot_type', key: 'snapshot_type', width: 100, render: (v: string) => pill(SNAPSHOT_TYPE, v) },
    { title: '变更摘要', dataIndex: 'change_summary', key: 'change_summary', ellipsis: true },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
    {
      title: '操作', key: 'action', width: 140, fixed: 'right',
      render: (_, row) =>
        hasPerm('PERM-FUNC-18') ? (
          <Button type="link" size="small" icon={<RollbackOutlined />} onClick={() => setRollbackTarget(row)}>回滚到此版</Button>
        ) : null,
    },
  ]

  return (
    <Card>
      <TableToolbar
        title="快照与回滚"
        description="快照序号同集群单调递增(INV-24)、创建后不可修改(INV-25);回滚前置=目标快照引用资源现存性校验(R-06)"
        total={total}
        extra={
          <Space wrap>
            {!ctx && (
              <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
                options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
                onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            )}
            <Select allowClear placeholder="快照类型" style={{ width: 130 }} options={TYPE_OPTIONS}
              onChange={(v) => { const f = { ...filters, snapshot_type: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-17') && (
              <Button type="primary" icon={<CameraOutlined />} onClick={() => {
                setCreateTarget({}); form.resetFields()
                if (ctx) form.setFieldsValue({ cluster_id: ctx.cluster.id })
              }}>创建快照</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1000 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
      <Modal title="创建版本快照(FUNC-17)" open={!!createTarget} onOk={handleCreate} onCancel={() => setCreateTarget(null)} confirmLoading={saving} width={520}>
        <Form form={form} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择' }]}>
            <Select showSearch optionFilterProp="label" disabled={!!ctx}
              options={(ctx ? [{ id: ctx.cluster.id, cluster_no: ctx.cluster.cluster_no, name: ctx.cluster.name }] : clusters)
                .map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Form.Item name="change_summary" label="变更摘要" rules={[{ required: true, message: '必填' }]}>
            <Input placeholder="如 升级前基线" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`回滚到快照 ${rollbackTarget?.snapshot_no || ''}(序号 ${rollbackTarget?.revision_no ?? '-'})`}
        open={!!rollbackTarget} onCancel={() => setRollbackTarget(null)} width={620}
        footer={[
          <Button key="abandon" danger onClick={() => handleRollback(true)}>放弃引用回滚</Button>,
          <Button key="ok" type="primary" onClick={() => handleRollback(false)}>执行回滚</Button>,
        ]}
      >
        <Alert
          type="warning" showIcon
          message="R-06 回滚校验:目标快照引用的资源必须仍然存在,缺失即拒绝并列出;"
          description="若资源确认无法恢复,可选择「放弃引用回滚」:引用置空并留痕(INV-26 显式口径)。回滚本身会产生一条新的 ROLLBACK 快照,不覆盖旧快照(INV-25)。"
        />
      </Modal>
    </Card>
  )
}
