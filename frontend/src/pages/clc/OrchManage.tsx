// 集群域 · 交付任务(MU frmClcOrch,MASTER_DETAIL_FORM + 审核界面形态):全局任务台账 + 下钻流水线 + 取消 + 销毁审批(影响面强制展示)。
// v1.3 收口(Q-121):「发起交付动作」表单(发起安装/升级/备份三个工具栏按钮)退出本屏,动作入口收口进集群台「安装」「生命周期」页签;
//   本屏仍是 FUNC-03/04/09~12/05 的主登记屏(登记的是权限与行为归属,不是按钮坐标),「续跑/取消/重试换机」是台账行内操作、原样保留。
// act-FUNC-33 取消(二次确认+原因必填)、act-FUNC-34/35 步骤/日志行内入口(详情抽屉三段式)。
// R-10 销毁强制不免审批;R-09 失败步骤索引写回;INV-28 STALLED 与 FAILED 分列;取消 ≠ 回滚(R-15);步骤权威在后端(裁定二)。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, DeleteOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { clusterApi, taskApi, type ClcTask } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import TaskDetailDrawer, { type TaskDetailDrawerProps } from './TaskDetail'
import { EXEC_PATH, TASK_FAIL_REASON, TASK_KIND, TASK_STATUS, pill, type ConsoleCtx } from './common'

const KIND_OPTIONS = Object.keys(TASK_KIND).map((k) => ({ value: k, label: TASK_KIND[k] }))

/** act-FUNC-33 前置(m2):仅 QUEUED/RUNNING 可取消、DESTROY 拒绝、终态不渲染入口 */
function cancellable(t: ClcTask): boolean {
  return (t.task_status === 'QUEUED' || t.task_status === 'RUNNING') && t.task_kind !== 'DESTROY'
}

