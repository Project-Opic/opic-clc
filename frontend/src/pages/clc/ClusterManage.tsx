// 集群域 · 集群台账(MU frmClcCluster,LIST_MAINTENANCE):筛选 + 创建四步向导(单表单聚合) + 显式恢复 + 详情。
// 五态:Loading=Table loading;Error=查询失败 Alert(可重试);Empty=Table 空态;Success=message;Not Found=路由通配重定向。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { EyeOutlined, PlusOutlined, ReloadOutlined, SafetyOutlined, EditOutlined, AppstoreOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { clusterApi, type ClcCluster } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { CLUSTER_HEALTH, CLUSTER_STATUS, DEGRADED_REASON, RUN_MODE, TARGET_CAPABILITY, TARGET_KIND, BINDING_PURPOSE, pill } from './common'

const STATUS_OPTIONS = Object.keys(CLUSTER_STATUS).map((k) => ({ value: k, label: CLUSTER_STATUS[k].label }))
const HEALTH_OPTIONS = Object.keys(CLUSTER_HEALTH).map((k) => ({ value: k, label: CLUSTER_HEALTH[k].label }))
const RUNMODE_OPTIONS = Object.keys(RUN_MODE).map((k) => ({ value: k, label: RUN_MODE[k].label }))

interface FormValues {
  name: string
  description?: string
  cluster_dns_domain?: string
  run_mode: string
  version_ref?: string
  executor_ref?: string
  registry_ref?: string
  registry_project_ref?: string
}

