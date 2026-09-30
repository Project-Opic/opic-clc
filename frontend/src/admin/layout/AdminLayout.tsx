// 视觉体系移植自 gopherforge (https://github.com/SuperiorChuo/gopherforge) MIT License,适配 OPIC 技术底座。
// 管理控制台布局:渐变玻璃 Sider 224px + 60px 玻璃 Header(折叠钮 + 面包屑 + 主题切换 + 用户下拉)
// + 内容区环境光 + 路由切换 fade-in,无页签。
// 主题作用域:[data-glass][data-theme] 已统一挂在 App 根节点(见 App.tsx)并镜像到 body,
// 控制台不再重复挂载;antd 双主题通过 GlassConfigProvider 子树注入。
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Layout, Menu, Avatar, Dropdown, Space, Breadcrumb } from 'antd'
import type { MenuProps } from 'antd'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  UserOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SunOutlined,
  MoonOutlined,
  HomeOutlined,
  DashboardOutlined,
  TeamOutlined,
  SafetyOutlined,
  FolderOutlined,
  PartitionOutlined,
  DesktopOutlined,
  SolutionOutlined,
  ControlOutlined,
  SwapOutlined,
} from '@ant-design/icons'
import { useSelector } from 'react-redux'
import { authApi } from '../../api/auth'
import { clearToken } from '../../api/request'
import { clearAuth } from '../../store/slices/authSlice'
import { store } from '../../store'
import type { RootState } from '../../store'
import { hasPermPrefix } from '../access'
import { useThemeMode } from '../../theme/ThemeContext'
import GlassConfigProvider from '../../theme/GlassConfigProvider'

const { Header, Sider, Content } = Layout

interface AdminMenuDef {
  key: string
  label: string
  icon: ReactNode
  children?: { key: string; label: string; icon: ReactNode }[]
}

/** 控制台菜单定义(静态,按权限前缀过滤) */
function buildMenuDefs(hasSystem: boolean, hasFlow: boolean): AdminMenuDef[] {
  const defs: AdminMenuDef[] = [
    {
      key: '/admin/dashboard',
      label: '控制台',
      icon: <DashboardOutlined />,
    },
  ]
  if (hasSystem) {
    defs.push({
      key: 'dir-system',
      label: '系统管理',
      icon: <TeamOutlined />,
      children: [
        { key: '/admin/system/users', label: '用户管理', icon: <TeamOutlined /> },
        { key: '/admin/system/roles', label: '角色管理', icon: <SafetyOutlined /> },
        { key: '/admin/system/permissions', label: '权限管理', icon: <SafetyOutlined /> },
        { key: '/admin/system/resources', label: '资源管理', icon: <FolderOutlined /> },
      ],
    })
  }
  if (hasFlow) {
    defs.push({
      key: 'dir-flow',
      label: '流程管理',
      icon: <PartitionOutlined />,
      children: [
        { key: '/admin/flow/definitions', label: '流程定义', icon: <PartitionOutlined /> },
        { key: '/admin/flow/instances', label: '流程实例', icon: <DesktopOutlined /> },
        { key: '/admin/flow/tasks', label: '任务管理', icon: <SolutionOutlined /> },
      ],
    })
  }
  return defs
}

function defsToMenuItems(defs: AdminMenuDef[]): MenuProps['items'] {
  return defs.map((d) => {
    if (d.children) {
      return {
        key: d.key,
        icon: d.icon,
        label: d.label,
        children: d.children.map((c) => ({ key: c.key, icon: c.icon, label: c.label })),
      }
    }
    return { key: d.key, icon: d.icon, label: d.label }
  })
}