// 集群台嵌入态(v1.3):「日志」页签 = 本集群 scope 的 REP-07 行列表 + REP-13 回放面板(9.10.2,同一报表第二个呈现入口)。
// 集群由台级唯一确定 ⇒ 不出集群选择器、cluster_ref 固定;销毁入口在「生命周期」页签,嵌入态不渲染(9.10.1 动作卡)。
export default function OrchManage({ ctx }: { ctx?: ConsoleCtx } = {}) {
  const hasPerm = usePermission()
  const [destroyForm] = Form.useForm()
  const [cancelForm] = Form.useForm<{ reason: string }>()
  const [data, setData] = useState<ClcTask[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; task_kind?: string; task_status?: string }>(
    () => (ctx ? { cluster_ref: ctx.cluster.cluster_no } : {}),
  )
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string; status: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [destroyTarget, setDestroyTarget] = useState<{ id: number; cluster_no: string; name: string } | null>(null)
  const [impact, setImpact] = useState<Record<string, unknown> | null>(null)
  const [destroying, setDestroying] = useState(false)
  const [detail, setDetail] = useState<{ task: ClcTask; focus: TaskDetailDrawerProps['focus'] } | null>(null)
  const [cancelTarget, setCancelTarget] = useState<ClcTask | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const cancelReason = Form.useWatch('reason', cancelForm)

  // 独立形态的销毁入口只对着「已选定集群」开：原实现取 clusters[0]，等于把破坏性动作指向清单里恰好排第一的集群。
  const destroyCluster = clusters.find((c) => c.cluster_no === filters.cluster_ref)

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
    if (ctx) return
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name, status: c.status })))).catch(() => undefined)
  }

  useEffect(() => {
    load()
    loadClusters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

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
      load()
      ctx?.refresh()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setDestroying(false)
    }
  }

  // act-FUNC-33 取消:原因必填(不填不可提交)→ Modal.confirm 二次确认 → 提交(失败直出后端中文 errorMsg)
  const openCancel = (task: ClcTask) => {
    setCancelTarget(task)
    cancelForm.resetFields()
  }

  const handleCancelSubmit = async () => {
    if (!cancelTarget) return
    const { reason } = await cancelForm.validateFields()
    Modal.confirm({
      title: `二次确认:取消任务 ${cancelTarget.task_no}`,
      content: `取消 ≠ 回滚(R-15),已执行步骤的效果由交付工具幂等性承担。取消原因:${reason.trim()}`,
      okText: '确认取消任务',
      okButtonProps: { danger: true },
      onOk: async () => {
        setCancelling(true)
        try {
          await taskApi.cancel(cancelTarget.task_no, { reason: reason.trim() })
          message.success('任务已取消(取消不是回滚,已执行步骤的效果由交付工具幂等性承担)')
          setCancelTarget(null)
          load()
          ctx?.refresh()
        } finally {
          setCancelling(false)
        }
      },
    })
  }

  // `REP-07` 的登记列结构（5.8）不含集群列；台内「日志」页签由徽章条唯一确定集群 ⇒ 不渲染，全局台账形态保留。
  const clusterColumn: ColumnsType<ClcTask> = ctx ? [] : [
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
  ]

  const columns: ColumnsType<ClcTask> = [
    { title: '任务编号', dataIndex: 'task_no', key: 'task_no', width: 170 },
    ...clusterColumn,
    { title: '类别', dataIndex: 'task_kind', key: 'task_kind', width: 90, render: (v: string) => TASK_KIND[v] || v },
    { title: '执行位置', dataIndex: 'execution_path', key: 'execution_path', width: 100, render: (v: string) => EXEC_PATH[v] || v },
    { title: '状态', dataIndex: 'task_status', key: 'task_status', width: 90, render: (v: string) => pill(TASK_STATUS, v) },
    { title: '失败原因', dataIndex: 'fail_reason', key: 'fail_reason', width: 110, render: (v: string | null) => (v ? TASK_FAIL_REASON[v] || v : '-') },
    { title: '失败步骤', dataIndex: 'fail_step_index', key: 'fail_step_index', width: 80, render: (v: number | null) => (v === null || v === undefined ? '-' : v) },
    { title: '结果摘要', dataIndex: 'result_summary', key: 'result_summary', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '起止时间', key: 'time', width: 260, render: (_, r) => `${r.started_at || '-'} ~ ${r.ended_at || '-'}` },
    { title: '提交人', dataIndex: 'submitted_by', key: 'submitted_by', width: 100 },
    {
      title: '操作',
      key: 'actions',
      width: 210,
      fixed: 'right',
      render: (_, r) => (
        <Space size={0} wrap>
          {hasPerm('PERM-FUNC-34') && (
            <Button size="small" type="link" onClick={() => setDetail({ task: r, focus: 'steps' })}>查看步骤</Button>
          )}
          {hasPerm('PERM-FUNC-35') && (
            <Button size="small" type="link" onClick={() => setDetail({ task: r, focus: 'logs' })}>查看日志</Button>
          )}
          {hasPerm('PERM-FUNC-33') && cancellable(r) && (
            <Button size="small" type="link" danger onClick={() => openCancel(r)}>取消</Button>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Card>
      <TableToolbar
        title="交付任务"
        description="交付动作经任务台账留痕(只增不改 INV-31);安装失败携带失败步骤索引并写回集群(R-09/INV-30);销毁强制不免审批(R-10);v1.3 起发起交付动作的表单收口进集群台,本屏是台账 + 行内续跑/取消/下钻"
        total={total}
        extra={
          <Space wrap>
            {!ctx && (
              <Select allowClear showSearch placeholder="集群" style={{ width: 190 }} optionFilterProp="label"
                options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
                onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            )}
            <Select allowClear placeholder="任务类别" style={{ width: 130 }} options={KIND_OPTIONS}
              onChange={(v) => { const f = { ...filters, task_kind: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="状态" style={{ width: 120 }}
              options={Object.keys(TASK_STATUS).map((k) => ({ value: k, label: TASK_STATUS[k].label }))}
              onChange={(v) => { const f = { ...filters, task_status: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {/* v1.3 Q-121:发起交付动作表单退出本屏(动作入口在集群台「安装/生命周期」页签);销毁在「生命周期」页签有同名入口,嵌入态不渲染 */}
            {!ctx && hasPerm('PERM-FUNC-05') && !!destroyCluster && (
              <Button danger icon={<DeleteOutlined />} onClick={() => openDestroy(destroyCluster)}>集群销毁</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1560 }}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
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
      <Modal
        title={`取消任务 → ${cancelTarget?.task_no || ''}`}
        open={!!cancelTarget}
        okText="进入二次确认"
        okButtonProps={{ danger: true, disabled: !cancelReason || !cancelReason.trim() }}
        confirmLoading={cancelling}
        onOk={handleCancelSubmit}
        onCancel={() => setCancelTarget(null)}
        width={520}
      >
        <Alert
          type="warning" showIcon style={{ marginBottom: 12 }}
          message="取消 ≠ 回滚(R-15):已执行步骤的效果由交付工具幂等性承担;DESTROY 类任务不可取消;原因写入审计(R-14)"
        />
        <Form form={cancelForm} layout="vertical">
          <Form.Item name="reason" label="取消原因(必填,act-FUNC-33)" rules={[{ required: true, whitespace: true, message: '取消原因必填,不填不可提交' }]}>
            <Input.TextArea rows={3} placeholder="如:变更窗口调整,暂停本次交付" />
          </Form.Item>
        </Form>
      </Modal>
      {detail && (
        <TaskDetailDrawer
          key={detail.task.task_no}
          task={detail.task}
          focus={detail.focus}
          onClose={() => setDetail(null)}
        />
      )}
    </Card>
  )
}
