// 集群域 · 运行观测(MU frmClcOps,QUERY_LIST):统计总览(FUNC-29)。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Statistic, Col, Row } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { opsApi, type ClcStatistics } from '../../api/clc'
import TableToolbar from '../../admin/components/TableToolbar'

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
        extra={<Button icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新</Button>}
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
    </Card>
  )
}
