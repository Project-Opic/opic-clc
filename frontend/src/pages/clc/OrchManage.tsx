// 集群域 · 交付编排(MU frmClcOrch,SINGLE_FORM):交付动作台 + 任务台账 + 销毁审批(影响面强制展示 + 保存草稿/提交双按钮)。
// R-10 销毁强制不免审批;R-09 失败步骤索引写回;INV-28 STALLED 与 FAILED 分列。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CaretRightOutlined, ReloadOutlined, DeleteOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { clusterApi, taskApi, type ClcTask } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import { EXEC_PATH, TASK_FAIL_REASON, TASK_KIND, TASK_STATUS, pill } from './common'

const KIND_OPTIONS = Object.keys(TASK_KIND).map((k) => ({ value: k, label: TASK_KIND[k] }))
const DELIVERABLE_KINDS = ['INSTALL', 'RETRY_INSTALL', 'UPGRADE', 'BACKUP', 'RESTORE', 'START', 'STOP'].map((k) => ({ value: k, label: TASK_KIND[k] }))

export default function OrchManage() {
  const hasPerm = usePermission()
  const [form] = Form.useForm()
  const [destroyForm] = Form.useForm()
  const [data, setData] = useState<ClcTask[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; task_kind?: string; task_status?: string }>({})
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string; status: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [actionTarget, setActionTarget] = useState<{ id: number; cluster_no: string } | null>(null)
  const [running, setRunning] = useState(false)
  const [destroyTarget, setDestroyTarget] = useState<{ id: number; cluster_no: string; name: string } | null>(null)
  const [impact, setImpact] = useState<Record<string, unknown> | null>(null)
  const [destroying, setDestroying] = useState(false)

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    try {
      const res = await taskApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '任务台账加载失败')
    } finally {
      setLoading(false)
    }
  }

  const loadClusters = () => {
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name, status: c.status })))).catch(() => undefined)
  }

  useEffect(() => {
    load()
    loadClusters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const openAction = (cluster?: { id: number; cluster_no: string }) => {
    if (!cluster) return
    setActionTarget(cluster)
    form.resetFields()
    form.setFieldsValue({ task_kind: 'INSTALL', execution_path: 'LOCAL' })
  }

  const handleAction = async () => {
    if (!actionTarget) return
    const values = await form.validateFields()
    setRunning(true)
    try {
      await clusterApi.taskAction(actionTarget.id, values)
      message.success('交付动作已执行(任务台账与集群状态同步回写)')
      setActionTarget(null)
      load()
      loadClusters()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setRunning(false)
    }
  }

  // 销毁申请:影响面强制展示(R-10)→ 保存草稿 / 提交 双按钮
  const openDestroy = async (cluster: { id: number; cluster_no: string; name: string }) => {
    setDestroyTarget(cluster)
    destroyForm.resetFields()
    try {
      const imp = await clusterApi.destroyImpact(cluster.id)
      setImpact(imp)
    } catch {
      setImpact(null)
    }
  }

  const handleDestroyDraft = async () => {
    if (!destroyTarget) return
    const values = await destroyForm.getFieldsValue()
    try {
      await clusterApi.destroyDraft(destroyTarget.id, values)
      message.success('销毁申请草稿已保存(状态不变)')
    } catch { /* 拦截器已提示 */ }
  }

  const handleDestroySubmit = async () => {
    if (!destroyTarget) return
    const values = await destroyForm.validateFields()
    setDestroying(true)
    try {
      await clusterApi.destroySubmit(destroyTarget.id, values)
      message.success('销毁申请已提交(CLC-ROLE-02 审批,审批通过前状态不变)')
      setDestroyTarget(null)
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setDestroying(false)
    }
  }

  const columns: ColumnsType<ClcTask> = [
    { title: '任务编号', dataIndex: 'task_no', key: 'task_no', width: 170 },
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '类别', dataIndex: 'task_kind', key: 'task_kind', width: 90, render: (v: string) => TASK_KIND[v] || v },
    { title: '执行位置', dataIndex: 'execution_path', key: 'execution_path', width: 100, render: (v: string) => EXEC_PATH[v] || v },
    { title: '状态', dataIndex: 'task_status', key: 'task_status', width: 90, render: (v: string) => pill(TASK_STATUS, v) },
    { title: '失败原因', dataIndex: 'fail_reason', key: 'fail_reason', width: 110, render: (v: string | null) => (v ? TASK_FAIL_REASON[v] || v : '-') },
    { title: '失败步骤', dataIndex: 'fail_step_index', key: 'fail_step_index', width: 80, render: (v: number | null) => (v === null || v === undefined ? '-' : v) },
    { title: '结果摘要', dataIndex: 'result_summary', key: 'result_summary', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '起止时间', key: 'time', width: 260, render: (_, r) => `${r.started_at || '-'} ~ ${r.ended_at || '-'}` },
    { title: '提交人', dataIndex: 'submitted_by', key: 'submitted_by', width: 100 },
  ]

  return (
    <Card>
      <TableToolbar
        title="交付编排"
        description="交付动作经任务台账留痕(只增不改 INV-31);安装失败携带失败步骤索引并写回集群(R-09/INV-30);销毁强制不免审批(R-10)"
        total={total}
        extra={
          <Space wrap>
            <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
              options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
              onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="任务类别" style={{ width: 130 }} options={KIND_OPTIONS}
              onChange={(v) => { const f = { ...filters, task_kind: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="状态" style={{ width: 120 }}
              options={Object.keys(TASK_STATUS).map((k) => ({ value: k, label: TASK_STATUS[k].label }))}
              onChange={(v) => { const f = { ...filters, task_status: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-03') && (
              <Button type="primary" icon={<CaretRightOutlined />} onClick={() => openAction(clusters[0])}>发起交付动作</Button>
            )}
            {hasPerm('PERM-FUNC-05') && clusters.length > 0 && (
              <Button danger icon={<DeleteOutlined />} onClick={() => openDestroy(clusters[0])}>集群销毁</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1300 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
      <Modal
        title={`发起交付动作 → ${actionTarget?.cluster_no || ''}`}
        open={!!actionTarget} onOk={handleAction} onCancel={() => setActionTarget(null)} confirmLoading={running} width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="task_kind" label="任务类别(INSTALL 前置 DRAFT/INSTALL_FAILED;其余前置 RUNNING)" rules={[{ required: true }]}>
            <Select options={DELIVERABLE_KINDS} />
          </Form.Item>
          <Form.Item name="execution_path" label="执行位置(INV-32:登记值与真实执行位置一致,绝不静默切换)" rules={[{ required: true }]}>
            <Select options={[{ value: 'LOCAL', label: '本地链路' }, { value: 'REMOTE_EXECUTOR', label: '远程执行机' }]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.task_kind !== b.task_kind}>
            {({ getFieldValue }) =>
              getFieldValue('task_kind') === 'UPGRADE' ? (
                <Form.Item name="version_ref" label="目标交付版本引用"><Input placeholder="IGN-VER-xxxx" /></Form.Item>
              ) : null
            }
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.task_kind !== b.task_kind}>
            {({ getFieldValue }) =>
              getFieldValue('task_kind') === 'INSTALL' ? (
                <>
                  <Form.Item name="master_endpoints" label="主控入口(可空,成功落点 R-13 自动生成)"><Input placeholder="https://api.xxx:6443" /></Form.Item>
                  <Form.Item name="kubeconfig_ref" label="凭据存放标识(仅位置标识,INV-08)"><Input placeholder="vault://opic/clc/..." /></Form.Item>
                  <Form.Item name="fail_step_index" label="注入失败步骤(冒烟验证用,可空;R-09 写回集群)"><Input type="number" min={0} placeholder="如 3" /></Form.Item>
                </>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`集群销毁申请 → ${destroyTarget?.name || ''}(${destroyTarget?.cluster_no || ''})`}
        open={!!destroyTarget} width={680} onCancel={() => setDestroyTarget(null)}
        footer={[
          <Button key="draft" onClick={handleDestroyDraft}>保存草稿</Button>,
          <Button key="submit" type="primary" danger icon={<ThunderboltOutlined />} loading={destroying} onClick={handleDestroySubmit}>提交申请</Button>,
        ]}
      >
        <Alert
          type="warning" showIcon style={{ marginBottom: 12 }}
          message="影响面强制展示(R-10:销毁任务的前置 = 审批通过结论,强制不免审批)"
        />
        {impact ? (
          <Descriptions column={1} size="small" bordered style={{ marginBottom: 12 }}>
            <Descriptions.Item label="节点">{impact.node_count as number} 个</Descriptions.Item>
            <Descriptions.Item label="引用台账">{impact.binding_count as number} 项(通过后级联清理,R-07)</Descriptions.Item>
            <Descriptions.Item label="基础设施证书">{impact.cert_count as number} 项(销毁链吊销,失败挂起重试、残留可见)</Descriptions.Item>
            <Descriptions.Item label="销毁编排">{impact.destroy_orch as string}</Descriptions.Item>
          </Descriptions>
        ) : (
          <Alert type="info" showIcon message="影响面加载中或集群暂不可销毁" style={{ marginBottom: 12 }} />
        )}
        <Form form={destroyForm} layout="vertical">
          <Form.Item name="reason" label="强制原因(必填,MU el17)" rules={[{ required: true, message: '销毁是破坏性动作,强制原因必填' }]}>
            <Input.TextArea rows={3} placeholder="说明销毁理由与影响面确认结论" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
