// 集群域 · 集群台（MU frmClcClusterConsole，v1.3 新增宿主屏，MASTER_DETAIL_FORM ｜ 无菜单叶，由集群台账行操作「台」进入）。
// 主 = 一个集群的头部徽章条（REP-02，链路事实唯一呈现位，INV-32）；从 = 五页签组（配置｜安装｜生命周期｜节点｜日志）。
// 本屏是纯装配层：零新增功能点 / 报表 / 权限码 / 字典（9.0 第四条纪律「载体置换不复制语义」），
// 页签内容一律渲染既有屏组件本体（回指 9.2 / 9.4~9.6 / 9.9 / 9.3），不在这里抄第二份。
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Descriptions, Space, Spin, Tabs, Tag, Typography } from 'antd'
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { clusterApi, taskApi, type ClcCluster, type ClcTask } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import { useDictionaries } from '../../hooks/useDictionaries'
import {
  CLUSTER_HEALTH, CLUSTER_STATUS, DEGRADED_REASON, LIVE_TASK_STATUSES, RUN_MODE,
  pill, type ConsoleCtx,
} from './common'
import NodeManage from './NodeManage'
import RefManage from './RefManage'
import SnapManage from './SnapManage'
import HealthManage from './HealthManage'
import OrchManage from './OrchManage'
import WizardConsole, { IgnConfigFieldView, IgnConfigRawView, LifecycleConsole } from './WizardConsole'

const TAB_KEYS = ['config', 'install', 'lifecycle', 'node', 'log'] as const
type TabKey = (typeof TAB_KEYS)[number]