export default function ClusterManage() {
  const hasPerm = usePermission()
  const navigate = useNavigate()
  const [form] = Form.useForm<FormValues>()
  const [recoverForm] = Form.useForm()
  const [data, setData] = useState<ClcCluster[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ status?: string; run_mode?: string; health?: string; keyword?: string }>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<ClcCluster | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const [detail, setDetail] = useState<ClcCluster | null>(null)
  const [recoverTarget, setRecoverTarget] = useState<ClcCluster | null>(null)

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    try {
      const res = await clusterApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '集群列表加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const openWizard = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ run_mode: 'MCC' })
    setShowForm(true)
  }

  const openEdit = async (row: ClcCluster) => {
    const full = await clusterApi.get(row.id)
    setEditing(full)
    form.setFieldsValue({
      name: full.name, description: full.description || '', cluster_dns_domain: full.cluster_dns_domain || '',
      run_mode: full.run_mode, version_ref: full.version_ref || '', executor_ref: full.executor_ref || '',
      registry_ref: full.registry_ref || '', registry_project_ref: full.registry_project_ref || '',
    })
    setShowForm(true)
  }

  const handleSave = async () => {
    const values = await form.validateFields()
    setSaving(true)
    try {
      if (editing) {
        await clusterApi.update(editing.id, values as unknown as Record<string, unknown>)
        message.success('集群已更新')
      } else {
        await clusterApi.create(values as unknown as Record<string, unknown>)
        message.success('集群已创建(集群 + 引用台账 + 首快照)')
      }
      setShowForm(false)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const openDetail = async (row: ClcCluster) => {
    const full = await clusterApi.get(row.id)
    setDetail(full)
  }

  const openRecover = (row: ClcCluster) => {
    setRecoverTarget(row)
    recoverForm.resetFields()
    recoverForm.setFieldsValue({ mode: 'confirm' })
  }

  const handleRecover = async () => {
    if (!recoverTarget) return
    const values = await recoverForm.validateFields()
    try {
      await clusterApi.recover(recoverTarget.id, values)
      message.success('显式恢复完成(降级清除,R-11)')
      setRecoverTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    }
  }

  const columns: ColumnsType<ClcCluster> = [
    { title: '集群编号', dataIndex: 'cluster_no', key: 'cluster_no', width: 170 },
    { title: '名称', dataIndex: 'name', key: 'name', width: 160 },
    { title: '运行模式', dataIndex: 'run_mode', key: 'run_mode', width: 110, render: (v: string) => pill(RUN_MODE, v) },
    { title: '生命周期状态', dataIndex: 'status', key: 'status', width: 100, render: (v: string) => pill(CLUSTER_STATUS, v) },
    {
      title: '健康(原因)',
      dataIndex: 'health',
      key: 'health',
      width: 130,
      render: (v: string, r) => (
        <Space size={4}>{pill(CLUSTER_HEALTH, v)}{r.degraded_reason ? <span style={{ color: '#d46b08', fontSize: 12 }}>{DEGRADED_REASON[r.degraded_reason] || r.degraded_reason}</span> : null}</Space>
      ),
    },
    { title: '节点数', dataIndex: 'node_count', key: 'node_count', width: 80, render: (v: number) => v ?? 0 },
    { title: '最近任务', dataIndex: 'latest_task', key: 'latest_task', render: (v: string | null) => v || '-' },
    {
      title: '操作',
      key: 'action',
      width: 260,
      fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          {/* 行操作「台」= v1.3 集群台宿主屏唯一入口(8.1.1 表后 / 9.10);终态行不给(已销毁无一生可看),主数据牌 PERM-FUNC-20 缺位即不渲染 */}
          {hasPerm('PERM-FUNC-20') && row.status !== 'DESTROYED' && (
            <Button type="link" size="small" icon={<AppstoreOutlined />} onClick={() => navigate(`/clc/console/${row.id}`)}>台</Button>
          )}
          <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => openDetail(row)}>详情</Button>
          {hasPerm('PERM-FUNC-02') && row.status !== 'DESTROYED' && (
            <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(row)}>编辑</Button>
          )}
          {hasPerm('PERM-FUNC-16') && row.health === 'DEGRADED' && (
            <Button type="link" size="small" icon={<SafetyOutlined />} onClick={() => openRecover(row)}>显式恢复</Button>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Card>
      <TableToolbar
        title="集群台账"
        description="集群生命周期(单向迁移 INV-02)、健康双源(R-08)与引用摘要;DEGRADED 仅可显式恢复"
        total={total}
        extra={
          <Space wrap>
            <Select allowClear placeholder="生命周期状态" style={{ width: 140 }} options={STATUS_OPTIONS}
              onChange={(v) => { const f = { ...filters, status: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="运行模式" style={{ width: 140 }} options={RUNMODE_OPTIONS}
              onChange={(v) => { const f = { ...filters, run_mode: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="健康" style={{ width: 120 }} options={HEALTH_OPTIONS}
              onChange={(v) => { const f = { ...filters, health: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Input allowClear placeholder="名称 / 编号" style={{ width: 150 }} value={filters.keyword}
              onChange={(e) => setFilters({ ...filters, keyword: e.target.value })}
              onPressEnter={() => { setPage(1); load(1, size) }} />
            <Button onClick={() => { setPage(1); load(1, size) }}>查询</Button>
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-01') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={openWizard}>创建集群</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1100 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
      <Modal
        title={editing ? `编辑集群 ${editing.cluster_no}` : '创建集群(四步向导:登记 → 节点 → 引用 → 快照)'}
        open={showForm} onCancel={() => setShowForm(false)} onOk={handleSave} confirmLoading={saving} width={720}
      >
        <Form form={form} layout="vertical">
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="name" label="集群名称" rules={[{ required: true, message: '请输入集群名称(INV-01 未销毁范围内唯一)' }]} style={{ width: 280 }}>
              <Input placeholder="如 华东生产集群-01" />
            </Form.Item>
            <Form.Item name="cluster_dns_domain" label="集群 DNS 域名" style={{ width: 280 }}>
              <Input placeholder="如 clc-01.opic-ai.ccoe.tech" />
            </Form.Item>
          </Space>
          <Form.Item name="description" label="说明"><Input.TextArea rows={2} /></Form.Item>
          <Form.Item name="run_mode" label="运行模式(INV-04:LEGACY ⇔ 四类引用全空;平台可禁用 legacy)">
            <Select options={RUNMODE_OPTIONS} disabled={!!editing} />
          </Form.Item>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="executor_ref" label="交付执行机引用" style={{ width: 280 }}>
              <Input placeholder="IGN-EXEC-xxxx" />
            </Form.Item>
            <Form.Item name="version_ref" label="交付版本引用" style={{ width: 280 }}>
              <Input placeholder="IGN-VER-xxxx(REF-05 未归档且就绪及之后)" />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="registry_ref" label="镜像仓库实例引用(与项目成对,INV-05)" style={{ width: 280 }}>
              <Input placeholder="REG-INST-xxxx" />
            </Form.Item>
            <Form.Item name="registry_project_ref" label="镜像仓库项目引用" style={{ width: 280 }}>
              <Input placeholder="REG-PROJ-xxxx" />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
      <Drawer title={`集群详情 ${detail?.cluster_no || ''}`} width={640} open={!!detail} onClose={() => setDetail(null)}>
        {detail && (
          <>
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="编号" span={1}>{detail.cluster_no}</Descriptions.Item>
              <Descriptions.Item label="名称" span={1}>{detail.name}</Descriptions.Item>
              <Descriptions.Item label="状态" span={1}>{pill(CLUSTER_STATUS, detail.status)}</Descriptions.Item>
              <Descriptions.Item label="健康" span={1}>{pill(CLUSTER_HEALTH, detail.health)}</Descriptions.Item>
              <Descriptions.Item label="运行模式" span={1}>{RUN_MODE[detail.run_mode]?.label || detail.run_mode}</Descriptions.Item>
              <Descriptions.Item label="DNS 域名" span={1}>{detail.cluster_dns_domain || '-'}</Descriptions.Item>
              <Descriptions.Item label="主控入口" span={1}>{detail.master_endpoints || '-'}</Descriptions.Item>
              <Descriptions.Item label="凭据存放标识(INV-08 仅标识)" span={2}>{detail.kubeconfig_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="版本引用" span={1}>{detail.version_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="执行机引用" span={1}>{detail.executor_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="仓库实例" span={1}>{detail.registry_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="仓库项目" span={1}>{detail.registry_project_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="登记时间" span={2}>{detail.created_at || '-'}</Descriptions.Item>
            </Descriptions>
            <h4 style={{ marginTop: 16 }}>引用台账(R-01 双写)</h4>
            <Table
              size="small" rowKey="binding_no" pagination={false}
              dataSource={detail.bindings || []}
              columns={[
                { title: '能力', dataIndex: 'target_capability', render: (v: string) => TARGET_CAPABILITY[v] || v },
                { title: '类型', dataIndex: 'target_kind', render: (v: string) => TARGET_KIND[v] || v },
                { title: '资源标识', dataIndex: 'target_ref' },
                { title: '用途', dataIndex: 'purpose', render: (v: string) => BINDING_PURPOSE[v] || v },
              ]}
            />
          </>
        )}
      </Drawer>
      <Modal
        title={`显式恢复 ${recoverTarget?.cluster_no || ''}(R-11:仅 DEGRADED 可调用)`}
        open={!!recoverTarget} onOk={handleRecover} onCancel={() => setRecoverTarget(null)} width={560}
      >
        <Form form={recoverForm} layout="vertical">
          <Form.Item name="mode" label="恢复方式" rules={[{ required: true }]}>
            <Select options={[
              { value: 'confirm', label: '确认恢复(资源已恢复,直接清除降级)' },
              { value: 'rebind', label: '重绑定(登记新引用 + 双写 + 派生参数)' },
            ]} onChange={(v) => recoverForm.setFieldsValue({ mode: v })} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.mode !== b.mode}>
            {({ getFieldValue }) =>
              getFieldValue('mode') === 'rebind' ? (
                <>
                  <Form.Item name="target_capability" label="目标能力" rules={[{ required: true, message: '必填' }]}>
                    <Select options={Object.keys(TARGET_CAPABILITY).map((k) => ({ value: k, label: TARGET_CAPABILITY[k] }))} />
                  </Form.Item>
                  <Form.Item name="target_kind" label="资源类型(须与能力组合合法,INV-17)" rules={[{ required: true, message: '必填' }]}>
                    <Select options={Object.keys(TARGET_KIND).map((k) => ({ value: k, label: TARGET_KIND[k] }))} />
                  </Form.Item>
                  <Form.Item name="new_target_ref" label="新资源标识(必须存在且可用)" rules={[{ required: true, message: '必填' }]}>
                    <Input placeholder="如 REG-INST-0002" />
                  </Form.Item>
                  <Form.Item name="purpose" label="用途" rules={[{ required: true, message: '必填' }]}>
                    <Select options={Object.keys(BINDING_PURPOSE).map((k) => ({ value: k, label: BINDING_PURPOSE[k] }))} />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
