// 集群域 · 集群模板台(MU frmClcTemplate,AGG-CLC-08,MASTER_DETAIL_FORM;act-FUNC-36~43)
//
// 形态（MU layout 逐句落点）：主表 = 模板清单（el04 / REP-14，只出配方头 + 角色分布摘要，
// 参数默认值不进列表页）；从表 = 节点组页签（el09）+ 参数默认行页签（el10），整组编辑（REP-15 / INV-52）。
// 默认状态筛「草稿 + 已启用」、默认行置顶高亮、未选中主行时从表给「请先选择模板」而非空表。
// el16 命中引用反查弹层的处置只有两选（改为停用 / 取消操作），不提供「解除集群引用」（INV-49 / R-22）。
// el14 设为默认仅 CLC-ROLE-02 渲染（PERM-FUNC-41 只在严运维岗位发牌集内，m5 roles 实测）。
// el18 来源集群引用建成后只读（只在详情/列表展示，界面不提供任何改写入口）。
//
// 口径来源：internal/api/business/clc_template.go + internal/service/clc_template.go。
// 不变量（默认须启用 / 启用空组 / 被引用不可删并列出全部出处 / 重名即拒 / 同角色至多一行 / 数量≥1 /
// 凭据形态值不进配方）的中文提示一律由后端给出，经响应拦截器原样透出——本屏不重写规则文案，
// 成功态只做动作确认短句；弹窗里的「前置提示」只是把契约里写明的前置条件告知用户，不是判据。
//
// 一处契约缺口（已如实实现，不改后端）：GET /api/clc/template 的 status 是单值等值筛选，
// 「草稿 + 已启用」这一缺省组合无法整体下推服务端，未显式选状态时由本屏在当页剔除停用行达成。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  AutoComplete,
  Badge,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  LinkOutlined,
  PauseCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  RightOutlined,
  SaveOutlined,
  SearchOutlined,
  StarOutlined,
} from '@ant-design/icons'
import { clusterApi, templateApi, type ClcTemplate, type ClcTemplateNodeGroup, type ClcTemplateParamLine } from '../../api/clc'
import { usePermission } from '../../hooks/usePermission'
import TableToolbar from '../../admin/components/TableToolbar'
import type { StatusTone } from '../../admin/components/StatusPill'
import { CLUSTER_STATUS, NODE_ROLE, pill, withDictLabels } from './common'
import { useDictionaries } from '../../hooks/useDictionaries'

/** 模板状态(字典 CLC_TEMPLATE_STATE；INV-51 迁移固定 DRAFT→ENABLED⇄DISABLED)。
 *  中文名与可选项一律取 /api/meta/dictionaries，本地只留 tone(DEF-09：界面不留字典副本，
 *  否则这里曾把 DISABLED 写成「停用」而模型是「已停用」)。 */
const TEMPLATE_STATE_TONE: Record<string, StatusTone> = {
  DRAFT: 'muted',
  ENABLED: 'success',
  DISABLED: 'warning',
}
const IS_DEFAULT_OPTIONS = [
  { value: '1', label: '默认项' },
  { value: '0', label: '非默认项' },
]
/** 节点角色字典域（el09 无地址 / 无凭据列；CLC_NODE_ROLE 与集群节点屏同一把尺子） */
const ROLE_OPTIONS = Object.keys(NODE_ROLE).map((k) => ({ value: k, label: `${NODE_ROLE[k]}（${k}）` }))

/** 治理动作（四类动作同口径必须携带事由——后端强校验，本屏 required 只是先行拦截） */
type GovernAction = 'enable' | 'disable' | 'default' | 'delete'

const GOVERN_META: Record<GovernAction, { title: string; ok: string; hint: string; danger?: boolean }> = {
  enable: {
    title: '启用模板',
    ok: '模板已启用',
    hint: '启用前置：节点组非空且至少一个 MASTER 组（配方至少要能描述「谁是主控」）；不满足请先用整组保存补齐角色拓扑。',
  },
  disable: {
    title: '停用模板',
    ok: '模板已停用',
    hint: '默认行不可直接停用（须先由另一张启用态模板承接默认位）；停用不回收任何既有集群的模板引用。',
  },
  default: {
    title: '设为默认',
    ok: '已设为默认',
    hint: '目标须处于启用态；全局至多一行默认，原默认位在同事务被撤销；影响面只是装机向导第二步的预选。',
  },
  delete: {
    title: '软删除模板',
    ok: '模板已软删除',
    danger: true,
    hint: '无物理删除入口；软删为终态、行保留、名称释放，节点组与参数行随聚合级联。既有集群的引用不因此失效。',
  },
}

/** 从表编辑态的草稿行（提交的是整组新行，没有行级增量改写） */
type GroupDraft = { group_name: string; node_role: string; expected_count: number }
type ParamDraft = { param_key: string; param_value: string; description: string }

const emptyGroup = (): GroupDraft => ({ group_name: '', node_role: '', expected_count: 1 })
const emptyParam = (): ParamDraft => ({ param_key: '', param_value: '', description: '' })

/** el07 / el08 候选：本域没有版本 / 参数面清单接口（R-21 只透传标识、不校验内容），
 *  下拉候选取本库既有配方头的去重值，允许直接键入新标识（AutoComplete 而非闭合 Select）。 */
function candidatesOf(rows: ClcTemplate[], pick: (r: ClcTemplate) => string | null): { value: string }[] {
  const set = new Set<string>()
  rows.forEach((r) => {
    const v = pick(r)
    if (v) set.add(v)
  })
  return [...set].sort().map((value) => ({ value }))
}

