// 集群域 · 运行观测(MU frmClcOps,QUERY_LIST,屏名「统计与参数」):统计总览(FUNC-29)+ 运行参数(FUNC-30)。
// v1.3 收口(Q-116 / 9.7.2):平台参数原先是独立菜单叶 + 独立屏(实现期多出,MU 从未登记该屏),
// 现按元素表 el07 legacy 开关 / el08 快照保留期 / el09 保存运行参数收回本屏的运行参数区段。
// 两条数据腿各自成五态:统计读需 PERM-FUNC-29(即本菜单叶的入口牌),参数读登录即可、写需 PERM-FUNC-30。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Col, Input, Row, Space, Statistic, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { opsApi, type ClcStatistics } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'

interface ParamRow {
  param_key: string
  param_value: string
  param_group: string
  description: string
  updated_at: string
}

function ParamsSection() {
  const hasPerm = usePermission()
  const [data, setData] = useState<ParamRow[]>([])
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setData(await opsApi.params())
      setDraft({})
    } catch (e) {
      setError((e as Error).message || '运行参数加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const save = async () => {
    if (Object.keys(draft).length === 0) {
      message.info('没有修改')
      return
    }
    setSaving(true)
    try {
      await opsApi.saveParams(draft)
      message.success('运行参数已保存(仅允许已登记键;热生效范围见参数说明)')
      load()
    } catch {
      /* 拦截器已提示 */
    } finally {
      setSaving(false)
    }
  }

  const columns: ColumnsType<ParamRow> = [
    { title: '参数键', dataIndex: 'param_key', key: 'param_key', width: 280 },
    {
      title: '参数值', dataIndex: 'param_value', key: 'param_value', width: 240,
      render: (v: string, r) =>
        hasPerm('PERM-FUNC-30') ? (
          <Input size="small" value={draft[r.param_key] ?? v}
            onChange={(e) => setDraft({ ...draft, [r.param_key]: e.target.value })} />
        ) : (
          v
        ),
    },
    { title: '分组', dataIndex: 'param_group', key: 'param_group', width: 100 },
    { title: '说明', dataIndex: 'description', key: 'description', ellipsis: true },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180 },
  ]

  return (
    <Card
      size="small"
      title="运行参数（REF-01~12 规则级平台配置）"
      extra={
        <Space>
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新</Button>
          {hasPerm('PERM-FUNC-30') && (
            <Button size="small" type="primary" icon={<SaveOutlined />} loading={saving} onClick={save}>保存运行参数</Button>
          )}
        </Space>
      }
    >
      {error ? (
        <Alert type="error" showIcon message={error} action={<Button size="small" onClick={load}>重试</Button>} />
      ) : (
        <Table rowKey="param_key" columns={columns} dataSource={data} loading={loading} pagination={false} size="small" />
      )}
    </Card>
  )
}

export default function OpsManage() {
  const [data, setData] = useState<ClcStatistics | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setData(await opsApi.statistics())
    } catch (e) {
      setError((e as Error).message || '统计总览加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Card>
      <TableToolbar
        title="运行观测"
        description="集群/健康/待同步/探活/销毁吊销统计总览(REP-11);健康缓存 60s 防下游穿透(REF-07)"
        total={data ? 1 : 0}
        extra={<Button icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新统计</Button>}
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={load}>重试</Button>} />
      )}
      <Row gutter={[16, 16]}>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="集群总数" value={data?.total ?? 0} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="运行中" value={data?.running ?? 0} valueStyle={{ color: '#3f8600' }} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="降级" value={data?.degraded ?? 0} valueStyle={{ color: '#d46b08' }} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="不可达" value={data?.unreachable ?? 0} valueStyle={{ color: '#cf1322' }} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="已销毁" value={data?.destroyed ?? 0} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="参数待同步" value={data?.sync_pending ?? 0} valueStyle={{ color: '#d46b08' }} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="探活样本(近 100)" value={data?.probe_total ?? 0} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="探活失败率" value={data?.probe_fail_rate ?? '-'} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="挂起吊销证书" value={data?.revocation_pending ?? 0} valueStyle={{ color: '#cf1322' }} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic loading={loading} title="快照总数" value={data?.snapshot_total ?? 0} /></Card></Col>
      </Row>
      <div style={{ marginTop: 16 }}>
        <ParamsSection />
      </div>
    </Card>
  )
}
