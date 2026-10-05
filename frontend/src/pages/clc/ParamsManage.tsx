// 集群域 · 平台参数(FUNC-30 运行参数维护):REF-01~12 规则级平台配置。
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Input, Table, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { opsApi } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'

interface ParamRow {
  param_key: string
  param_value: string
  param_group: string
  description: string
  updated_at: string
}

export default function ParamsManage() {
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
      setError((e as Error).message || '平台参数加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleSave = async () => {
    if (Object.keys(draft).length === 0) {
      message.info('没有修改')
      return
    }
    setSaving(true)
    try {
      await opsApi.saveParams(draft)
      message.success('运行参数已保存(仅允许已登记键)')
      load()
    } catch { /* 拦截器已提示 */ } finally {
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
    <Card>
      <TableToolbar
        title="平台参数"
        description="规则级平台配置(REF-01 legacy 开关 / REF-02 快照保留期 / REF-03 探活阈值 / REF-11 防抖阈值等);TEXT 值按参数语义校验"
        total={data.length}
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>
            {hasPerm('PERM-FUNC-30') && (
              <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={handleSave}>保存运行参数</Button>
            )}
          </>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={load}>重试</Button>} />
      )}
      <Table rowKey="param_key" columns={columns} dataSource={data} loading={loading} pagination={false} />
    </Card>
  )
}
