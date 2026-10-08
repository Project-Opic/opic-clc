// 集群域 · 节点管理(MU frmClcNode,LIST_MAINTENANCE):节点清单 + 添加/移除/重绑定 + 登录标识。
// R-02 资产占用拦截、INV-13 REMOVED 终局、INV-12 裸机人工接管。
// v1.2 增量:el12「复制登录标识(行内按钮)」+ el13「登录标识(行内只读,点按复制时才出)」
//   = act-FUNC-47 / PERM-FUNC-47 / REP-16(MU:models/mu-ui-model.yaml frmClcNode);
//   el14「呈现档(按角色分组 ｜ 平铺,缺省分组)」+ el15「分组标题(台数 / 异常数)」
//   = REP-03 的呈现形态、不是第二张报表(5.16 口径 ⑰,9.2.2 / 9.2.6)。
import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Form, Input, Modal, Pagination, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CopyOutlined, MinusCircleOutlined, PlusOutlined, ReloadOutlined, SwapOutlined } from '@ant-design/icons'
import { clusterApi, nodeApi, wizardApi, type ClcLoginIdentity, type ClcNode } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import { useDictionaries } from '../../hooks/useDictionaries'
import TableToolbar from '../../admin/components/TableToolbar'
import { NODE_HEALTH, NODE_ROLE, NODE_STATUS, pill } from './common'

const ROLE_OPTIONS = Object.keys(NODE_ROLE).map((k) => ({ value: k, label: NODE_ROLE[k] }))
const STATUS_OPTIONS = Object.keys(NODE_STATUS).map((k) => ({ value: k, label: NODE_STATUS[k].label }))

// el12 复制动作 = 纯前端剪贴板写入(§9.2.7①:不发写请求、界面不出现「已留痕」类提示,
// 与 FUNC-49 原文视图的逐次留痕刻意分档 —— REP-16 / REP-18 口径注记)。
// navigator.clipboard 在非安全上下文(http 直连端口)下会 reject,故留一次 execCommand 兜底。
function writeClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    return navigator.clipboard.writeText(text).then(() => true).catch(() => Promise.resolve(execCopy(text)))
  }
  return Promise.resolve(execCopy(text))
}

function execCopy(text: string): boolean {
  try {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(area)
    return ok
  } catch {
    return false
  }
}