function ConsoleShell() {
  const user = useSelector((s: RootState) => s.auth.user)
  const permissions = useSelector((s: RootState) => s.auth.permissions)
  const navigate = useNavigate()
  const location = useLocation()
  const { mode, toggle: toggleTheme } = useThemeMode()
  const [collapsed, setCollapsed] = useState(false)
  const [isMobile, setIsMobile] = useState(false)

  useEffect(() => {
    const onResize = () => setIsMobile(window.innerWidth < 992)
    onResize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const hasSystem = hasPermPrefix(permissions, 'system:')
  const hasFlow = hasPermPrefix(permissions, 'flow:')
  const menuDefs = useMemo(() => buildMenuDefs(hasSystem, hasFlow), [hasSystem, hasFlow])
  const menuItems = useMemo(() => defsToMenuItems(menuDefs), [menuDefs])

  const handleLogout = async () => {
    try {
      await authApi.logout()
    } catch {
      // 忽略登出接口错误,本地清理后跳转
    }
    clearToken()
    store.dispatch(clearAuth())
    navigate('/login')
  }

  const userMenu: MenuProps['items'] = [
    {
      key: 'name',
      disabled: true,
      label: (
        <div style={{ cursor: 'default' }}>
          <div style={{ fontWeight: 600 }}>{user?.real_name || user?.username || '-'}</div>
          <div style={{ fontSize: 12, opacity: 0.65 }}>{user?.username || ''}</div>
        </div>
      ),
    },
    { type: 'divider' },
    {
      key: 'workbench',
      icon: <SwapOutlined />,
      label: '用户工作台',
      onClick: () => navigate('/customer/apply'),
    },
    { type: 'divider' },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      onClick: handleLogout,
      danger: true,
    },
  ]

  const pathname = location.pathname
  // 侧栏选中:设计器页高亮流程定义
  const selectedKey = pathname.startsWith('/admin/flow/designer/')
    ? '/admin/flow/definitions'
    : pathname

  // 面包屑:当前路径的菜单祖先链(分组 → 叶子)
  const currentDef = useMemo(() => {
    for (const d of menuDefs) {
      if (d.key === selectedKey) return { group: null as AdminMenuDef | null, leaf: { key: d.key, label: d.label } }
      for (const c of d.children || []) {
        if (c.key === selectedKey) return { group: d, leaf: { key: c.key, label: c.label } }
      }
    }
    return null
  }, [menuDefs, selectedKey])

  const breadcrumbItems = [
    {
      title: (
        <button
          type="button"
          className="app-bc-link"
          onClick={() => navigate('/admin/dashboard')}
          title="回到控制台"
        >
          <HomeOutlined />
          <span>首页</span>
        </button>
      ),
    },
    ...(currentDef?.group
      ? [
          {
            title: (
              <span className="app-bc-mid">
                {currentDef.group.icon}
                <span>{currentDef.group.label}</span>
              </span>
            ),
          },
        ]
      : []),
    ...(currentDef ? [{ title: <span className="app-bc-current">{currentDef.leaf.label}</span> }] : []),
  ]

  return (
    <Layout className={`app-shell${isMobile ? ' is-mobile' : ''}`} hasSider>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        trigger={null}
        width={224}
        collapsedWidth={isMobile ? 0 : 80}
        className="app-sider"
      >
        <div className="app-logo">
          <div className="app-logo-mark">
            <ControlOutlined />
          </div>
          {!collapsed && <span className="app-logo-text">OPIC 控制台</span>}
        </div>
        <div className="app-menu-scroll">
          <Menu
            theme={mode === 'dark' ? 'dark' : 'light'}
            mode="inline"
            selectedKeys={[selectedKey]}
            items={menuItems}
            onClick={({ key }) => {
              if (key.startsWith('/')) navigate(key)
            }}
          />
        </div>
      </Sider>

      <Layout className="app-main">
        <Header className="app-header">
          <Space size={16} className="app-header-leading">
            <span
              className="app-trigger"
              onClick={() => setCollapsed(!collapsed)}
              role="button"
              tabIndex={0}
              aria-label={collapsed ? '展开导航菜单' : '收起导航菜单'}
              onKeyDown={(e) => {
                if (e.key !== 'Enter' && e.key !== ' ') return
                e.preventDefault()
                setCollapsed(!collapsed)
              }}
            >
              {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            </span>
            <Breadcrumb className="app-breadcrumb" separator="/" items={breadcrumbItems} />
          </Space>

          <Space size={8} className="app-header-actions">
            <span
              className="app-trigger"
              onClick={(e) => {
                const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
                toggleTheme({ x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 })
              }}
              title={mode === 'dark' ? '切换为白蓝亮色' : '切换为深空暗色'}
            >
              {mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
            </span>
            <Dropdown placement="bottomRight" trigger={['click']} menu={{ items: userMenu }}>
              <div className="app-user">
                <Avatar
                  size={34}
                  icon={<UserOutlined />}
                  style={{ background: 'linear-gradient(135deg, #6366f1, #4f46e5)' }}
                />
                <span className="app-user-name">{user?.real_name || user?.username || '-'}</span>
              </div>
            </Dropdown>
          </Space>
        </Header>

        <div className="app-content-glow" />
        <Content className="app-content" style={{ position: 'relative', zIndex: 1 }}>
          <div className="page-fade-in" key={pathname}>
            <Outlet />
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}

/**
 * 管理控制台布局根节点:
 * - [data-glass][data-theme] CSS 作用域由 App 根节点统一提供;
 * - 子树内经 GlassConfigProvider 注入玻璃双主题 antd 配置。
 */
export default function AdminLayout() {
  return (
    <GlassConfigProvider>
      <ConsoleShell />
    </GlassConfigProvider>
  )
}
