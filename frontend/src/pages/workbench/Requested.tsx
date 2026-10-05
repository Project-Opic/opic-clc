// 我的申请(clc 口径:我提交的集群销毁审批单;只读查看,审批期间集群状态不变)。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined } from '@ant-design/icons'
import { workbenchApi, type RequestedItem } from '../../api/flow'
import TableToolbar from '../../admin/components/TableToolbar'
import { InstanceStatusPill } from '../../admin/components/StatusPill'
import { CLUSTER_STATUS, pill } from '../clc/common'

export default function Requested() {
  const [data, setData] = useState<RequestedItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = async (p = page, s = size) => {
    setLoading(true)
    setError('')
    try {
      const res = await workbenchApi.requested({ page: p, size: s })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '我的申请加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const columns: ColumnsType<RequestedItem> = [
    { title: '集群编号', dataIndex: 'business_key', key: 'business_key', width: 170 },
    { title: '集群名称', dataIndex: 'cluster_name', key: 'cluster_name', render: (v) => v || '-' },
    {
      title: '集群状态',
      dataIndex: 'cluster_status',
      key: 'cluster_status',
      width: 110,
      render: (v: string | null | undefined) => (v ? pill(CLUSTER_STATUS, v) : '-'),
    },
    { title: '强制原因', dataIndex: 'destroy_reason', key: 'destroy_reason', ellipsis: true, render: (v) => v || '-' },
    {
      title: '审批状态',
      dataIndex: 'flow_status',
      key: 'flow_status',
      width: 130,
      render: (v: string | undefined) => (v ? <InstanceStatusPill status={v} /> : '-'),
    },
    { title: '发起时间', dataIndex: 'started_at', key: 'started_at', width: 180, render: (v) => v || '-' },
  ]

  return (
    <Card>
      <TableToolbar
        title="我的申请"
        description="我提交的集群销毁审批单(R-10 强制不免审批,审批期间集群状态不变)"
        total={total}
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => load()}>
            刷新
          </Button>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      <Table
        rowKey="id" columns={columns} dataSource={data} loading={loading}
        pagination={{
          current: page, pageSize: size, total, showSizeChanger: true,
          onChange: (p, s) => { setPage(p); setSize(s) },
        }}
      />
    </Card>
  )
}