export default function NodeManage() {
  const hasPerm = usePermission()
  const [form] = Form.useForm()
  const [rebindForm] = Form.useForm()
  const [data, setData] = useState<ClcNode[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [filters, setFilters] = useState<{ cluster_ref?: string; node_role?: string; node_status?: string }>({})
  const [clusters, setClusters] = useState<{ id: number; cluster_no: string; name: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [addTarget, setAddTarget] = useState<ClcNode | null>(null)
  const [rebindTarget, setRebindTarget] = useState<ClcNode | null>(null)
  const [saving, setSaving] = useState(false)
  // el13 登录标识逐次点按取数:REP-16 不随清单一并下发(§9.9 第③条),已取到的行才在本行回显。
  const [identities, setIdentities] = useState<Record<string, ClcLoginIdentity>>({})
  const [identityBusy, setIdentityBusy] = useState('')
  // MU el14 呈现档:缺省按角色分组(9.2.6「默认以分组视图打开」),切档只换形态、不改筛选与分页语义(5.16 口径 ⑰)。
  const [viewMode, setViewMode] = useState('GROUP')
  // 分组轴与组头名称取 CLC_NODE_ROLE 词条序(字典已下发,界面不再自排一套角色序)。
  const dict = useDictionaries()

  // REP-16 的参数 p1「集群」必填(模型 m7 REP-16 parameters.p1 required:true),
  // 而节点清单行只有 cluster_ref(cluster_no),故用已加载的集群清单换算 :id。
  const clusterIdByNo = useMemo(
    () => Object.fromEntries(clusters.map((c) => [c.cluster_no, c.id])) as Record<string, number>,
    [clusters],
  )

  // MU el15 分组呈现:三档各归各档——异常=UNREACHABLE(探测过且不可达),UNKNOWN(未知)是没探测过,
  // 既不算健康也不算异常;否则刚登记的整组节点会读成「全组异常」,把管理员引去查并不存在的事件。
  // 异常行置顶只在加载时做一次,组内行序不随健康事件重排(9.2.6 ③)——数据行刷新才会重排,推送刷新不动序。
  const healthRank = (h: string) => (h === 'UNREACHABLE' ? 0 : h === 'HEALTHY' ? 1 : 2)
  const orderedRows = useMemo(
    () => [...data].sort((a, b) => healthRank(a.node_health || 'UNKNOWN') - healthRank(b.node_health || 'UNKNOWN')),
    [data],
  )
  const roleGroups = useMemo(() => {
    const buckets = new Map<string, ClcNode[]>()
    orderedRows.forEach((r) => {
      const key = r.node_role || '-'
      buckets.set(key, [...(buckets.get(key) || []), r])
    })
    // 组序 = CLC_NODE_ROLE 词条序;字典里没有的角色(含空值)排在末尾并按码值显示
    const axis = dict.itemsOf('CLC_NODE_ROLE').map((i) => i.code)
    const keys = [...axis.filter((k) => buckets.has(k)), ...[...buckets.keys()].filter((k) => !axis.includes(k))]
    return keys.map((role) => {
      const rows = buckets.get(role) || []
      return {
        role,
        label: dict.labelOf('CLC_NODE_ROLE', role),
        count: rows.length,
        healthy: rows.filter((r) => r.node_health === 'HEALTHY').length,
        abnormal: rows.filter((r) => r.node_health === 'UNREACHABLE').length,
        unknown: rows.filter((r) => !['HEALTHY', 'UNREACHABLE'].includes(r.node_health)).length,
        rows,
      }
    })
  }, [orderedRows, dict])

  const load = async (p = page, s = size, f = filters) => {
    setLoading(true)
    setError('')
    // 逐次点按取数:清单刷新即清空已取到的标识,界面不缓存 REP-16 结果(不随清单一并下发)
    setIdentities({})
    try {
      const res = await nodeApi.list({ page: p, size: s, ...f })
      setData(res.list || [])
      setTotal(res.total || 0)
    } catch (e) {
      setError((e as Error).message || '节点清单加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    clusterApi.list({ page: 1, size: 100 }).then((r) => setClusters((r.list || []).map((c) => ({ id: c.id, cluster_no: c.cluster_no, name: c.name })))).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size])

  const handleAdd = async () => {
    if (!addTarget) return
    const values = await form.validateFields()
    setSaving(true)
    try {
      await clusterApi.addNode(values.cluster_id, values)
      message.success('节点已登记为待加入,ADD_NODE 任务排队执行(成功后回填 K8s 节点名 INV-14)')
      setAddTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    } finally {
      setSaving(false)
    }
  }

  const handleRebind = async () => {
    if (!rebindTarget) return
    const values = await rebindForm.validateFields()
    try {
      await nodeApi.rebind(rebindTarget.id, values)
      message.success('节点已重绑定(R-12)')
      setRebindTarget(null)
      load()
    } catch (e) {
      if ((e as { errorFields?: unknown }).errorFields) return
    }
  }

  // el12 复制登录标识(act-FUNC-47 / PERM-FUNC-47 / REP-16):逐次点按才取数、才回显,
  // 取到的只有「登录用户@节点地址」(REF-18 固定口径)—— 拼出口令/私钥的任何形态都不存在本域。
  const handleCopyIdentity = async (row: ClcNode) => {
    const clusterId = clusterIdByNo[row.cluster_ref]
    if (clusterId === undefined) {
      message.warning(`未能定位节点所属集群(REP-16 的「集群」参数必填),请在上方「集群」筛选中选择后重试`)
      return
    }
    setIdentityBusy(row.node_no)
    try {
      const res = await wizardApi.nodeLoginIdentities(clusterId)
      const item = (res.items || []).find((it) => it.node_no === row.node_no)
      if (!item || !item.login_identity) {
        message.warning('本行不在登录标识名录(REP-16 只列在册且未移除的节点 INV-13)')
        return
      }
      setIdentities((prev) => ({ ...prev, [row.node_no]: item }))
      const ok = await writeClipboard(item.login_identity)
      message.success(
        ok
          ? `登录标识已复制到剪贴板(${item.login_identity},只出用户与地址、不含任何凭据)`
          : `浏览器剪贴板不可用,请手动选中复制登录标识(${item.login_identity})`,
      )
    } catch { /* 统一拦截器已直出后端中文 errorMsg */ } finally {
      setIdentityBusy('')
    }
  }

  // el12/el13 登录标识列(MU frmClcNode;FUNC-47 / PERM-FUNC-47 / REP-16)。
  // 本列只有两种出口:点按才取的「登录用户@节点地址」串(REF-18)与旁注的 R-03 接管注记;
  // 本屏没有终端 / WebSSH 位(INV-47 / §1.2.2 第 8 行 —— 浏览器里能敲命令就会击穿凭据零持久化与白名单动作)。
  const identityColumn: ColumnsType<ClcNode> = hasPerm('PERM-FUNC-47') ? [{
    title: (
      <Tooltip title="REP-16 / REF-18:平台配置的 SSH 全局登录用户@节点地址(即时拼接、不落库);只出标识、零凭据出口(INV-08);复制=前端剪贴板写入、不留痕(与 FUNC-49 原文视图的逐次留痕刻意分档);本屏无终端入口(INV-47)">
        <span>登录标识(用户@地址)</span>
      </Tooltip>
    ),
    key: 'login_identity',
    width: 250,
    render: (_, row) => {
      if (row.node_status === 'REMOVED') return <span style={{ color: '#8c8c8c' }}>-（REMOVED 终局行不出标识 INV-13）</span>
      const item = identities[row.node_no]
      return (
        <Space direction="vertical" size={2}>
          <Space size={4} wrap>
            {item
              ? <Typography.Text code>{item.login_identity}</Typography.Text>
              : <span style={{ color: '#8c8c8c' }}>—（点按复制时才出）</span>}
            {item?.takeover && (
              <Tooltip title={item.takeover_note || 'R-03 人工接管值(资产引用已置空、地址保留)'}>
                <Tag color="warning" style={{ marginInlineEnd: 0 }}>人工接管</Tag>
              </Tooltip>
            )}
          </Space>
          <Button type="link" size="small" style={{ padding: 0, height: 22 }} icon={<CopyOutlined />}
            loading={identityBusy === row.node_no} onClick={() => handleCopyIdentity(row)}>复制登录标识</Button>
        </Space>
      )
    },
  }] : []

  const columns: ColumnsType<ClcNode> = [
    { title: '节点编号', dataIndex: 'node_no', key: 'node_no', width: 170 },
    { title: '集群', dataIndex: 'cluster_ref', key: 'cluster_ref', width: 150, render: (v: string, r) => r.cluster_name ? `${r.cluster_name}(${v})` : v },
    { title: '角色', dataIndex: 'node_role', key: 'node_role', width: 90, render: (v: string) => NODE_ROLE[v] || v },
    { title: 'IP', dataIndex: 'ip_address', key: 'ip_address', width: 130 },
    { title: 'K8s 节点名', dataIndex: 'k8s_nodename', key: 'k8s_nodename', render: (v: string | null) => v || '-' },
    { title: '资产引用', dataIndex: 'asset_ref', key: 'asset_ref', width: 140, render: (v: string | null) => v || <span style={{ color: '#8c8c8c' }}>裸机(IP 人工接管 INV-12)</span> },
    { title: '状态', dataIndex: 'node_status', key: 'node_status', width: 90, render: (v: string) => pill(NODE_STATUS, v) },
    { title: '健康(时间)', key: 'health', width: 140, render: (_, r) => <Space size={4}>{pill(NODE_HEALTH, r.node_health)}<span style={{ fontSize: 12, color: '#8c8c8c' }}>{r.node_health_at || ''}</span></Space> },
    // MU el13「登录标识(行内只读,点按复制时才出)」+ el12「复制登录标识(行内按钮)」:
    // 无 PERM-FUNC-47 时整列不渲染(发牌按屏,REP-16 对四角色全开,§9.2.4)。
    ...(identityColumn),
    {
      title: '操作', key: 'action', width: 170, fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          {hasPerm('PERM-FUNC-08') && (row.node_status === 'ADDED' || row.node_status === 'PENDING') && (
            <Button type="link" size="small" icon={<SwapOutlined />} onClick={() => { setRebindTarget(row); rebindForm.resetFields() }}>重绑定</Button>
          )}
          {hasPerm('PERM-FUNC-07') && row.node_status !== 'REMOVED' && (
            <Popconfirm title="移除该节点?REMOVED 为终局(INV-13),重新加入须登记新行"
              onConfirm={async () => {
                try {
                  await nodeApi.remove(row.id)
                  message.success('移除已受理,节点置移除中(DEL_NODE 任务成功后置 REMOVED,终局保留行)')
                  load()
                } catch { /* 拦截器已提示 */ }
              }}>
              <Button type="link" size="small" danger icon={<MinusCircleOutlined />}>移除</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Card>
      <TableToolbar
        title="节点管理"
        description="节点清单:同一集群内 IP 唯一(INV-10);资产占用拦截(R-02 一台资产只服务一个活跃集群);移除即终局(INV-13);v1.2 行内登录标识只出「用户@地址」(REP-16/REF-18 零凭据出口),本屏不做浏览器内终端(INV-47)"
        total={total}
        extra={
          <Space wrap>
            <Select allowClear showSearch placeholder="集群" style={{ width: 190 }}
              options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}(${c.cluster_no})` }))}
              optionFilterProp="label"
              onChange={(v) => { const f = { ...filters, cluster_ref: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="节点角色" style={{ width: 130 }} options={ROLE_OPTIONS}
              onChange={(v) => { const f = { ...filters, node_role: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            <Select allowClear placeholder="状态" style={{ width: 120 }} options={STATUS_OPTIONS}
              onChange={(v) => { const f = { ...filters, node_status: v }; setFilters(f); setPage(1); load(1, size, f) }} />
            {/* MU el14 呈现档:纯形态切换,不进 filters、不参与请求参数(9.2.6「切回平铺不改变筛选语义」) */}
            <Tooltip title="REP-03 的呈现形态,不是第二张报表(5.16 口径 ⑰);分组与组头计数按当前页已加载的行算">
              <Select value={viewMode} style={{ width: 150 }} onChange={setViewMode}
                options={[{ value: 'GROUP', label: '按角色分组' }, { value: 'FLAT', label: '平铺' }]} />
            </Tooltip>
            <Button icon={<ReloadOutlined />} onClick={() => load()}>刷新</Button>
            {hasPerm('PERM-FUNC-06') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => { setAddTarget({ id: 0 } as ClcNode); form.resetFields() }}>添加节点</Button>
            )}
          </Space>
        }
      />
      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error}
          action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      {viewMode === 'FLAT' ? (
        <Table
          rowKey="id" columns={columns} dataSource={orderedRows} loading={loading} scroll={{ x: 1450 }}
          pagination={{
            current: page, pageSize: size, total, showSizeChanger: true,
            onChange: (p, s) => { setPage(p); setSize(s) },
          }}
        />
      ) : (
        <>
          {/* MU el15 分组标题:组序 = CLC_NODE_ROLE 词条序,台数/各档数由已加载行算得;
              三档名一律取 CLC_NODE_HEALTH 词条(界面不留第二份标签表,DEF-09 同一口径) */}
          {roleGroups.map((g) => (
            <div key={g.role} style={{ marginBottom: 14 }}>
              <Space size={8} style={{ marginBottom: 6 }}>
                <Typography.Text strong>分组：{g.label}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  ｜ {g.count} 台（{dict.labelOf('CLC_NODE_HEALTH', 'HEALTHY')} {g.healthy} · {dict.labelOf('CLC_NODE_HEALTH', 'UNREACHABLE')} {g.abnormal}
                  {g.unknown > 0 && ` · ${dict.labelOf('CLC_NODE_HEALTH', 'UNKNOWN')} ${g.unknown}`}）
                </Typography.Text>
              </Space>
              <Table rowKey="id" size="small" columns={columns} dataSource={g.rows} loading={loading} pagination={false} scroll={{ x: 1450 }} />
            </div>
          ))}
          {!roleGroups.length && !loading && (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前筛选下没有节点行(REP-03 清单为空)" />
          )}
          {/* 分页仍是 REP-03 的服务端分页;换档不改分页语义(9.2.6)。翻页由 [page,size] 的 effect 统一取数。 */}
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
            <Pagination
              current={page} pageSize={size} total={total} showSizeChanger
              showTotal={(t) => `共 ${t} 条${t > data.length ? `（分组按本页已加载的 ${data.length} 条算）` : ''}`}
              onChange={(p, s) => { setPage(p); setSize(s) }}
            />
          </div>
        </>
      )}
      <Modal
        title="添加节点(选择目标集群;交付编排发起 ADD_NODE)"
        open={!!addTarget} onOk={handleAdd} onCancel={() => setAddTarget(null)} confirmLoading={saving} width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择目标集群' }]}>
            <Select showSearch optionFilterProp="label" placeholder="选择集群"
              options={clusters.map((c) => ({ value: c.id, label: `${c.name}(${c.cluster_no})` }))} />
          </Form.Item>
          <Form.Item name="ip_address" label="IP 地址(必填;裸机凭 IP 人工接管 INV-12)" rules={[{ required: true, message: '请输入 IP 地址' }]}>
            <Input placeholder="如 10.26.8.101" />
          </Form.Item>
          <Form.Item name="node_role" label="节点角色(INV-11 字典)" rules={[{ required: true, message: '请选择角色' }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="asset_ref" label="资产引用(可空 = 裸机;R-02 占用拦截)">
            <Input placeholder="AST-xxxx(可空)" />
          </Form.Item>
          <Form.Item name="k8s_nodename" label="K8s 节点名(可空,成功后自动回填)">
            <Input placeholder="可空" />
          </Form.Item>
          <Form.Item name="remark" label="备注"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`节点重绑定 ${rebindTarget?.node_no || ''}(R-12:新资产未被占用且登记值可反查)`}
        open={!!rebindTarget} onOk={handleRebind} onCancel={() => setRebindTarget(null)} width={480}
      >
        <Form form={rebindForm} layout="vertical">
          <Form.Item name="new_asset_ref" label="新资产引用" rules={[{ required: true, message: '请输入新资产引用' }]}>
            <Input placeholder="AST-xxxx" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