export default function ClcTemplateManage() {
  const hasPerm = usePermission()
  const canRead = hasPerm('PERM-FUNC-36')
  const dict = useDictionaries()
  const stateMap = withDictLabels(TEMPLATE_STATE_TONE, dict.itemsOf('CLC_TEMPLATE_STATE'))

  // ---------- 主表：el01 / el02 / el03 筛选 + el04 清单（act-FUNC-36） ----------
  const [rows, setRows] = useState<ClcTemplate[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [status, setStatus] = useState<string | undefined>()
  const [isDefault, setIsDefault] = useState<string | undefined>()
  const [keyword, setKeyword] = useState('')
  const [showDeleted, setShowDeleted] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(
    async (p = page, s = size) => {
      setLoading(true)
      setError('')
      try {
        const res = await templateApi.list({
          page: p,
          size: s,
          status,
          is_default: isDefault,
          keyword: keyword || undefined,
          // 「已软删」需显式勾选才出（el01 sourceNote 的口径 3 家族）：后端 deleted=0 只出存活行、
          // deleted=1 只出软删行、不传则两者都出——勾选后不传该参即「存活 + 软删一并列出」。
          deleted: showDeleted ? undefined : '0',
        })
        setRows(res.list || [])
        setTotal(res.total || 0)
      } catch (e) {
        setError((e as Error).message || '模板清单加载失败')
      } finally {
        setLoading(false)
      }
    },
    [page, size, status, isDefault, keyword, showDeleted],
  )

  useEffect(() => {
    if (canRead) void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, size, status, isDefault, showDeleted, canRead])

  const displayRows = useMemo(() => {
    const filtered = status ? rows : rows.filter((r) => r.template_status !== 'DISABLED')
    // 默认行置顶（MU layout；后端 ORDER BY flag ASC, id DESC 不含默认优先，稳定排序保留其余次序）
    return [...filtered].sort((a, b) => Number(b.default_badge) - Number(a.default_badge))
  }, [rows, status])

  // ---------- 从表：选中主行 → act-FUNC-37 详情（REP-15 两组从表 + el16 反查数据源） ----------
  const [selectedNo, setSelectedNo] = useState('')
  const [detail, setDetail] = useState<ClcTemplate | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const lastFetched = useRef('')

  // ---------- el12 整组编辑（act-FUNC-39，INV-52 整组替换） ----------
  const [editing, setEditing] = useState(false)
  const [head, setHead] = useState({ name: '', description: '', version_ref: '', param_template_ref: '' })
  const [groupDrafts, setGroupDrafts] = useState<GroupDraft[]>([])
  const [paramDrafts, setParamDrafts] = useState<ParamDraft[]>([])
  const [groupsForm] = Form.useForm<{ reason: string }>()
  const [groupsSaving, setGroupsSaving] = useState(false)

  const openDetail = useCallback(async (no: string) => {
    // 行点击与单选框 change 对同一行会各触发一次（同一 tick 内 selectedNo 还没更新），去重一次读取
    if (lastFetched.current === no) return
    lastFetched.current = no
    setSelectedNo(no)
    setDetailLoading(true)
    setEditing(false)
    try {
      setDetail(await templateApi.get(no))
    } catch {
      // 拦截器已透出后端中文提示（含编号不存在）
      lastFetched.current = ''
      setDetail(null)
    } finally {
      setDetailLoading(false)
    }
  }, [])

  /** 写接口都回传最新详情（GetClcTemplate 同一形状）：主表行就地更新，选中态同步从表 */
  const applyFresh = (fresh: ClcTemplate) => {
    setRows((prev) => prev.map((r) => (r.template_no === fresh.template_no ? fresh : r)))
    if (fresh.template_no === selectedNo) setDetail(fresh)
  }

  const startEdit = () => {
    if (!detail) return
    setHead({
      name: detail.name,
      description: detail.description || '',
      version_ref: detail.version_ref || '',
      param_template_ref: detail.param_template_ref || '',
    })
    setGroupDrafts((detail.node_groups || []).map((g) => ({ group_name: g.group_name, node_role: g.node_role, expected_count: g.expected_count })))
    setParamDrafts((detail.params || []).map((p) => ({ param_key: p.param_key, param_value: p.param_value, description: p.description || '' })))
    groupsForm.resetFields()
    setEditing(true)
  }

  const doSaveGroups = async () => {
    if (!detail) return
    const { reason } = await groupsForm.validateFields()
    setGroupsSaving(true)
    try {
      const fresh = await templateApi.saveGroups(detail.template_no, {
        reason,
        name: head.name.trim(),
        description: head.description,
        version_ref: head.version_ref,
        param_template_ref: head.param_template_ref,
        node_groups: groupDrafts.map((g) => ({ group_name: g.group_name.trim(), node_role: g.node_role, expected_count: g.expected_count })),
        params: paramDrafts.map((p) => ({ param_key: p.param_key.trim(), param_value: p.param_value, description: p.description })),
      })
      applyFresh(fresh)
      setEditing(false)
      message.success('已整组替换（未提交的行即不存在）')
      void load()
    } catch {
      // 拦截器已透出后端中文拒绝（重名 / 同角色多行 / 数量<1 / 键重复 / 凭据形态值等）
    } finally {
      setGroupsSaving(false)
    }
  }

  /** el12 sourceNote：保存前弹层给出替换前后行数差异 */
  const saveGroups = async () => {
    if (!detail) return
    if (!head.name.trim()) {
      message.warning('模板名称不能为空')
      return
    }
    if (groupDrafts.some((g) => !g.group_name.trim() || !g.node_role || g.expected_count < 1)) {
      message.warning('节点组存在未填完的行（组名 / 角色 / 期望数量 ≥1 均为必填）')
      return
    }
    if (paramDrafts.some((p) => !p.param_key.trim())) {
      message.warning('参数行存在缺少参数键的空行，请补齐或移除')
      return
    }
    const beforeGroups = (detail.node_groups || []).length
    const beforeParams = (detail.params || []).length
    const beforeGroupKeys = new Set((detail.node_groups || []).map((g) => g.group_name))
    const beforeParamKeys = new Set((detail.params || []).map((p) => p.param_key))
    const addedGroups = groupDrafts.filter((g) => !beforeGroupKeys.has(g.group_name.trim())).map((g) => g.group_name.trim())
    const droppedGroups = [...beforeGroupKeys].filter((k) => !groupDrafts.some((g) => g.group_name.trim() === k))
    const addedParams = paramDrafts.filter((p) => !beforeParamKeys.has(p.param_key.trim())).map((p) => p.param_key.trim())
    const droppedParams = [...beforeParamKeys].filter((k) => !paramDrafts.some((p) => p.param_key.trim() === k))
    Modal.confirm({
      title: `整组替换确认 · ${detail.template_no}`,
      width: 620,
      content: (
        <Space direction="vertical" size={4} style={{ fontSize: 12 }}>
          <div>节点组：替换前 {beforeGroups} 行 → 替换后 {groupDrafts.length} 行</div>
          <div>参数默认行：替换前 {beforeParams} 行 → 替换后 {paramDrafts.length} 行</div>
          {addedGroups.length > 0 && <div>本次新增组：{addedGroups.join('、')}</div>}
          {droppedGroups.length > 0 && <div>本次移除组：{droppedGroups.join('、')}</div>}
          {addedParams.length > 0 && <div>本次新增键：{addedParams.join('、')}</div>}
          {droppedParams.length > 0 && <div>本次移除键：{droppedParams.join('、')}</div>}
          <div style={{ opacity: 0.7 }}>提交的是「一整组新行」：未包含在提交里的行即不存在（旧行只在库里留痕），不改动任何已建成集群。</div>
        </Space>
      ),
      okText: '提交整组',
      onOk: doSaveGroups,
    })
  }

  // ---------- 治理动作弹窗（el13 / el14 / el15 = act-FUNC-40 / 41 / 42） ----------
  const [gov, setGov] = useState<{ action: GovernAction; row: ClcTemplate } | null>(null)
  const [govForm] = Form.useForm<{ reason: string }>()
  const [govSaving, setGovSaving] = useState(false)

  const openGovern = async (action: GovernAction, row: ClcTemplate) => {
    if (action === 'delete') {
      // el16：删除前先做命中引用反查（引用出处只有详情接口给）；命中即给两选处置，不给「解除引用」
      try {
        const full = await templateApi.get(row.template_no)
        setSelectedNo(full.template_no)
        lastFetched.current = full.template_no
        setDetail(full)
        if ((full.used_by_clusters || []).length > 0) {
          openRefs(full)
          return
        }
      } catch {
        // 反查失败不拦用户——直接进删除表单，提交时后端会再查一次并把中文拒绝直出
      }
    }
    govForm.resetFields()
    setGov({ action, row })
  }

  const submitGovern = async () => {
    if (!gov) return
    const { reason } = await govForm.validateFields()
    const { action, row } = gov
    setGovSaving(true)
    try {
      let fresh: ClcTemplate
      if (action === 'enable') fresh = await templateApi.enable(row.template_no, reason)
      else if (action === 'disable') fresh = await templateApi.disable(row.template_no, reason)
      else if (action === 'default') fresh = await templateApi.setDefault(row.template_no, reason)
      else fresh = await templateApi.del(row.template_no, reason)
      message.success(GOVERN_META[action].ok)
      setGov(null)
      if (action === 'delete') {
        // 软删为终态：主表就地留痕（勾选「显示已软删」仍可见），从表随之失效
        setRows((prev) => prev.map((r) => (r.template_no === row.template_no ? fresh : r)))
        if (selectedNo === row.template_no) {
          setSelectedNo('')
          setDetail(null)
          setEditing(false)
          lastFetched.current = ''
        }
      } else {
        applyFresh(fresh)
      }
      void load()
    } catch {
      // 拦截器已透出后端中文拒绝（默认须启用 / 停用先撤默认 / 被引用不可删并列出全部出处）
    } finally {
      setGovSaving(false)
    }
  }

  // ---------- el16 命中引用反查弹层（只读列表 + 处置单选；INV-49 / R-22） ----------
  const [refsOf, setRefsOf] = useState<ClcTemplate | null>(null)
  const [disposition, setDisposition] = useState<'disable' | 'cancel'>('cancel')
  const [refsForm] = Form.useForm<{ reason: string }>()
  const [refsSaving, setRefsSaving] = useState(false)

  function openRefs(full: ClcTemplate) {
    setDisposition('cancel')
    refsForm.resetFields()
    setRefsOf(full)
  }

  /** 主表行的反查入口：详情接口回的两组从表 + used_by_clusters 与删除保护是同一条查询 */
  async function lookupRefs(row: ClcTemplate) {
    setSelectedNo(row.template_no)
    lastFetched.current = row.template_no
    setDetailLoading(true)
    setEditing(false)
    try {
      const full = await templateApi.get(row.template_no)
      setDetail(full)
      if ((full.used_by_clusters || []).length > 0) openRefs(full)
      else message.info('该配方当前没有被任何未销毁集群引用')
    } catch {
      lastFetched.current = ''
      setDetail(null)
    } finally {
      setDetailLoading(false)
    }
  }

  const submitRefs = async () => {
    if (!refsOf) return
    if (disposition === 'cancel') {
      setRefsOf(null)
      return
    }
    const { reason } = await refsForm.validateFields()
    setRefsSaving(true)
    try {
      const fresh = await templateApi.disable(refsOf.template_no, reason)
      applyFresh(fresh)
      message.success('模板已停用')
      setRefsOf(null)
      void load()
    } catch {
      // 拦截器已透出后端中文拒绝（默认项停用须先撤默认位）
    } finally {
      setRefsSaving(false)
    }
  }

  // ---------- el11 登记模板（act-FUNC-38：草稿态 + 同事务可带整组子实体） ----------
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm] = Form.useForm<{ name: string; description?: string; version_ref?: string; param_template_ref?: string }>()
  const [createGroups, setCreateGroups] = useState<GroupDraft[]>([])
  const [createParams, setCreateParams] = useState<ParamDraft[]>([])
  const [saving, setSaving] = useState(false)

  const openCreate = () => {
    createForm.resetFields()
    setCreateGroups([emptyGroup()])
    setCreateParams([])
    setCreateOpen(true)
  }

  const submitCreate = async () => {
    const values = await createForm.validateFields()
    if (createGroups.some((g) => !g.group_name.trim() || !g.node_role || g.expected_count < 1)) {
      message.warning('节点组存在未填完的行；不需要初版拓扑请整行移除（登记允许空组，启用前再补齐）')
      return
    }
    if (createParams.some((p) => !p.param_key.trim())) {
      message.warning('参数行存在缺少参数键的空行，请补齐或移除')
      return
    }
    setSaving(true)
    try {
      await templateApi.create({
        name: values.name.trim(),
        description: values.description,
        version_ref: values.version_ref,
        param_template_ref: values.param_template_ref,
        node_groups: createGroups,
        params: createParams,
      })
      message.success('模板已登记（草稿态）')
      setCreateOpen(false)
      void load()
    } catch {
      // 拦截器已透出后端中文拒绝（重名即拒 / 组名或键重复 / 凭据形态值不进配方）
    } finally {
      setSaving(false)
    }
  }

  // ---------- el17 + act-FUNC-43 克隆（从既有模板 / 从既有集群存为模板，二选一） ----------
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneSource, setCloneSource] = useState<'template' | 'cluster'>('template')
  const [cloneForm] = Form.useForm<{ name: string; description?: string; source_template_no?: string; source_cluster_no?: string }>()
  const [clusters, setClusters] = useState<{ cluster_no: string; name: string; status: string }[]>([])
  const [cloneSaving, setCloneSaving] = useState(false)

  const openClone = (source: 'template' | 'cluster', row?: ClcTemplate) => {
    cloneForm.resetFields()
    setCloneSource(source)
    if (source === 'cluster') {
      clusterApi
        .list({ page: 1, size: 100 })
        .then((r) => setClusters((r.list || []).map((c) => ({ cluster_no: c.cluster_no, name: c.name, status: c.status }))))
        .catch(() => setClusters([]))
    } else if (row) {
      cloneForm.setFieldsValue({ source_template_no: row.template_no })
    }
    setCloneOpen(true)
  }

  const submitClone = async () => {
    const v = await cloneForm.validateFields()
    setCloneSaving(true)
    try {
      await templateApi.clone({
        name: v.name.trim(),
        description: v.description,
        source_template_no: cloneSource === 'template' ? v.source_template_no : undefined,
        source_cluster_no: cloneSource === 'cluster' ? v.source_cluster_no : undefined,
      })
      message.success('克隆完成（产物为新登记一份草稿）')
      setCloneOpen(false)
      void load()
    } catch {
      // 拦截器已透出后端中文拒绝（两来源都给 / 都不给、来源集群无在册节点、重名）
    } finally {
      setCloneSaving(false)
    }
  }

  const versionCandidates = useMemo(() => candidatesOf(rows, (r) => r.version_ref), [rows])
  const paramTemplateCandidates = useMemo(() => candidatesOf(rows, (r) => r.param_template_ref), [rows])

  // ---------- el04 主表列 ----------
  const columns: ColumnsType<ClcTemplate> = [
    { title: '模板编号', dataIndex: 'template_no', key: 'template_no', width: 165 },
    {
      title: '模板名称（el05）',
      dataIndex: 'name',
      key: 'name',
      width: 190,
      render: (_, r) => (
        <Space size={4}>
          {r.name}
          {r.default_badge && (
            <Tooltip title="默认行（全局至多一行且须处于启用态）：只作用于装机向导第二步预选，不改动任何既有集群">
              <Tag color="gold">默认</Tag>
            </Tooltip>
          )}
          {r.deleted && <Tag>已软删</Tag>}
        </Space>
      ),
    },
    { title: '状态', dataIndex: 'template_status', key: 'template_status', width: 92, render: (v: string) => pill(stateMap, v) },
    {
      title: '角色分布摘要',
      dataIndex: 'role_summary',
      key: 'role_summary',
      width: 170,
      render: (v: string) => <span style={{ fontSize: 12 }}>{v}</span>,
    },
    {
      // el16 入口：命中引用反查（出处即下一步动作的位置——这些集群行可在集群台账屏按来源模板筛出）
      title: '命中引用',
      key: 'refs',
      width: 150,
      render: (_, r) => {
        const loaded = r.template_no === detail?.template_no
        const count = loaded ? (detail?.used_by_clusters || []).length : undefined
        return (
          <Space size={4}>
            {count === undefined ? (
              <span style={{ fontSize: 12, opacity: 0.6 }}>未取数</span>
            ) : (
              <Badge count={count} showZero color={count ? 'processing' : undefined} />
            )}
            <Button size="small" type="link" icon={<LinkOutlined />} onClick={() => void lookupRefs(r)}>
              反查
            </Button>
          </Space>
        )
      },
    },
    { title: '交付版本引用（el07）', dataIndex: 'version_ref', key: 'version_ref', width: 160, render: (v: string | null) => <span style={{ fontSize: 12 }}>{v || '-'}</span> },
    { title: '参数面模板引用（el08）', dataIndex: 'param_template_ref', key: 'param_template_ref', width: 180, render: (v: string | null) => <span style={{ fontSize: 12 }}>{v || '-'}</span> },
    {
      // el18：建成后只读——没有任何入口能改它（克隆留痕 / 人工登记为空）
      title: '来源集群（el18 只读）',
      dataIndex: 'source_cluster_ref',
      key: 'source_cluster_ref',
      width: 150,
      render: (v: string | null) => <span style={{ fontSize: 12 }}>{v || '人工登记'}</span>,
    },
    { title: '说明（el06）', dataIndex: 'description', key: 'description', ellipsis: true, render: (v: string | null) => v || '-' },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 155, render: (v: string) => <span style={{ fontSize: 12 }}>{v}</span> },
    {
      title: '操作',
      key: 'action',
      width: 290,
      fixed: 'right',
      render: (_, r) => {
        const dead = r.deleted
        return (
          <Space size={2}>
            <Button size="small" type="link" onClick={() => openDetail(r.template_no)}>
              从表
            </Button>
            {!dead && hasPerm('PERM-FUNC-40') && (r.template_status === 'DRAFT' || r.template_status === 'DISABLED') && (
              <Button size="small" type="link" icon={<RightOutlined />} onClick={() => openGovern('enable', r)}>
                启用
              </Button>
            )}
            {!dead && hasPerm('PERM-FUNC-40') && r.template_status === 'ENABLED' && (
              <Button size="small" type="link" icon={<PauseCircleOutlined />} onClick={() => openGovern('disable', r)}>
                停用
              </Button>
            )}
            {/* el14：设为默认仅 CLC-ROLE-02 渲染（PERM-FUNC-41 只在严运维岗位发牌集内） */}
            {!dead && hasPerm('PERM-FUNC-41') && r.template_status === 'ENABLED' && !r.is_default && (
              <Button size="small" type="link" icon={<StarOutlined />} onClick={() => openGovern('default', r)}>
                设为默认
              </Button>
            )}
            {!dead && hasPerm('PERM-FUNC-43') && (
              <Button size="small" type="link" icon={<CopyOutlined />} onClick={() => openClone('template', r)}>
                克隆
              </Button>
            )}
            {!dead && hasPerm('PERM-FUNC-42') && (
              <Button size="small" type="link" danger icon={<DeleteOutlined />} onClick={() => openGovern('delete', r)}>
                删除
              </Button>
            )}
          </Space>
        )
      },
    },
  ]

  // ---------- el09 节点组只读列 ----------
  const groupViewColumns: ColumnsType<ClcTemplateNodeGroup> = [
    { title: '分组名称', dataIndex: 'group_name', key: 'group_name' },
    { title: '节点角色', dataIndex: 'node_role', key: 'node_role', width: 140, render: (v: string) => NODE_ROLE[v] || v },
    { title: '期望数量', dataIndex: 'expected_count', key: 'expected_count', width: 110 },
    { title: '展示次序', dataIndex: 'sort_no', key: 'sort_no', width: 100 },
  ]

  // ---------- el10 参数默认行只读列（敏感值不整页展开：显示掩码列 + 命中提示） ----------
  const paramViewColumns: ColumnsType<ClcTemplateParamLine> = [
    { title: '参数键', dataIndex: 'param_key', key: 'param_key', width: 220 },
    {
      title: '默认值',
      key: 'value',
      render: (_, p) => {
        if (p.display_note) return <span style={{ fontSize: 12, opacity: 0.75 }}>{p.display_note}</span>
        const shown = p.param_value_masked ?? p.param_value
        return (
          <Space size={4}>
            <span style={{ fontSize: 12, whiteSpace: 'pre-wrap' }}>{shown || '-'}</span>
            {p.masked_hit && <Tag color="orange">库里脏值已掩码显示</Tag>}
          </Space>
        )
      },
    },
    { title: '说明', dataIndex: 'description', key: 'description', width: 220, render: (v: string | null) => v || '-' },
    { title: '次序', dataIndex: 'sort_no', key: 'sort_no', width: 80 },
  ]

  /** 从表编辑态的行编辑器（整组语义：新增 / 移除只改本地草稿，提交才落库） */
  const groupEditor = (drafts: GroupDraft[], setDrafts: (next: GroupDraft[]) => void) => (
    <>
      {drafts.map((g, idx) => (
        <Row key={idx} gutter={8} align="middle" style={{ marginBottom: 6 }}>
          <Col span={7}>
            <Input placeholder="分组名称（模板内唯一）" value={g.group_name} onChange={(e) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, group_name: e.target.value } : o)))} />
          </Col>
          <Col span={6}>
            <Select
              style={{ width: '100%' }}
              placeholder="节点角色"
              value={g.node_role || undefined}
              options={ROLE_OPTIONS.map((o) => ({ ...o, disabled: drafts.some((x, i) => i !== idx && x.node_role === o.value) }))}
              onChange={(v) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, node_role: v } : o)))}
            />
          </Col>
          <Col span={4}>
            <InputNumber min={1} style={{ width: '100%' }} value={g.expected_count} onChange={(v) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, expected_count: Number(v ?? 1) } : o)))} />
          </Col>
          <Col span={3}>
            <Button size="small" danger icon={<DeleteOutlined />} onClick={() => setDrafts(drafts.filter((_, i) => i !== idx))}>
              移除
            </Button>
          </Col>
        </Row>
      ))}
      <Button size="small" icon={<PlusOutlined />} onClick={() => setDrafts([...drafts, emptyGroup()])}>
        新增组行
      </Button>
      <div style={{ fontSize: 12, opacity: 0.7, marginTop: 6 }}>同角色模板内至多一行（数量由期望数量表达）；节点组不承载地址与凭据——那是实例事实。</div>
    </>
  )

  const paramEditor = (drafts: ParamDraft[], setDrafts: (next: ParamDraft[]) => void) => (
    <>
      {drafts.map((p, idx) => (
        <Row key={idx} gutter={8} align="middle" style={{ marginBottom: 6 }}>
          <Col span={7}>
            <Input placeholder="参数键（模板内唯一，键域沿用集群参数口径）" value={p.param_key} onChange={(e) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, param_key: e.target.value } : o)))} />
          </Col>
          <Col span={8}>
            <Input placeholder="默认值（空串 = 实例化时现场填）" value={p.param_value} onChange={(e) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, param_value: e.target.value } : o)))} />
          </Col>
          <Col span={6}>
            <Input placeholder="说明" value={p.description} onChange={(e) => setDrafts(drafts.map((o, i) => (i === idx ? { ...o, description: e.target.value } : o)))} />
          </Col>
          <Col span={3}>
            <Button size="small" danger icon={<DeleteOutlined />} onClick={() => setDrafts(drafts.filter((_, i) => i !== idx))}>
              移除
            </Button>
          </Col>
        </Row>
      ))}
      <Button size="small" icon={<PlusOutlined />} onClick={() => setDrafts([...drafts, emptyParam()])}>
        新增参数行
      </Button>
      <div style={{ fontSize: 12, opacity: 0.7, marginTop: 6 }}>口令 / 令牌 / 私钥一类的凭据值不进配方（提交即被治理拒绝）；要引用现场凭据请登记存放位置标识，或留空由实例化时现场填。</div>
    </>
  )

  /** 未选中主行时的从表（MU layout：给「请先选择模板」而不是空表） */
  const noMasterHint = (
    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先选择模板" style={{ padding: '24px 0' }}>
      <span style={{ fontSize: 12, opacity: 0.7 }}>在主表点一行模板，节点组与参数默认行两组从表按模板编号（FUNC-37）读取</span>
    </Empty>
  )

  if (!canRead) {
    // 无 PERM-FUNC-36 时本屏不渲染任何数据入口也不发请求（菜单入口由后端 GetMenus 同码过滤）
    return (
      <Card>
        <Alert type="warning" showIcon message="无本屏访问权限" description="集群模板清单需要 PERM-FUNC-36（模板列表查询）；请联系平台管理员确认岗位发牌。" />
      </Card>
    )
  }

  return (
    <Card>
      <style>{`
        .clc-tpl-default-row > td { background: rgba(250, 211, 120, 0.16) !important; }
        .clc-tpl-selected-row > td { box-shadow: inset 3px 0 0 #4f46e5; }
      `}</style>
      <TableToolbar
        title="集群模板"
        description="输入配方而非运行依赖（R-20）：登记 / 整组编辑 / 启停 / 默认 / 软删 / 克隆都不回写任何已建成集群（INV-49）"
        total={total}
        extra={
          <Space wrap>
            <Button icon={<ReloadOutlined />} loading={loading} onClick={() => load()}>
              刷新
            </Button>
            {hasPerm('PERM-FUNC-43') && (
              // el17：读集群当前节点组与参数行做初值，不带地址 / 凭据
              <Button icon={<CopyOutlined />} onClick={() => openClone('cluster')}>
                从既有集群存为模板
              </Button>
            )}
            {hasPerm('PERM-FUNC-38') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                登记模板
              </Button>
            )}
          </Space>
        }
      />

      <Space wrap style={{ marginBottom: 12 }}>
        <span style={{ fontSize: 12 }}>状态</span>
        <Select
          allowClear
          placeholder="草稿 + 已启用（缺省，不含停用）"
          style={{ width: 240 }}
          value={status}
          options={dict.optionsOf('CLC_TEMPLATE_STATE')}
          onChange={(v) => {
            setStatus(v)
            setPage(1)
          }}
        />
        <span style={{ fontSize: 12 }}>是否默认</span>
        <Select allowClear placeholder="全部" style={{ width: 130 }} value={isDefault} options={IS_DEFAULT_OPTIONS} onChange={(v) => { setIsDefault(v); setPage(1) }} />
        <Input
          allowClear
          placeholder="关键字（只匹配名称 / 说明，不检索参数值）"
          style={{ width: 260 }}
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          onPressEnter={() => { setPage(1); void load(1, size) }}
        />
        <Button icon={<SearchOutlined />} onClick={() => { setPage(1); void load(1, size) }}>
          查询
        </Button>
        <Tooltip title="默认只看存活行；勾选后连同已软删行一并列出（无物理删除入口，软删为终态）">
          <Checkbox checked={showDeleted} onChange={(e) => { setShowDeleted(e.target.checked); setPage(1) }}>
            显示已软删
          </Checkbox>
        </Tooltip>
      </Space>

      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error} action={<Button size="small" onClick={() => load()}>重试</Button>} />
      )}
      {!status && rows.some((r) => r.template_status === 'DISABLED') && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="缺省视图按「草稿 + 已启用」呈现：本模板列表接口的状态筛选是单值等值口径，停用行已在当页剔除——需要停用行请把状态显式选为「停用」，需要连同软删一并看请勾选「显示已软删」。"
        />
      )}

      <Table
        rowKey="template_no"
        size="small"
        columns={columns}
        dataSource={displayRows}
        loading={loading}
        scroll={{ x: 1680 }}
        rowClassName={(r) => `${r.default_badge ? 'clc-tpl-default-row ' : ''}${r.template_no === selectedNo ? 'clc-tpl-selected-row' : ''}`}
        rowSelection={{
          type: 'radio',
          selectedRowKeys: selectedNo ? [selectedNo] : [],
          columnTitle: '从表',
          onChange: (keys) => {
            const no = String(keys[0] || '')
            if (no) void openDetail(no)
          },
        }}
        onRow={(r) => ({
          onClick: () => {
            if (selectedNo !== r.template_no) void openDetail(r.template_no)
          },
          style: { cursor: 'pointer' },
        })}
        pagination={{
          current: page,
          pageSize: size,
          total,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50],
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p)
            setSize(s)
          },
        }}
      />

      {/* 从表区（REP-15）：未选中主行即给「请先选择模板」 */}
      <Card
        size="small"
        style={{ marginTop: 12 }}
        title={detail ? `从表 · ${detail.template_no}（${detail.name}）` : '从表'}
        loading={detailLoading}
        extra={
          detail && !detail.deleted && hasPerm('PERM-FUNC-39') ? (
            editing ? (
              <Space>
                <Button size="small" onClick={() => setEditing(false)}>
                  取消编辑
                </Button>
                {/* el12：提交「一整组新行」整组替换，保存前先给替换前后行数差异 */}
                <Button size="small" type="primary" icon={<SaveOutlined />} loading={groupsSaving} onClick={() => void saveGroups()}>
                  整组保存
                </Button>
              </Space>
            ) : (
              <Button size="small" icon={<EditOutlined />} onClick={startEdit}>
                整组编辑
              </Button>
            )
          ) : null
        }
      >
        {!detail ? (
          noMasterHint
        ) : (
          <>
            <Descriptions column={3} size="small" bordered style={{ marginBottom: 12 }}>
              <Descriptions.Item label="模板编号">{detail.template_no}</Descriptions.Item>
              <Descriptions.Item label="状态">{pill(stateMap, detail.template_status)}</Descriptions.Item>
              <Descriptions.Item label="角色分布摘要">{detail.role_summary}</Descriptions.Item>
              <Descriptions.Item label="模板名称">
                <Space size={4}>
                  {editing ? <Input value={head.name} onChange={(e) => setHead({ ...head, name: e.target.value })} /> : detail.name}
                  {detail.default_badge && <Tag color="gold">默认</Tag>}
                  {detail.deleted && <Tag>已软删</Tag>}
                </Space>
              </Descriptions.Item>
              <Descriptions.Item label="交付版本引用（el07 只透传标识 R-21）">
                {editing ? (
                  <AutoComplete style={{ width: '100%' }} value={head.version_ref} options={versionCandidates} onChange={(v) => setHead({ ...head, version_ref: v })} placeholder="O-IGN 侧版本标识，不校验内容" />
                ) : (
                  detail.version_ref || '-'
                )}
              </Descriptions.Item>
              <Descriptions.Item label="参数面模板引用（el08 只透传标识 R-21）">
                {editing ? (
                  <AutoComplete style={{ width: '100%' }} value={head.param_template_ref} options={paramTemplateCandidates} onChange={(v) => setHead({ ...head, param_template_ref: v })} placeholder="O-IGN AGG-IGN-07 标识" />
                ) : (
                  detail.param_template_ref || '-'
                )}
              </Descriptions.Item>
              <Descriptions.Item label="说明（el06）" span={3}>
                {editing ? <Input value={head.description} onChange={(e) => setHead({ ...head, description: e.target.value })} maxLength={500} /> : detail.description || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="来源集群引用（el18 建成后只读）" span={3}>
                {detail.source_cluster_ref || '人工登记（无来源集群）'}
              </Descriptions.Item>
            </Descriptions>

            <Tabs
              items={[
                {
                  key: 'groups',
                  label: `节点组（${(detail.node_groups || []).length}）`,
                  children: editing ? (
                    groupEditor(groupDrafts, setGroupDrafts)
                  ) : (
                    <Table
                      rowKey={(r) => `${r.group_name}-${r.node_role}`}
                      size="small"
                      pagination={false}
                      columns={groupViewColumns}
                      dataSource={detail.node_groups || []}
                      locale={{
                        emptyText: selectedNo ? '节点组为空（草稿态正常；启用前置是非空且至少一个 MASTER 组，被治理拒绝时后端直出）' : '请先选择模板',
                      }}
                    />
                  ),
                },
                {
                  key: 'params',
                  label: `参数默认行（${(detail.params || []).length}）`,
                  children: editing ? (
                    paramEditor(paramDrafts, setParamDrafts)
                  ) : (
                    <Table
                      rowKey={(r) => r.param_key}
                      size="small"
                      pagination={false}
                      columns={paramViewColumns}
                      dataSource={detail.params || []}
                      locale={{ emptyText: selectedNo ? '参数默认行为空（允许——参数键不建字典，实例化时按向导逐键确认）' : '请先选择模板' }}
                    />
                  ),
                },
              ]}
            />

            {editing && (
              <Form form={groupsForm} layout="vertical" style={{ marginTop: 8 }}>
                <Form.Item name="reason" label="变更事由（整组替换必填）" rules={[{ required: true, message: '整组编辑必须携带事由' }]}>
                  <Input.TextArea rows={2} maxLength={500} placeholder="改了什么、为什么改（配方是跨集群复用资产，要可回放）" />
                </Form.Item>
              </Form>
            )}
          </>
        )}
      </Card>

      {/* el11 登记模板（act-FUNC-38）：草稿态 + 同事务可带整组子实体 */}
      <Modal title="登记集群模板" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => void submitCreate()} confirmLoading={saving} width={760}>
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message="登记即分配编号 TPL-YYYYMMDD-NNNN（生成后只读），状态一律草稿；重名在未删除范围内即拒（软删释放名称）。" />
        <Form form={createForm} layout="vertical">
          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="name" label="模板名称（el05 必填）" rules={[{ required: true, message: '必填' }]}>
                <Input placeholder="生产三主 + 十二工作负载基线" maxLength={100} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="version_ref" label="交付版本引用（el07）">
                <AutoComplete options={versionCandidates} placeholder="O-IGN 侧标识，只透传不校验" />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="param_template_ref" label="参数面模板引用（el08）">
                <AutoComplete options={paramTemplateCandidates} placeholder="AGG-IGN-07 标识" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="description" label="说明（el06 ≤500 字）">
            <Input.TextArea rows={2} maxLength={500} placeholder="区分两张「看起来一样」的配方只有靠说明" />
          </Form.Item>
          <div style={{ marginBottom: 8, fontWeight: 600 }}>节点组初值（可留空，启用前补齐；与模板同事务整组写入）</div>
          {groupEditor(createGroups, setCreateGroups)}
          <div style={{ margin: '16px 0 8px', fontWeight: 600 }}>参数默认行初值</div>
          {paramEditor(createParams, setCreateParams)}
        </Form>
      </Modal>

      {/* el17 + act-FUNC-43 克隆：来源二选一 */}
      <Modal title="克隆为模板（草稿态新登记一份）" open={cloneOpen} onCancel={() => setCloneOpen(false)} onOk={() => void submitClone()} confirmLoading={cloneSaving} width={620}>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="来源只能给一个：既有模板（改名复制头字段与两组行）或既有集群（读集群当前节点组与参数行做初值，不带地址与凭据）。克隆产物一律新登记一份草稿，不复用来源状态。"
        />
        <Form form={cloneForm} layout="vertical">
          <Form.Item label="克隆来源">
            <Radio.Group
              value={cloneSource}
              onChange={(e) => {
                setCloneSource(e.target.value)
                cloneForm.setFieldsValue({ source_template_no: undefined, source_cluster_no: undefined })
              }}
              options={[
                { value: 'template', label: '从既有模板' },
                { value: 'cluster', label: '从既有集群（el17）' },
              ]}
            />
          </Form.Item>
          {cloneSource === 'template' ? (
            <Form.Item name="source_template_no" label="来源模板" rules={[{ required: true, message: '必选' }]}>
              <Select
                showSearch
                optionFilterProp="label"
                placeholder="已软删行不能作为克隆来源（提交即被治理拒绝）"
                options={rows.filter((r) => !r.deleted).map((r) => ({ value: r.template_no, label: `${r.name}（${r.template_no}）` }))}
              />
            </Form.Item>
          ) : (
            <Form.Item name="source_cluster_no" label="来源集群" rules={[{ required: true, message: '必选' }]} extra="集群须有在册节点（待加入 / 已加入），否则角色拓扑克隆不出来——提交时后端直出">
              <Select
                showSearch
                optionFilterProp="label"
                placeholder={clusters.length ? '选择集群' : '集群清单加载中…'}
                options={clusters.map((c) => ({ value: c.cluster_no, label: `${c.name}（${c.cluster_no} · ${CLUSTER_STATUS[c.status]?.label || c.status}）` }))}
              />
            </Form.Item>
          )}
          <Form.Item name="name" label="新模板名称（必填，未删除范围内唯一）" rules={[{ required: true, message: '必填' }]}>
            <Input placeholder="生产基线-变体A" maxLength={100} />
          </Form.Item>
          <Form.Item name="description" label="说明（可空）">
            <Input.TextArea rows={2} maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 治理动作确认（el13 / el14 / el15）：事由必填 + 前置条件提示；拒绝原文由后端直出 */}
      <Modal
        title={gov ? `${GOVERN_META[gov.action].title} · ${gov.row.template_no}` : ''}
        open={!!gov}
        onCancel={() => setGov(null)}
        onOk={() => void submitGovern()}
        confirmLoading={govSaving}
        okButtonProps={{ danger: GOVERN_META[gov?.action || 'enable'].danger }}
        width={600}
      >
        {gov && (
          <>
            <Alert type={gov.action === 'delete' ? 'warning' : 'info'} showIcon style={{ marginBottom: 12 }} message={GOVERN_META[gov.action].hint} />
            <div style={{ fontSize: 12, marginBottom: 8 }}>
              当前：{pill(stateMap, gov.row.template_status)} · 角色分布 {gov.row.role_summary}
              {gov.row.default_badge ? ' · 默认行' : ''}
            </div>
            <Form form={govForm} layout="vertical">
              <Form.Item name="reason" label="变更事由（治理动作留痕，必填）" rules={[{ required: true, message: '必须携带事由' }]}>
                <Input.TextArea rows={3} maxLength={500} placeholder="为什么执行该治理动作" />
              </Form.Item>
            </Form>
          </>
        )}
      </Modal>

      {/* el16 命中引用反查弹层：只读列表 + 处置单选（只有两选，不提供「解除集群引用」INV-49 / R-22） */}
      <Modal
        title={refsOf ? `命中引用反查 · ${refsOf.template_no}` : ''}
        open={!!refsOf}
        onCancel={() => setRefsOf(null)}
        onOk={() => void submitRefs()}
        confirmLoading={refsSaving}
        okText="提交处置"
        width={680}
      >
        {refsOf && (
          <>
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 12 }}
              message={`本配方正被 ${(refsOf.used_by_clusters || []).length} 个未销毁集群引用，不可删除（删除保护）。引用是集群侧的一次性留痕，模板侧不提供「解除集群引用」——处置只有两选：改为停用，或取消操作。`}
              description="出处即下一步动作的位置：这些集群行可在「集群台账」屏按来源模板筛出；引用本身不因模板软删失效。"
            />
            <Table
              rowKey={(r) => r.cluster_no}
              size="small"
              pagination={false}
              dataSource={refsOf.used_by_clusters || []}
              columns={[
                { title: '集群编号', dataIndex: 'cluster_no', key: 'cluster_no', width: 170 },
                { title: '集群名称', dataIndex: 'name', key: 'name' },
                { title: '生命周期状态', dataIndex: 'status', key: 'status', width: 130, render: (v: string) => pill(CLUSTER_STATUS, v) },
              ]}
            />
            <Radio.Group
              style={{ marginTop: 12 }}
              value={disposition}
              onChange={(e) => setDisposition(e.target.value)}
              options={[
                { value: 'disable', label: '改为停用（不再被新集群选中，既有引用不回收）' },
                { value: 'cancel', label: '取消操作（保持现状，先确认配方是否还复用）' },
              ]}
            />
            {disposition === 'disable' && (
              <Form form={refsForm} layout="vertical" style={{ marginTop: 8 }}>
                <Form.Item name="reason" label="停用事由（必填）" rules={[{ required: true, message: '停用是治理动作，必须携带事由' }]}>
                  <Input.TextArea rows={2} maxLength={500} />
                </Form.Item>
              </Form>
            )}
          </>
        )}
      </Modal>
    </Card>
  )
}