export default function ClusterConsole() {
  const { id } = useParams()
  const navigate = useNavigate()
  const hasPerm = usePermission()
  const dict = useDictionaries()
  const [searchParams, setSearchParams] = useSearchParams()

  const clusterId = Number(id)
  const [cluster, setCluster] = useState<ClcCluster | null>(null)
  const [tasks, setTasks] = useState<ClcTask[]>([])
  const [missing, setMissing] = useState(false)
  const [loading, setLoading] = useState(true)

  // REP-02 徽章条 + REP-07 本集群 scope 的任务行（台级持有一份，「安装」「日志」两页签共用，不在页签内各拉一次）
  const reloadCluster = useCallback(() => {
    if (!Number.isFinite(clusterId) || !hasPerm('PERM-FUNC-20')) return
    clusterApi
      .get(clusterId)
      .then((c) => {
        setCluster(c)
        setMissing(false)
      })
      .catch(() => setMissing(true))
      .finally(() => setLoading(false))
  }, [clusterId, hasPerm])

  const reloadTasks = useCallback(
    (clusterNo: string) => {
      if (!clusterNo || !hasPerm('PERM-FUNC-25')) return
      taskApi
        .list({ cluster_ref: clusterNo, page: 1, size: 20 })
        .then((r) => setTasks(r.list || []))
        .catch(() => undefined)
    },
    [hasPerm],
  )

  const refresh = useCallback(() => {
    reloadCluster()
    reloadTasks(cluster?.cluster_no || '')
  }, [reloadCluster, reloadTasks, cluster?.cluster_no])

  useEffect(() => {
    setLoading(true)
    reloadCluster()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId])

  useEffect(() => {
    if (cluster) reloadTasks(cluster.cluster_no)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster?.cluster_no])

  const attached = useMemo<ClcTask | null>(
    () => tasks.find((t) => LIVE_TASK_STATUSES.includes(t.task_status)) ?? tasks[0] ?? null,
    [tasks],
  )
  const hasLive = !!attached && LIVE_TASK_STATUSES.includes(attached.task_status)

  // 9.10.7 ①：徽章条随在途任务刷新（本地查询，跨中心日志腿只在「安装」页签可见时挂载 —— Tabs destroyOnHidden）
  useEffect(() => {
    if (!hasLive || !cluster) return
    const timer = window.setInterval(refresh, 5000)
    return () => window.clearInterval(timer)
  }, [hasLive, cluster, refresh])

  // 页签可见性按页签主数据权限码判（DEF-06 口径，不为页签签发新码）；缺位页签不进序列（9.10.2）
  const tabVisible: Record<TabKey, boolean> = {
    // 配置页签承载：集群详情 + 引用与参数 + 交付配置字段级代理（FUNC-48 随 v1.3 收口上移到这里）
    config: hasPerm(['PERM-FUNC-20', 'PERM-FUNC-22', 'PERM-FUNC-23', 'PERM-FUNC-48']),
    // 判据只取「安装」页签自身承载的动作(9.10.1 / 9.10.2:步进/跳步/预检/原文只读/远程日志腿)——
    // 步进器与日志的数据读牌(FUNC-34/35)同时挂在「日志」页签上,把它们算进本档会让 CLC-ROLE-02
    // 依 9.10.4 不该出现的安装页签浮出(它只有 25/34/35,没有任何向导屏动作牌)。
    install: hasPerm(['PERM-FUNC-44', 'PERM-FUNC-45', 'PERM-FUNC-49', 'PERM-FUNC-50', 'PERM-FUNC-51']),
    lifecycle: hasPerm(['PERM-FUNC-24', 'PERM-FUNC-26', 'PERM-FUNC-46', 'PERM-FUNC-09', 'PERM-FUNC-10', 'PERM-FUNC-11', 'PERM-FUNC-12', 'PERM-FUNC-05']),
    node: hasPerm(['PERM-FUNC-21']),
    log: hasPerm(['PERM-FUNC-25', 'PERM-FUNC-35', 'PERM-FUNC-51']),
  }
  const visibleTabs = TAB_KEYS.filter((k) => tabVisible[k])

  // 9.10.6 缺省页签派生：一生刚开始 / 卡住的交付 → 安装；看现状 → 配置
  const defaultTab: TabKey | undefined = useMemo(() => {
    if (!cluster) return undefined
    const stalled = tasks.some((t) => t.task_status === 'STALLED')
    const derived: TabKey =
      cluster.status === 'INSTALL_FAILED' || stalled || ['DRAFT', 'INSTALLING'].includes(cluster.status)
        ? 'install'
        : 'config'
    if (tabVisible[derived]) return derived
    return visibleTabs[0]
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster?.status, tasks, visibleTabs.join(',')])

  const urlTab = searchParams.get('tab') as TabKey | null
  const activeTab = urlTab && tabVisible[urlTab] ? urlTab : defaultTab

  const onTabChange = (key: string) => {
    setSearchParams(key === defaultTab ? {} : { tab: key }, { replace: true })
  }

  // 9.10.4：深链直达无权限页签 ⇒ 页签不渲染、落回缺省页签；地址栏同步落回，否则刷新仍是同一条越权深链
  useEffect(() => {
    if (!cluster || !urlTab || tabVisible[urlTab]) return
    setSearchParams({}, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, urlTab, visibleTabs.join(',')])

  const ctx: ConsoleCtx | undefined = useMemo(
    () => (cluster ? { cluster, tasks, refresh } : undefined),
    [cluster, tasks, refresh],
  )

  if (!hasPerm('PERM-FUNC-20')) {
    return (
      <Card>
        <Alert type="warning" showIcon message="当前角色无集群详情读取牌（PERM-FUNC-20），集群台无主数据可装配。" />
      </Card>
    )
  }
  if (loading && !cluster) {
    return (
      <Card>
        <Spin />
      </Card>
    )
  }
  if (!cluster) {
    return (
      <Card>
        <Alert
          type="error"
          showIcon
          message={missing ? '集群不存在或已被清除，回集群台账。' : '集群详情加载失败'}
          action={
            <Space>
              {missing ? (
                <Button size="small" onClick={() => navigate('/clc/cluster')}>返回列表</Button>
              ) : (
                <Button size="small" onClick={reloadCluster}>重试</Button>
              )}
            </Space>
          }
        />
      </Card>
    )
  }
  // 终态不渲染（9.10.6：已销毁 → 提示后回列表；DESTROYING 仍可进台看销毁进度）
  if (cluster.status === 'DESTROYED') {
    return (
      <Card>
        <Alert
          type="info"
          showIcon
          message={`集群 ${cluster.cluster_no} 已销毁（终态保留行，不渲染动作面）。`}
          action={<Button size="small" onClick={() => navigate('/clc/cluster')}>返回列表</Button>}
        />
      </Card>
    )
  }
  if (visibleTabs.length === 0) {
    return (
      <Card>
        <Alert type="info" showIcon message="当前角色在本集群上没有任何页签的主数据牌；页签按既有权限码判定，缺位即不渲染（界面隐藏不是安全边界）。" />
      </Card>
    )
  }

  // 徽章条 = 链路事实唯一呈现位（INV-32）；页签切换不重载本条（9.10.6）
  const badges = (
    <Card size="small" style={{ marginBottom: 9 }}>
      <Space wrap size={6} style={{ width: '100%', justifyContent: 'space-between' }}>
        <Space wrap size={6}>
          <Typography.Text strong style={{ fontSize: 15 }}>{cluster.name}（{cluster.cluster_no}）</Typography.Text>
          {pill(CLUSTER_STATUS, cluster.status)}
          <Space size={4}>
            {pill(CLUSTER_HEALTH, cluster.health)}
            {cluster.degraded_reason ? (
              <Typography.Text type="warning" style={{ fontSize: 12 }}>{DEGRADED_REASON[cluster.degraded_reason] || cluster.degraded_reason}</Typography.Text>
            ) : null}
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {cluster.last_probe_at ? `探活 ${cluster.last_probe_at}` : '尚无探活时间'}
            </Typography.Text>
          </Space>
          <Tag>
            推进模式：
            {attached?.step_mode ? dict.labelOf('CLC_STEP_MODE', attached.step_mode) : '派发瞬间按入口落定（INV-50 / REF-17）'}
          </Tag>
          <Tag color={cluster.template_ref ? 'geekblue' : 'default'}>
            来源模板：{cluster.template_ref ? `${cluster.template_ref}（建成后只读，INV-49）` : '未使用模板'}
          </Tag>
          <Tag>{RUN_MODE[cluster.run_mode]?.label || cluster.run_mode}</Tag>
        </Space>
        <Space size={6}>
          <Button size="small" icon={<ReloadOutlined />} onClick={refresh}>刷新</Button>
          <Button size="small" icon={<ArrowLeftOutlined />} onClick={() => navigate('/clc/cluster')}>返回列表</Button>
        </Space>
      </Space>
    </Card>
  )

  const items: { key: string; label: string; children: React.ReactNode }[] = []
  // 页签主体一律带 cluster.id 作 key：路由参数换成另一个集群时整棵子树重挂载，
  // 否则子屏的 filters 初值（useState 只在挂载时读一次 ctx）会残留上一个集群的筛选键。
  if (tabVisible.config) {
    items.push({
      key: 'config',
      label: '配置',
      children: (
        <Space direction="vertical" size={9} style={{ width: '100%' }}>
          {/* 集群基本信息（REP-02，台级已取，不再发一次查询） */}
          <Card size="small" title="集群基本信息">
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="编号">{cluster.cluster_no}</Descriptions.Item>
              <Descriptions.Item label="名称">{cluster.name}</Descriptions.Item>
              <Descriptions.Item label="DNS 域名">{cluster.cluster_dns_domain || '-'}</Descriptions.Item>
              <Descriptions.Item label="主控入口">{cluster.master_endpoints || '-'}</Descriptions.Item>
              <Descriptions.Item label="凭据存放标识（INV-08 仅标识）">{cluster.kubeconfig_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="交付版本引用">{cluster.version_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="执行机引用">{cluster.executor_ref || '-'}</Descriptions.Item>
              <Descriptions.Item label="镜像仓库实例 / 项目">
                {cluster.registry_ref || '-'} / {cluster.registry_project_ref || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="登记时间" span={2}>{cluster.created_at || '-'}</Descriptions.Item>
            </Descriptions>
          </Card>
          {/* 引用台账与参数清单（原 frmClcRef 载体，9.4；元素登记位不动） */}
          {hasPerm(['PERM-FUNC-22', 'PERM-FUNC-23']) && <RefManage key={`ref-${cluster.id}`} ctx={ctx} />}
          {/* 交付配置代理（9.9 的 FUNC-48 元素随 v1.3 收口上移至本页签；FUNC-49 原文只读按 9.10.1 / 9.10.2 留在「安装」页签） */}
          {hasPerm('PERM-FUNC-48') && (
            <Card size="small" title="交付配置代理（字段级视图，REP-17）">
              <IgnConfigFieldView clusterId={cluster.id} />
            </Card>
          )}
          {!hasPerm(['PERM-FUNC-22', 'PERM-FUNC-23', 'PERM-FUNC-48']) && (
            <Alert type="info" showIcon message="只读形态：本页签仅呈现集群详情区段，引用与参数、交付配置代理区段按权限不渲染（MU el03）。" />
          )}
        </Space>
      ),
    })
  }
  if (tabVisible.install) {
    items.push({
      key: 'install',
      label: '安装',
      children: (
        // 9.10.1 / 9.10.2：安装页签本体 = 步进器 + 两腿日志 + 完备性预检 + 原文只读（FUNC-49 / REP-18），
        // 上移到台级的只有配置代理（入「配置」）与动作卡（入「生命周期」）。
        <Space direction="vertical" size={9} style={{ width: '100%' }}>
          <WizardConsole key={`wiz-${cluster.id}`} ctx={ctx} />
          {hasPerm('PERM-FUNC-49') && (
            <Card size="small" title="交付配置原文只读（REP-18，逐次留痕、不留副本）">
              <IgnConfigRawView clusterId={cluster.id} />
            </Card>
          )}
        </Space>
      ),
    })
  }
  if (tabVisible.lifecycle) {
    items.push({
      key: 'lifecycle',
      label: '生命周期',
      children: (
        <Space direction="vertical" size={9} style={{ width: '100%' }}>
          <LifecycleConsole key={`lc-${cluster.id}`} cluster={cluster} onDispatched={refresh} />
          {hasPerm('PERM-FUNC-24') && <SnapManage key={`snap-${cluster.id}`} ctx={ctx} />}
          {hasPerm('PERM-FUNC-26') && <HealthManage key={`health-${cluster.id}`} ctx={ctx} />}
        </Space>
      ),
    })
  }
  if (tabVisible.node) {
    items.push({ key: 'node', label: '节点', children: <NodeManage key={`node-${cluster.id}`} ctx={ctx} /> })
  }
  if (tabVisible.log) {
    items.push({ key: 'log', label: '日志', children: <OrchManage key={`orch-${cluster.id}`} ctx={ctx} /> })
  }

  return (
    <Card>
      {badges}
      {/* destroyOnHidden：未可见页签不挂载 ⇒ 远程日志腿（FUNC-51 代理直展，2~3 秒游标短轮询、每次调用在对端留痕）
          不会在隐藏页签上继续轮询 */}
      <Tabs activeKey={activeTab} items={items} onChange={onTabChange} destroyOnHidden />
    </Card>
  )
}
